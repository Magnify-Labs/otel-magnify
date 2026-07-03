package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/magnify-labs/otel-magnify/internal/audit"
	"github.com/magnify-labs/otel-magnify/internal/opamp"
	"github.com/magnify-labs/otel-magnify/internal/validator"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

const canaryHeartbeatFreshness = 2 * time.Minute

type canaryRequest struct {
	Config    string                 `json:"config"`
	Selection models.CanarySelection `json:"selection"`
}

func (a *API) handleValidateCanary(w http.ResponseWriter, r *http.Request) {
	req, body, ok := readCanaryRequest(w, r)
	if !ok {
		return
	}
	result, _, _ := a.validateCanaryRequest(r, chi.URLParam(r, "id"), req, body)
	if !result.Valid {
		respondJSON(w, canaryHTTPStatus(result), result)
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func (a *API) handleStartCanary(w http.ResponseWriter, r *http.Request) {
	workloadID := chi.URLParam(r, "id")
	req, body, ok := readCanaryRequest(w, r)
	if !ok {
		return
	}
	result, wl, available := a.validateCanaryRequest(r, workloadID, req, body)
	_ = available
	if !result.Valid {
		respondJSON(w, canaryHTTPStatus(result), result)
		return
	}
	hash := canarySHA256Hex(body)
	actor := userEmail(r)
	_ = a.db.CreateConfig(models.Config{ID: hash, Name: fmt.Sprintf("canary-%s", hash[:8]), Content: string(body), CreatedAt: time.Now().UTC(), CreatedBy: actor})

	now := time.Now().UTC()
	status := models.CanaryStatus{ID: newCanaryID(), WorkloadID: workloadID, ConfigHash: hash, Status: models.CanaryStatusRunning, Selection: req.Selection, Targets: result.Targets, Actor: actor, CreatedAt: now, UpdatedAt: now}
	for i := range status.Targets {
		status.Targets[i].Status = models.InstanceStatusSent
		status.Targets[i].UpdatedAt = now
	}
	if err := a.db.CreateCanaryStatus(status); err != nil {
		respondError(w, 500, "failed to record canary")
		return
	}
	for _, target := range status.Targets {
		if err := a.opamp.PushConfig(r.Context(), workloadID, body, target.InstanceUID); err != nil {
			sanitized := models.SanitizeRemoteConfigErrorMessage(err.Error())
			markCanaryTargetFailed(&status, target.InstanceUID, models.CanaryStopRemoteConfigFailed)
			status.Status = models.CanaryStatusStopped
			_ = a.db.UpdateCanaryStatus(status)
			respondError(w, http.StatusBadGateway, sanitized)
			return
		}
	}
	if err := audit.Emit(r.Context(), a.audit, "config.canary.start", "workload", workloadID, canaryAuditDetail(status.ID, hash, status.Targets)); err != nil {
		respondAuditUnavailable(w, sideEffectApplied)
		return
	}
	_ = wl
	status.Recount()
	respondJSON(w, http.StatusAccepted, status)
}

func (a *API) handleGetCanary(w http.ResponseWriter, r *http.Request) {
	status, err := a.loadCanaryWithHealth(chi.URLParam(r, "id"), chi.URLParam(r, "canary_id"))
	if err != nil {
		respondError(w, 500, "failed to load canary")
		return
	}
	if status == nil {
		respondError(w, 404, "canary not found")
		return
	}
	respondJSON(w, http.StatusOK, status)
}

func (a *API) handlePromoteCanary(w http.ResponseWriter, r *http.Request) {
	workloadID := chi.URLParam(r, "id")
	status, err := a.loadCanaryWithHealth(workloadID, chi.URLParam(r, "canary_id"))
	if err != nil {
		respondError(w, 500, "failed to load canary")
		return
	}
	if status == nil {
		respondError(w, 404, "canary not found")
		return
	}
	if status.Status != models.CanaryStatusSucceeded {
		respondJSON(w, http.StatusConflict, map[string]any{"error": "canary is not in a promotable state", "canary": status})
		return
	}
	if status.Counts.Failed > 0 || len(status.StopReasons) > 0 {
		respondJSON(w, http.StatusConflict, status)
		return
	}
	if status.Counts.Applied != len(status.Targets) {
		respondJSON(w, http.StatusConflict, map[string]any{"error": "canary has not succeeded", "canary": status})
		return
	}
	cfg, err := a.db.GetConfig(status.ConfigHash)
	if err != nil {
		respondError(w, 500, "failed to load canary config")
		return
	}
	selected := map[string]bool{}
	for _, t := range status.Targets {
		selected[t.InstanceUID] = true
	}
	remaining := eligibleRemainingTargets(a.opamp.Instances(workloadID), selected)
	for _, inst := range remaining {
		if err := a.opamp.PushConfig(r.Context(), workloadID, []byte(cfg.Content), inst.InstanceUID); err != nil {
			markCanaryStop(status, models.CanaryStopRemoteConfigFailed)
			status.Status = models.CanaryStatusStopped
			_ = a.db.UpdateCanaryStatus(*status)
			respondError(w, http.StatusBadGateway, models.SanitizeRemoteConfigErrorMessage(err.Error()))
			return
		}
	}
	now := time.Now().UTC()
	status.PromotedAt = &now
	status.UpdatedAt = now
	status.Status = models.CanaryStatusPromoted
	_ = a.db.UpdateCanaryStatus(*status)
	if err := audit.Emit(r.Context(), a.audit, "config.canary.promote", "workload", workloadID, status.ID+":"+status.ConfigHash); err != nil {
		respondAuditUnavailable(w, sideEffectApplied)
		return
	}
	respondJSON(w, http.StatusAccepted, status)
}

func (a *API) handleAbortCanary(w http.ResponseWriter, r *http.Request) {
	workloadID := chi.URLParam(r, "id")
	status, err := a.db.GetCanaryStatus(workloadID, chi.URLParam(r, "canary_id"))
	if err != nil {
		respondError(w, 500, "failed to load canary")
		return
	}
	if status == nil {
		respondError(w, 404, "canary not found")
		return
	}
	now := time.Now().UTC()
	status.AbortedAt = &now
	status.UpdatedAt = now
	status.Status = models.CanaryStatusAborted
	_ = a.db.UpdateCanaryStatus(*status)
	if err := audit.Emit(r.Context(), a.audit, "config.canary.abort", "workload", workloadID, status.ID+":"+status.ConfigHash); err != nil {
		respondAuditUnavailable(w, sideEffectApplied)
		return
	}
	respondJSON(w, http.StatusOK, status)
}

func (a *API) handleRollbackCanary(w http.ResponseWriter, r *http.Request) {
	workloadID := chi.URLParam(r, "id")
	status, err := a.db.GetCanaryStatus(workloadID, chi.URLParam(r, "canary_id"))
	if err != nil {
		respondError(w, 500, "failed to load canary")
		return
	}
	if status == nil {
		respondError(w, 404, "canary not found")
		return
	}
	cfg, err := a.rollbackConfigForCanary(workloadID, status.ConfigHash)
	if err != nil {
		respondJSON(w, http.StatusConflict, map[string]string{"error": "no rollback target available"})
		return
	}
	for _, target := range status.Targets {
		if err := a.opamp.PushConfig(r.Context(), workloadID, []byte(cfg.Content), target.InstanceUID); err != nil {
			markCanaryStop(status, models.CanaryStopRemoteConfigFailed)
			_ = a.db.UpdateCanaryStatus(*status)
			respondError(w, http.StatusBadGateway, models.SanitizeRemoteConfigErrorMessage(err.Error()))
			return
		}
	}
	now := time.Now().UTC()
	status.RolledBackAt = &now
	status.UpdatedAt = now
	status.Status = models.CanaryStatusRollback
	_ = a.db.UpdateCanaryStatus(*status)
	if err := audit.Emit(r.Context(), a.audit, "config.canary.rollback", "workload", workloadID, status.ID+":"+cfg.ConfigID); err != nil {
		respondAuditUnavailable(w, sideEffectApplied)
		return
	}
	respondJSON(w, http.StatusAccepted, status)
}

func readCanaryRequest(w http.ResponseWriter, r *http.Request) (canaryRequest, []byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		respondError(w, 400, "failed to read body")
		return canaryRequest{}, nil, false
	}
	defer r.Body.Close() //nolint:errcheck
	var req canaryRequest
	if err := json.Unmarshal(body, &req); err != nil {
		respondError(w, 400, "invalid json body")
		return req, nil, false
	}
	if strings.TrimSpace(req.Config) == "" {
		respondError(w, 400, "empty config body")
		return req, nil, false
	}
	return req, []byte(req.Config), true
}

func (a *API) validateCanaryRequest(r *http.Request, workloadID string, req canaryRequest, body []byte) (models.CanaryValidationResult, models.Workload, *models.AvailableComponents) {
	if a.opamp == nil {
		return invalidCanary("OpAMP server not available", ""), models.Workload{}, nil
	}
	wl, err := a.db.GetWorkload(workloadID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return invalidCanary("failed to load workload", ""), wl, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return invalidCanary("workload not found", ""), wl, nil
	}
	if !wl.AcceptsRemoteConfig {
		return invalidCanary("workload does not accept remote config", "remote_config_unsupported"), wl, nil
	}
	var available *models.AvailableComponents
	if wl.AvailableComponents != nil {
		available = wl.AvailableComponents
	}
	runtimeOpts := validator.RuntimeOptionsFromEnv()
	runtimeOpts.TargetVersion = wl.Version
	if runtimeOpts.TargetVersion != "" {
		runtimeOpts.TargetVersionSource = "workload"
	}
	if result := validator.ValidateWithRuntime(r.Context(), body, available, runtimeOpts); !result.Valid {
		return models.CanaryValidationResult{Valid: false, Errors: append([]string{"configuration failed validation"}, validationMessages(result.Errors)...)}, wl, available
	}
	targets, stop, errors, errorCodes := a.selectCanaryTargets(wl, req.Selection)
	return models.CanaryValidationResult{Valid: len(errors) == 0, Targets: targets, StopReasons: stop, ErrorCodes: errorCodes, Errors: errors}, wl, available
}

func (a *API) selectCanaryTargets(wl models.Workload, selection models.CanarySelection) ([]models.CanaryTarget, []string, []string, []string) {
	instances := a.opamp.Instances(wl.ID)
	sort.Slice(instances, func(i, j int) bool { return instances[i].InstanceUID < instances[j].InstanceUID })
	byUID := map[string]opamp.Instance{}
	for _, inst := range instances {
		byUID[inst.InstanceUID] = inst
	}
	var selected []opamp.Instance
	var errs []string
	var errorCodes []string
	switch selection.Strategy {
	case "one":
		if strings.TrimSpace(selection.InstanceUID) == "" {
			return nil, nil, []string{"instance_uid is required"}, []string{"invalid_instance_target"}
		}
		if inst, ok := byUID[selection.InstanceUID]; ok {
			selected = append(selected, inst)
		} else if boundWorkloadID, found := a.opamp.InstanceWorkload(selection.InstanceUID); found && boundWorkloadID != wl.ID {
			return nil, nil, []string{"instance target belongs to a different workload: " + selection.InstanceUID}, []string{"instance_target_cross_workload"}
		} else {
			return nil, nil, []string{"instance target is not connected: " + selection.InstanceUID}, []string{"instance_target_not_connected"}
		}
	case "count", "n":
		n := selection.Count
		if n <= 0 {
			return nil, nil, []string{"count must be positive"}, nil
		}
		if n >= len(instances) {
			return nil, nil, []string{"canary target count must be smaller than eligible instances"}, nil
		}
		selected = append(selected, instances[:n]...)
	case "percentage":
		if selection.Percentage <= 0 {
			return nil, nil, []string{"percentage must be positive"}, nil
		}
		if selection.Percentage >= 100 {
			return nil, nil, []string{"percentage must be less than 100"}, nil
		}
		n := int(math.Ceil(float64(len(instances)) * float64(selection.Percentage) / 100))
		if n < 1 && len(instances) > 0 {
			n = 1
		}
		if n > 0 {
			selected = append(selected, instances[:n]...)
		}
	case "label_selector":
		if labelsMatch(wl.Labels, selection.Labels) {
			selected = instances
		}
	case "instances":
		seen := map[string]bool{}
		for _, uid := range selection.InstanceUIDs {
			if inst, ok := byUID[uid]; ok && !seen[uid] {
				selected = append(selected, inst)
				seen[uid] = true
			} else if uid != "" {
				if boundWorkloadID, found := a.opamp.InstanceWorkload(uid); found && boundWorkloadID != wl.ID {
					errs = append(errs, "instance target belongs to a different workload: "+uid)
					errorCodes = appendUnique(errorCodes, "instance_target_cross_workload")
				} else {
					errs = append(errs, "instance target is not connected: "+uid)
					errorCodes = appendUnique(errorCodes, "instance_target_not_connected")
				}
			}
		}
	default:
		return nil, nil, []string{"unsupported selection strategy"}, nil
	}
	if len(errs) > 0 {
		return nil, nil, errs, errorCodes
	}
	if len(selected) == 0 {
		return nil, nil, []string{"canary target set is empty"}, nil
	}
	if len(selected) >= len(instances) {
		return nil, nil, []string{"canary target set must be smaller than eligible instances"}, nil
	}
	var targets []models.CanaryTarget
	var reasons []string
	now := time.Now().UTC()
	for _, inst := range selected {
		t := models.CanaryTarget{InstanceUID: inst.InstanceUID, PodName: inst.PodName, Status: models.InstanceStatusSent, UpdatedAt: now}
		if !inst.Healthy {
			t.StopReason = models.CanaryStopCollectorDegraded
			reasons = appendUnique(reasons, t.StopReason)
			errs = append(errs, "collector degraded: "+inst.InstanceUID)
		}
		if inst.LastMessageAt.IsZero() || time.Since(inst.LastMessageAt) > canaryHeartbeatFreshness {
			t.StopReason = models.CanaryStopNoHeartbeat
			reasons = appendUnique(reasons, t.StopReason)
			errs = append(errs, "stale heartbeat: "+inst.InstanceUID)
		}
		targets = append(targets, t)
	}
	return targets, reasons, errs, errorCodes
}

func (a *API) loadCanaryWithHealth(workloadID, canaryID string) (*models.CanaryStatus, error) {
	status, err := a.db.GetCanaryStatus(workloadID, canaryID)
	if err != nil || status == nil {
		return status, err
	}
	changed := false
	instances := a.opamp.Instances(workloadID)
	byUID := map[string]opamp.Instance{}
	for _, inst := range instances {
		byUID[inst.InstanceUID] = inst
	}
	for i := range status.Targets {
		inst, ok := byUID[status.Targets[i].InstanceUID]
		if !ok || inst.LastMessageAt.IsZero() || time.Since(inst.LastMessageAt) > canaryHeartbeatFreshness {
			status.Targets[i].StopReason = models.CanaryStopNoHeartbeat
			changed = true
		}
		if ok && !inst.Healthy {
			status.Targets[i].StopReason = models.CanaryStopCollectorDegraded
			changed = true
		}
		// Config drift detection is an extension point for real OpAMP status reconciliation.
		// The in-memory instance snapshot can still report the pre-canary hash immediately
		// after a manual test status update, so do not stop solely on this cache here.
	}
	if alert, _ := a.db.GetUnresolvedAlertByWorkloadAndRule(workloadID, "canary_stop"); alert != nil {
		markCanaryStop(status, models.CanaryStopAlertTriggered)
		changed = true
	}
	status.Recount()
	if len(status.StopReasons) > 0 && status.Status == models.CanaryStatusRunning {
		status.Status = models.CanaryStatusStopped
		changed = true
	}
	if changed {
		status.UpdatedAt = time.Now().UTC()
		_ = a.db.UpdateCanaryStatus(*status)
		_ = audit.Emit(context.Background(), a.audit, "config.canary.stopped", "workload", workloadID, status.ID+":"+strings.Join(status.StopReasons, ","))
	}
	return status, nil
}

func (a *API) rollbackConfigForCanary(workloadID, excludeHash string) (*models.WorkloadConfig, error) {
	target, err := a.db.GetRollbackTarget(workloadID, excludeHash)
	if err != nil {
		return nil, err
	}
	if target == nil || target.Config.Content == "" {
		return nil, errors.New("no rollback target available")
	}
	return &target.Config, nil
}

func eligibleRemainingTargets(instances []opamp.Instance, selected map[string]bool) []opamp.Instance {
	var out []opamp.Instance
	for _, inst := range instances {
		if !selected[inst.InstanceUID] && inst.Healthy && !inst.LastMessageAt.IsZero() && time.Since(inst.LastMessageAt) <= canaryHeartbeatFreshness {
			out = append(out, inst)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].InstanceUID < out[j].InstanceUID })
	return out
}

