package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/magnify-labs/otel-magnify/internal/audit"
	"github.com/magnify-labs/otel-magnify/internal/opamp"
	"github.com/magnify-labs/otel-magnify/internal/validator"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func (a *API) handleListWorkloads(w http.ResponseWriter, r *http.Request) {
	includeArchived := r.URL.Query().Get("include_archived") == "true"
	items, err := a.db.ListWorkloads(includeArchived)
	if err != nil {
		respondError(w, 500, "failed to list workloads")
		return
	}
	respondJSON(w, 200, items)
}

func (a *API) handleGetWorkload(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	wl, err := a.db.GetWorkload(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondError(w, 404, "workload not found")
			return
		}
		respondError(w, 500, "failed to get workload")
		return
	}
	respondJSON(w, 200, wl)
}

// handleListWorkloadInstances returns the live in-memory instance snapshot
// for a workload. The registry lives in the OpAMP server — when it is not
// wired (e.g. tests that stub opamp=nil) we return an empty array so the
// frontend still sees a well-formed response.
func (a *API) handleListWorkloadInstances(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if a.opamp == nil {
		respondJSON(w, 200, []opamp.Instance{})
		return
	}
	instances := a.opamp.Instances(id)
	if instances == nil {
		instances = []opamp.Instance{}
	}
	respondJSON(w, 200, instances)
}

func (a *API) handleListWorkloadEvents(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	limit := 100
	if ls := r.URL.Query().Get("limit"); ls != "" {
		if n, err := strconv.Atoi(ls); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	var since time.Time
	if ss := r.URL.Query().Get("since"); ss != "" {
		if t, err := time.Parse(time.RFC3339, ss); err == nil {
			since = t
		}
	}
	events, err := a.db.ListWorkloadEvents(id, limit, since)
	if err != nil {
		respondError(w, 500, "failed to list events")
		return
	}
	if events == nil {
		events = []models.WorkloadEvent{}
	}
	respondJSON(w, 200, events)
}

// handleWorkloadEventsStats aggregates event counts over a rolling window
// (default 24h). Caps the scan at 5000 rows — enough for any realistic
// workload at our event rates and bounds worst-case latency.
func (a *API) handleWorkloadEventsStats(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	window := 24 * time.Hour
	if ws := r.URL.Query().Get("window"); ws != "" {
		if d, err := time.ParseDuration(ws); err == nil && d > 0 {
			window = d
		}
	}
	since := time.Now().UTC().Add(-window)
	events, err := a.db.ListWorkloadEvents(id, 5000, since)
	if err != nil {
		respondError(w, 500, "failed to compute stats")
		return
	}
	var connected, disconnected, versionChanged int
	for _, e := range events {
		switch e.EventType {
		case "connected":
			connected++
		case "disconnected":
			disconnected++
		case "version_changed":
			versionChanged++
		}
	}
	churnRate := float64(disconnected) / window.Hours()
	respondJSON(w, 200, map[string]any{
		"connected":           connected,
		"disconnected":        disconnected,
		"version_changed":     versionChanged,
		"churn_rate_per_hour": churnRate,
	})
}

func (a *API) handlePushWorkloadConfig(w http.ResponseWriter, r *http.Request) {
	workloadID := chi.URLParam(r, "id")

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		respondError(w, 400, "failed to read body")
		return
	}
	//nolint:errcheck // deferred cleanup of fully-read request body; net/http server also closes it
	defer r.Body.Close()

	if len(body) == 0 {
		respondError(w, 400, "empty config body")
		return
	}

	if a.opamp == nil {
		respondError(w, 503, "OpAMP server not available")
		return
	}

	// Load the workload once: we need both the capability flag (gate) and
	// AvailableComponents (validation). Treat sql.ErrNoRows as "unknown
	// workload" — the OpAMP push below will return a clearer "not connected"
	// error in that case.
	wl, err := a.db.GetWorkload(workloadID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		respondError(w, 500, "failed to load workload")
		return
	}
	if err == nil && !wl.AcceptsRemoteConfig {
		respondJSON(w, http.StatusConflict, map[string]string{
			"error": "workload does not accept remote config",
			"code":  "remote_config_unsupported",
		})
		return
	}

	// Safety net: refuse to push a config that fails light validation.
	// The frontend should call /validate first for UX feedback; this blocks
	// API-level bypass.
	var available *models.AvailableComponents
	if wl.AvailableComponents != nil {
		available = wl.AvailableComponents
	}
	if result := validator.Validate(body, available); !result.Valid {
		respondJSON(w, 400, map[string]any{
			"error":             "configuration failed validation",
			"validation_errors": result.Errors,
		})
		return
	}

	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])

	pushedBy := ""
	if info := ext.UserInfoFromContext(r.Context()); info != nil {
		pushedBy = info.Email
	}

	// Persist the config (dedup by hash). Ignore errors on duplicate hash —
	// if the row is genuinely missing, the RecordWorkloadConfig FK would fail
	// below.
	_ = a.db.CreateConfig(models.Config{
		ID:        hash,
		Name:      fmt.Sprintf("push-%s", hash[:8]),
		Content:   string(body),
		CreatedAt: time.Now().UTC(),
		CreatedBy: pushedBy,
	})

	if err := a.db.RecordWorkloadConfig(models.WorkloadConfig{
		WorkloadID: workloadID,
		ConfigID:   hash,
		Status:     "pending",
		PushedBy:   pushedBy,
	}); err != nil {
		respondError(w, 500, "failed to record push")
		return
	}

	if err := a.opamp.PushConfig(r.Context(), workloadID, body, ""); err != nil {
		_ = a.db.UpdateWorkloadConfigStatus(workloadID, hash, "failed", err.Error())
		respondError(w, 502, err.Error())
		return
	}

	if err := audit.Emit(r.Context(), a.audit, "config.push", "workload", workloadID, hash); err != nil {
		respondAuditUnavailable(w, sideEffectApplied)
		return
	}
	respondJSON(w, 202, map[string]string{
		"status":      "config push initiated",
		"config_hash": hash,
	})
}

