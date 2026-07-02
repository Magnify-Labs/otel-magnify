package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/magnify-labs/otel-magnify/internal/audit"
	"github.com/magnify-labs/otel-magnify/internal/validator"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

type configApprovalRequestPayload struct {
	DraftYAML        string `json:"draft_yaml"`
	TargetGroup      string `json:"target_group"`
	TargetEnv        string `json:"target_env"`
	Comment          string `json:"comment"`
	ProdConfirmation bool   `json:"prod_confirmation"`
}

type configApprovalCommentPayload struct {
	Comment string `json:"comment"`
}

type configApprovalPushPayload struct {
	Comment             string `json:"comment"`
	ProdDoubleConfirmed bool   `json:"prod_double_confirmed"`
	BreakGlass          bool   `json:"break_glass"`
	BreakGlassReason    string `json:"break_glass_reason"`
}

func (a *API) handleCreateOrUpdateConfigApproval(w http.ResponseWriter, r *http.Request) {
	workloadID := chi.URLParam(r, "id")
	var req configApprovalRequestPayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid approval request body")
		return
	}
	//nolint:errcheck // deferred cleanup of request body; net/http server also closes it
	defer r.Body.Close()

	draft := strings.TrimSpace(req.DraftYAML)
	if draft == "" {
		respondError(w, http.StatusBadRequest, "empty config draft")
		return
	}
	if strings.TrimSpace(req.TargetGroup) == "" {
		respondError(w, http.StatusBadRequest, "target_group is required")
		return
	}
	if strings.TrimSpace(req.Comment) == "" {
		respondError(w, http.StatusBadRequest, "comment is required")
		return
	}

	wl, err := a.db.GetWorkload(workloadID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondError(w, http.StatusNotFound, "workload not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to load workload")
		return
	}
	if !wl.AcceptsRemoteConfig {
		respondJSON(w, http.StatusConflict, map[string]string{"error": "workload does not accept remote config", "code": "remote_config_unsupported"})
		return
	}

	if result := validateApprovalDraft(r, []byte(req.DraftYAML), wl); !result.Valid {
		respondJSON(w, http.StatusBadRequest, map[string]any{"error": "configuration failed validation", "validation_errors": result.Errors})
		return
	}

	actor := currentUserEmail(r)
	prodTarget := isProdTarget(req.TargetGroup, req.TargetEnv)
	if prodTarget && !req.ProdConfirmation {
		respondError(w, http.StatusBadRequest, "prod approval request requires confirmation")
		return
	}
	approval, err := a.db.CreateOrUpdateConfigApprovalRequest(models.ConfigApprovalRequest{
		WorkloadID:       workloadID,
		DraftYAML:        req.DraftYAML,
		TargetGroup:      strings.TrimSpace(req.TargetGroup),
		TargetEnv:        strings.TrimSpace(req.TargetEnv),
		Requester:        actor,
		RequestComment:   strings.TrimSpace(req.Comment),
		ProdTarget:       prodTarget,
		ProdConfirmation: req.ProdConfirmation,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save approval request")
		return
	}
	if err := audit.Emit(r.Context(), a.audit, "config.approval.request", "workload", workloadID, approval.ID+":"+approval.TargetGroup); err != nil {
		respondAuditUnavailable(w, sideEffectApplied)
		return
	}
	respondJSON(w, http.StatusCreated, approval)
}

func (a *API) handleListConfigApprovals(w http.ResponseWriter, r *http.Request) {
	items, err := a.db.ListConfigApprovalRequests(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list approval requests")
		return
	}
	if items == nil {
		items = []models.ConfigApprovalRequest{}
	}
	respondJSON(w, http.StatusOK, items)
}

func (a *API) handleApproveConfigApproval(w http.ResponseWriter, r *http.Request) {
	workloadID := chi.URLParam(r, "id")
	approvalID := chi.URLParam(r, "approval_id")
	var req configApprovalCommentPayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid approval body")
		return
	}
	//nolint:errcheck // deferred cleanup of request body; net/http server also closes it
	defer r.Body.Close()
	if strings.TrimSpace(req.Comment) == "" {
		respondError(w, http.StatusBadRequest, "comment is required")
		return
	}

	approval, err := a.db.GetConfigApprovalRequest(approvalID)
	if err != nil {
		respondConfigApprovalLookupError(w, err)
		return
	}
	if approval.WorkloadID != workloadID {
		respondError(w, http.StatusNotFound, "approval request not found")
		return
	}
	wl, err := a.db.GetWorkload(workloadID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load workload")
		return
	}
	if result := validateApprovalDraft(r, []byte(approval.DraftYAML), wl); !result.Valid {
		respondJSON(w, http.StatusBadRequest, map[string]any{"error": "configuration failed validation", "validation_errors": result.Errors})
		return
	}

	approved, err := a.db.ApproveConfigApprovalRequest(approvalID, currentUserEmail(r), strings.TrimSpace(req.Comment), time.Now().UTC())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondJSON(w, http.StatusConflict, map[string]string{"error": "approval request is not pending", "code": "approval_not_pending"})
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to approve request")
		return
	}
	if err := audit.Emit(r.Context(), a.audit, "config.approval.approve", "workload", workloadID, approvalID); err != nil {
		respondAuditUnavailable(w, sideEffectApplied)
		return
	}
	respondJSON(w, http.StatusOK, approved)
}