func canaryHTTPStatus(result models.CanaryValidationResult) int {
	for _, code := range result.ErrorCodes {
		if code == "instance_target_not_connected" || code == "instance_target_cross_workload" || code == "remote_config_unsupported" {
			return http.StatusConflict
		}
	}
	for _, reason := range result.StopReasons {
		if reason == models.CanaryStopCollectorDegraded || reason == models.CanaryStopNoHeartbeat || reason == "remote_config_unsupported" {
			return http.StatusConflict
		}
	}
	for _, err := range result.Errors {
		if strings.Contains(err, "degraded") || strings.Contains(err, "stale") || strings.Contains(err, "remote config") {
			return http.StatusConflict
		}
	}
	return http.StatusBadRequest
}
func invalidCanary(msg, reason string) models.CanaryValidationResult {
	r := models.CanaryValidationResult{Valid: false, Errors: []string{msg}}
	if reason != "" {
		r.StopReasons = []string{reason}
		r.ErrorCodes = []string{reason}
	}
	return r
}
func canaryAuditDetail(canaryID, hash string, targets []models.CanaryTarget) string {
	uids := make([]string, 0, len(targets))
	for _, target := range targets {
		uids = append(uids, target.InstanceUID)
	}
	sort.Strings(uids)
	if len(uids) == 0 {
		return canaryID + ":" + hash
	}
	return fmt.Sprintf("%s:%s instance_uids=%s", canaryID, hash, strings.Join(uids, ","))
}
func canarySHA256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
func userEmail(r *http.Request) string {
	if info := ext.UserInfoFromContext(r.Context()); info != nil {
		return info.Email
	}
	return ""
}
func labelsMatch(have models.Labels, want map[string]string) bool {
	if len(want) == 0 {
		return false
	}
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}
func appendUnique(items []string, item string) []string {
	for _, existing := range items {
		if existing == item {
			return items
		}
	}
	return append(items, item)
}
func markCanaryTargetFailed(status *models.CanaryStatus, uid, reason string) {
	for i := range status.Targets {
		if status.Targets[i].InstanceUID == uid {
			status.Targets[i].Status = models.PushStatusFailed
			status.Targets[i].StopReason = reason
			status.Targets[i].UpdatedAt = time.Now().UTC()
		}
	}
	status.Recount()
}
func markCanaryStop(status *models.CanaryStatus, reason string) {
	status.StopReasons = appendUnique(status.StopReasons, reason)
	status.Status = models.CanaryStatusStopped
	status.UpdatedAt = time.Now().UTC()
}
func newCanaryID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("canary-%d", time.Now().UnixNano())
	}
	return "canary_" + hex.EncodeToString(b[:])
}
func validationMessages(errs []validator.Error) []string {
	out := make([]string, 0, len(errs))
	for _, e := range errs {
		out = append(out, e.Message)
	}
	return out
}