// handleValidateWorkloadConfig runs the light validator against a candidate
// YAML for a workload, using the workload's reported AvailableComponents when
// present. Always returns 200 with a Result body — the client inspects
// result.valid.
func (a *API) handleValidateWorkloadConfig(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		respondError(w, 400, "failed to read body")
		return
	}
	//nolint:errcheck // deferred cleanup of fully-read request body; net/http server also closes it
	defer r.Body.Close()
	if len(body) == 0 {
		respondError(w, 400, "empty config body")
		return
	}

	var available *models.AvailableComponents
	if wl, err := a.db.GetWorkload(id); err == nil {
		available = wl.AvailableComponents
	} else if !errors.Is(err, sql.ErrNoRows) {
		respondError(w, 500, "failed to load workload")
		return
	}

	respondJSON(w, 200, validator.Validate(body, available))
}

func (a *API) handleGetWorkloadConfigHistory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	history, err := a.db.GetWorkloadConfigHistory(id)
	if err != nil {
		respondError(w, 500, "failed to get config history")
		return
	}
	respondJSON(w, 200, history)
}

// handleGetWorkloadConfigByHash returns a single past push of a config to the
// workload, joined with the YAML content. Used by ConfigCompareDialog to
// fetch arbitrary revisions for diffing.
func (a *API) handleGetWorkloadConfigByHash(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	hash := chi.URLParam(r, "hash")

	wc, err := a.db.GetWorkloadConfigByHash(id, hash)
	if err != nil {
		respondError(w, 500, "failed to get config")
		return
	}
	if wc == nil {
		respondError(w, 404, "config not found in this workload's history")
		return
	}
	respondJSON(w, 200, wc)
}

type setLabelRequest struct {
	Label string `json:"label"`
}

// handleSetWorkloadConfigLabel attaches (or clears, when label == "") a
// human-readable label to a past revision. Operators use this from the push
// history table to mark specific hashes as "stable", "before audit", etc.
// Emits a config.label audit event regardless of community vs EE — the sink
// is NopAuditLogger by default; EE wires a real one via WithAuditLogger.
func (a *API) handleSetWorkloadConfigLabel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	hash := chi.URLParam(r, "hash")

	var req setLabelRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil {
		respondError(w, 400, "invalid JSON body")
		return
	}
	// Trim user input but keep "" as the explicit clear signal — the store
	// turns it into SQL NULL.
	label := strings.TrimSpace(req.Label)
	if len(label) > 128 {
		respondError(w, 400, "label too long (max 128 chars)")
		return
	}

	if err := a.db.SetWorkloadConfigLabel(id, hash, label); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondError(w, 404, "config not found in this workload's history")
			return
		}
		respondError(w, 500, "failed to set label")
		return
	}

	if err := audit.Emit(r.Context(), a.audit, "config.label", "workload", id, label); err != nil {
		respondAuditUnavailable(w, sideEffectApplied)
		return
	}
	respondJSON(w, 200, map[string]string{"label": label})
}