func (a *API) handlePushConfigApproval(w http.ResponseWriter, r *http.Request) {
	workloadID := chi.URLParam(r, "id")
	approvalID := chi.URLParam(r, "approval_id")
	var req configApprovalPushPayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid approval push body")
		return
	}
	//nolint:errcheck // deferred cleanup of request body; net/http server also closes it
	defer r.Body.Close()
	if strings.TrimSpace(req.Comment) == "" {
		respondError(w, http.StatusBadRequest, "comment is required")
		return
	}
	if req.BreakGlass && strings.TrimSpace(req.BreakGlassReason) == "" {
		respondError(w, http.StatusBadRequest, "break_glass_reason is required")
		return
	}
	if a.opamp == nil {
		respondError(w, http.StatusServiceUnavailable, "OpAMP server not available")
		return
	}

	approval, err := a.db.GetConfigApprovalRequest(approvalID)
	if err != nil {
		respondConfigApprovalLookupError(w, err)
		return
	}
	if approval.WorkloadID != workloadID {
		respondError(w, http.StatusNotFound, "approval request not found")
		return
	}
	if approval.ProdTarget && !req.ProdDoubleConfirmed {
		respondError(w, http.StatusBadRequest, "prod push requires double confirmation")
		return
	}
	if approval.Status == models.ConfigApprovalStatusPushed {
		respondJSON(w, http.StatusConflict, map[string]string{"error": "approval request has already been pushed", "code": "approval_already_pushed"})
		return
	}
	if !req.BreakGlass && approval.Status != models.ConfigApprovalStatusApproved {
		respondJSON(w, http.StatusConflict, map[string]string{"error": "approval request is not approved", "code": "approval_required"})
		return
	}

	wl, err := a.db.GetWorkload(workloadID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load workload")
		return
	}
	if !wl.AcceptsRemoteConfig {
		respondJSON(w, http.StatusConflict, map[string]string{"error": "workload does not accept remote config", "code": "remote_config_unsupported"})
		return
	}
	if !workloadIsConnected(wl) {
		respondJSON(w, http.StatusConflict, map[string]string{"error": "workload is not connected", "code": "workload_not_connected"})
		return
	}
	if result := validateApprovalDraft(r, []byte(approval.DraftYAML), wl); !result.Valid {
		respondJSON(w, http.StatusBadRequest, map[string]any{"error": "configuration failed validation", "validation_errors": result.Errors})
		return
	}

	hash, err := a.persistAndPushApprovalConfig(r, workloadID, []byte(approval.DraftYAML))
	if err != nil {
		returnPushApprovalError(w, a, workloadID, hash, err)
		return
	}
	pushed, err := a.db.MarkConfigApprovalRequestPushed(approvalID, hash, strings.TrimSpace(req.Comment), req.ProdDoubleConfirmed, req.BreakGlass, strings.TrimSpace(req.BreakGlassReason), time.Now().UTC())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to mark approval request pushed")
		return
	}
	action := "config.approval.push"
	detail := approvalID + ":" + hash
	if req.BreakGlass {
		action = "config.approval.break_glass_push"
		detail = approvalID + ":" + strings.TrimSpace(req.BreakGlassReason) + ":" + hash
	}
	if err := audit.Emit(r.Context(), a.audit, action, "workload", workloadID, detail); err != nil {
		respondAuditUnavailable(w, sideEffectApplied)
		return
	}
	respondJSON(w, http.StatusAccepted, pushed)
}

func validateApprovalDraft(r *http.Request, body []byte, wl models.Workload) validator.Result {
	runtimeOpts := validator.RuntimeOptionsFromEnv()
	runtimeOpts.TargetVersion = wl.Version
	if runtimeOpts.TargetVersion != "" {
		runtimeOpts.TargetVersionSource = "workload"
	}
	return validator.ValidateWithRuntime(r.Context(), body, wl.AvailableComponents, runtimeOpts)
}

func (a *API) persistAndPushApprovalConfig(r *http.Request, workloadID string, body []byte) (string, error) {
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	pushedBy := currentUserEmail(r)
	_ = a.db.CreateConfig(models.Config{ID: hash, Name: fmt.Sprintf("push-%s", hash[:8]), Content: string(body), CreatedAt: time.Now().UTC(), CreatedBy: pushedBy})
	submittedAt := time.Now().UTC()
	if err := a.db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: workloadID, ConfigID: hash, AppliedAt: submittedAt, SubmittedAt: submittedAt, Status: models.PushStatusSubmitted, PushedBy: pushedBy, InstanceStatuses: initialPushInstanceStatuses(hash, submittedAt, a.opamp.Instances(workloadID))}); err != nil {
		return hash, fmt.Errorf("record push: %w", err)
	}
	if err := a.opamp.PushConfig(r.Context(), workloadID, body, ""); err != nil {
		sanitized := models.SanitizeRemoteConfigErrorMessage(err.Error())
		_ = a.db.UpdateWorkloadConfigStatus(workloadID, hash, models.PushStatusFailed, sanitized)
		return hash, errors.New(sanitized)
	}
	_ = a.db.MarkWorkloadConfigSent(workloadID, hash, time.Now().UTC())
	return hash, nil
}

func currentUserEmail(r *http.Request) string {
	if info := ext.UserInfoFromContext(r.Context()); info != nil {
		return info.Email
	}
	return ""
}

func isProdTarget(targetGroup, targetEnv string) bool {
	group := strings.ToLower(strings.TrimSpace(targetGroup))
	env := strings.ToLower(strings.TrimSpace(targetEnv))
	return env == "prod" || env == "production" || strings.Contains(group, "prod")
}

func respondConfigApprovalLookupError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		respondError(w, http.StatusNotFound, "approval request not found")
		return
	}
	respondError(w, http.StatusInternalServerError, "failed to load approval request")
}

func returnPushApprovalError(w http.ResponseWriter, _ *API, _ string, _ string, err error) {
	msg := err.Error()
	if strings.HasPrefix(msg, "record push:") {
		respondError(w, http.StatusInternalServerError, "failed to record push")
		return
	}
	respondError(w, http.StatusBadGateway, msg)
}