// handleRollbackWorkloadConfig re-pushes the YAML of the given hash through
// the same pipeline as a fresh push (validation, RecordWorkloadConfig with
// status=pending, opamp.PushConfig). The new history row carries a fresh
// timestamp and pushed_by — rollback is observable as a normal push, just
// re-using past content.
func (a *API) handleRollbackWorkloadConfig(w http.ResponseWriter, r *http.Request) {
	workloadID := chi.URLParam(r, "id")
	hash := chi.URLParam(r, "hash")

	if a.opamp == nil {
		respondError(w, 503, "OpAMP server not available")
		return
	}

	wc, err := a.db.GetWorkloadConfigByHash(workloadID, hash)
	if err != nil {
		respondError(w, 500, "failed to load past config")
		return
	}
	if wc == nil {
		respondError(w, 404, "config not found in this workload's history")
		return
	}
	if wc.Content == "" {
		// History rows JOIN configs.content; an empty string means the
		// underlying configs row is missing. Refuse to rollback to a
		// body we cannot reconstruct.
		respondError(w, 410, "config content is no longer available")
		return
	}

	wl, err := a.db.GetWorkload(workloadID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		respondError(w, 500, "failed to load workload")
		return
	}
	if err == nil && !wl.AcceptsRemoteConfig {
		respondJSON(w, http.StatusConflict, map[string]string{
			"error": "workload does not accept remote config",
			"code":  "remote_config_unsupported",
		})
		return
	}

	body := []byte(wc.Content)

	var available *models.AvailableComponents
	if wl.AvailableComponents != nil {
		available = wl.AvailableComponents
	}
	// Re-validate against the workload's *current* AvailableComponents
	// rather than trusting that the past hash is still semantically valid.
	// A rollback to a config that referenced a now-uninstalled exporter
	// must fail loudly, not silently break the agent.
	if result := validator.Validate(body, available); !result.Valid {
		respondJSON(w, 400, map[string]any{
			"error":             "configuration failed validation",
			"validation_errors": result.Errors,
		})
		return
	}

	pushedBy := ""
	if info := ext.UserInfoFromContext(r.Context()); info != nil {
		pushedBy = info.Email
	}

	if err := a.db.RecordWorkloadConfig(models.WorkloadConfig{
		WorkloadID: workloadID,
		ConfigID:   hash,
		Status:     "pending",
		PushedBy:   pushedBy,
	}); err != nil {
		respondError(w, 500, "failed to record push")
		return
	}

	if err := a.opamp.PushConfig(r.Context(), workloadID, body, ""); err != nil {
		_ = a.db.UpdateWorkloadConfigStatus(workloadID, hash, "failed", err.Error())
		respondError(w, 502, err.Error())
		return
	}

	if err := audit.Emit(r.Context(), a.audit, "config.rollback", "workload", workloadID, hash); err != nil {
		respondAuditUnavailable(w, sideEffectApplied)
		return
	}
	respondJSON(w, 202, map[string]string{
		"status":      "rollback initiated",
		"config_hash": hash,
	})
}

func (a *API) handleDeleteWorkload(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.db.DeleteWorkload(id); err != nil {
		respondError(w, 500, "failed to delete workload")
		return
	}
	if err := audit.Emit(r.Context(), a.audit, "workload.delete", "workload", id, ""); err != nil {
		respondAuditUnavailable(w, sideEffectApplied)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// redirectAgentsToWorkloads rewrites /api/agents/... → /api/workloads/... and
// emits a 307 so the client re-sends the request (keeping the method + body).
// This is a transitional shim for frontends or scripts still on the old URL
// shape — slated for removal at the next minor release.
func redirectAgentsToWorkloads(w http.ResponseWriter, r *http.Request) {
	// Validate the *decoded* path so that percent-encoded bypasses (e.g.
	// `/%5Cevil.com/...` which decodes to `/\evil.com/...`) are caught.
	// `RequestURI()` keeps the encoded form, which would slip past a literal
	// HasPrefix check on `\`. Browsers resolve `//foo` and `/\foo` (after
	// normalisation) as absolute URLs — both must be rejected.
	p := r.URL.Path
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.HasPrefix(p, `/\`) {
		respondError(w, http.StatusBadRequest, "invalid path")
		return
	}
	target := strings.Replace(r.URL.RequestURI(), "/api/agents", "/api/workloads", 1)
	// gosec G710 (taint analysis) does not recognise the prefix guard above
	// as sanitisation, so target is still flagged as user-tainted. The guard
	// is sufficient — TestLegacyAgentsRedirect_RejectsProtocolRelativePath
	// covers the bypass attempt.
	http.Redirect(w, r, target, http.StatusTemporaryRedirect) //nolint:gosec // G710 false positive
}
