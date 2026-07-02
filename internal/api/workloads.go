package api

import (
	"context"
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
	"github.com/magnify-labs/otel-magnify/internal/oteldiff"
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
	for i := range items {
		a.hydrateCurrentConfigPush(&items[i])
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
	a.hydrateCurrentConfigPush(&wl)
	respondJSON(w, 200, wl)
}

func (a *API) hydrateCurrentConfigPush(wl *models.Workload) {
	if a == nil || a.db == nil || wl == nil {
		return
	}
	if push, err := a.db.GetLatestWorkloadConfig(wl.ID); err == nil && push != nil {
		wl.CurrentConfigPush = push
	}
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

func initialPushInstanceStatuses(hash string, at time.Time, instances []opamp.Instance) []models.WorkloadConfigInstanceStatus {
	out := make([]models.WorkloadConfigInstanceStatus, 0, len(instances))
	for _, inst := range instances {
		out = append(out, models.WorkloadConfigInstanceStatus{
			InstanceUID: inst.InstanceUID,
			PodName:     inst.PodName,
			Required:    true,
			Status:      models.InstanceStatusSent,
			ConfigHash:  hash,
			UpdatedAt:   at,
		})
	}
	return out
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
	if err == nil && !workloadIsConnected(wl) {
		respondJSON(w, http.StatusConflict, map[string]string{
			"error": "workload is not connected",
			"code":  "workload_not_connected",
		})
		return
	}

	// Safety net: refuse to push a config that fails light validation.
	// The frontend should call /validate first for UX feedback; this blocks
	// API-level bypass.
	var available *models.AvailableComponents
	if err == nil && wl.AvailableComponents != nil {
		available = wl.AvailableComponents
	}
	runtimeOpts := validator.RuntimeOptionsFromEnv()
	if err == nil {
		runtimeOpts.TargetVersion = wl.Version
	}
	if runtimeOpts.TargetVersion != "" {
		runtimeOpts.TargetVersionSource = "workload"
	}
	if result := validator.ValidateWithRuntime(r.Context(), body, available, runtimeOpts); !result.Valid {
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

	submittedAt := time.Now().UTC()
	if err := a.db.RecordWorkloadConfig(models.WorkloadConfig{
		WorkloadID:       workloadID,
		ConfigID:         hash,
		AppliedAt:        submittedAt,
		SubmittedAt:      submittedAt,
		Status:           models.PushStatusSubmitted,
		PushedBy:         pushedBy,
		InstanceStatuses: initialPushInstanceStatuses(hash, submittedAt, a.opamp.Instances(workloadID)),
	}); err != nil {
		respondError(w, 500, "failed to record push")
		return
	}

	if err := a.opamp.PushConfig(r.Context(), workloadID, body, ""); err != nil {
		sanitized := models.SanitizeRemoteConfigErrorMessage(err.Error())
		_ = a.db.UpdateWorkloadConfigStatus(workloadID, hash, models.PushStatusFailed, sanitized)
		respondError(w, 502, sanitized)
		return
	}
	_ = a.db.MarkWorkloadConfigSent(workloadID, hash, time.Now().UTC())

	if err := audit.Emit(r.Context(), a.audit, "config.push", "workload", workloadID, hash); err != nil {
		respondAuditUnavailable(w, sideEffectApplied)
		return
	}
	push, _ := a.db.GetLatestWorkloadConfig(workloadID)
	respondJSON(w, 202, push)
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
	targetVersion := strings.TrimSpace(r.URL.Query().Get("target_collector_version"))
	versionSource := ""
	if targetVersion != "" {
		versionSource = "request"
	}
	if wl, err := a.db.GetWorkload(id); err == nil {
		available = wl.AvailableComponents
		if targetVersion == "" {
			targetVersion = wl.Version
			if targetVersion != "" {
				versionSource = "workload"
			}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		respondError(w, 500, "failed to load workload")
		return
	}

	runtimeOpts := validator.RuntimeOptionsFromEnv()
	runtimeOpts.TargetVersion = targetVersion
	runtimeOpts.TargetVersionSource = versionSource
	respondJSON(w, 200, validator.ValidateWithRuntime(r.Context(), body, available, runtimeOpts))
}

func (a *API) handlePlanWorkloadConfig(w http.ResponseWriter, r *http.Request) {
	workloadID := chi.URLParam(r, "id")
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		respondError(w, 400, "failed to read body")
		return
	}
	//nolint:errcheck // deferred cleanup of fully-read request body; net/http server also closes it
	defer r.Body.Close()

	plan, err := a.buildConfigApplicationPlan(r.Context(), workloadID, body)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondError(w, 404, "workload not found")
			return
		}
		respondError(w, 500, "failed to build config application plan")
		return
	}
	respondJSON(w, http.StatusOK, plan)
}

func (a *API) handleExportWorkloadConfigPlan(w http.ResponseWriter, r *http.Request) {
	workloadID := chi.URLParam(r, "id")
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		respondError(w, 400, "failed to read body")
		return
	}
	//nolint:errcheck // deferred cleanup of fully-read request body; net/http server also closes it
	defer r.Body.Close()

	plan, err := a.buildConfigApplicationPlan(r.Context(), workloadID, body)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondError(w, 404, "workload not found")
			return
		}
		respondError(w, 500, "failed to build config application plan")
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" || format == "json" {
		respondJSON(w, http.StatusOK, plan)
		return
	}
	if format != "markdown" && format != "md" {
		respondError(w, 400, "unsupported export format")
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="config-safety-plan.md"`)
	_, _ = w.Write([]byte(renderConfigApplicationPlanMarkdown(plan)))
}

func (a *API) buildConfigApplicationPlan(ctx context.Context, workloadID string, body []byte) (models.ConfigApplicationPlan, error) {
	wl, err := a.db.GetWorkload(workloadID)
	if err != nil {
		return models.ConfigApplicationPlan{}, err
	}
	sum := sha256.Sum256(body)
	plan := models.ConfigApplicationPlan{
		SchemaVersion: "config_application_plan.v1",
		WorkloadID:    workloadID,
		ConfigHash:    hex.EncodeToString(sum[:]),
		Targets:       []models.ConfigApplicationPlanTarget{},
		HardFailures:  []string{},
		Export: models.ConfigApplicationPlanExport{
			Supported:        true,
			Formats:          []string{"json", "markdown"},
			JSONEndpoint:     fmt.Sprintf("/api/workloads/%s/config/plan/export?format=json", workloadID),
			MarkdownEndpoint: fmt.Sprintf("/api/workloads/%s/config/plan/export?format=markdown", workloadID),
			PersistedRollout: "not_persisted",
		},
	}
	if len(body) == 0 {
		plan.HardFailures = append(plan.HardFailures, "empty_config")
	}

	target := a.buildConfigApplicationPlanTarget(ctx, wl, body)
	plan.Targets = append(plan.Targets, target)
	plan.Summary.TargetCount = len(plan.Targets)
	for _, target := range plan.Targets {
		if target.Type == "collector" {
			plan.Summary.CollectorTargetCount++
		}
		if target.AcceptsRemoteConfig {
			plan.Summary.RemoteConfigCapableCount++
		}
		if target.ReadOnly {
			plan.Summary.ReadOnlyCount++
		}
		switch target.ValidationStatus {
		case "ok":
			plan.Summary.ValidationOKCount++
		case "failed":
			plan.Summary.ValidationFailedCount++
		}
		plan.Summary.ComponentsMissingCount += target.ComponentsMissingCount
		plan.Summary.HighRiskChangeCount += target.HighRiskChangeCount
		if target.Excluded {
			plan.Summary.ExcludedCount++
		}
	}
	if len(plan.Targets) == 0 || plan.Summary.ExcludedCount == len(plan.Targets) {
		plan.HardFailures = appendIfMissing(plan.HardFailures, "all_targets_excluded")
	}
	if plan.Summary.ValidationFailedCount > 0 {
		plan.HardFailures = appendIfMissing(plan.HardFailures, "validation_failed")
	}
	plan.CanPush = len(plan.HardFailures) == 0
	plan.ApplyAllowed = plan.CanPush
	return plan, nil
}

func (a *API) buildConfigApplicationPlanTarget(ctx context.Context, wl models.Workload, body []byte) models.ConfigApplicationPlanTarget {
	target := models.ConfigApplicationPlanTarget{
		WorkloadID:          wl.ID,
		DisplayName:         wl.DisplayName,
		Type:                wl.Type,
		AcceptsRemoteConfig: wl.AcceptsRemoteConfig,
		ReadOnly:            !wl.AcceptsRemoteConfig,
		ExclusionReasons:    []string{},
		HardFailures:        []string{},
		ValidationErrors:    []string{},
		ActiveConfigHash:    wl.ActiveConfigHash,
	}
	if wl.Type != "collector" {
		target.ExclusionReasons = append(target.ExclusionReasons, "non_collector")
	}
	if target.ReadOnly {
		target.ExclusionReasons = append(target.ExclusionReasons, "read_only")
		target.HardFailures = append(target.HardFailures, "read_only")
	}
	if !workloadIsConnected(wl) {
		target.ExclusionReasons = append(target.ExclusionReasons, "workload_offline")
		target.HardFailures = append(target.HardFailures, "workload_offline")
	}

	runtimeOpts := runtimeOptionsForWorkload(wl)
	validation := validator.ValidateWithRuntime(ctx, body, wl.AvailableComponents, runtimeOpts)
	if validation.Valid {
		target.ValidationStatus = "ok"
	} else {
		target.ValidationStatus = "failed"
		target.ExclusionReasons = append(target.ExclusionReasons, "validation_failed")
		target.HardFailures = append(target.HardFailures, "validation_failed")
	}
	for _, validationErr := range validation.Errors {
		target.ValidationErrors = append(target.ValidationErrors, validationErr.Code)
	}
	target.ComponentsMissingCount = countMissingComponents(validation)

	active, activeAvailable := a.activeConfigContent(wl)
	if activeAvailable {
		diff := oteldiff.Compare([]byte(active.Content), body)
		for _, item := range diff.RiskItems {
			if item.Risk == oteldiff.RiskHigh {
				target.HighRiskChangeCount++
			}
		}
	} else {
		target.ActiveConfigUnavailable = true
	}

	if len(target.ExclusionReasons) > 0 {
		target.Excluded = true
	}
	return target
}

func workloadIsConnected(wl models.Workload) bool {
	return strings.EqualFold(strings.TrimSpace(wl.Status), "connected")
}

func (a *API) activeConfigContent(wl models.Workload) (*models.WorkloadConfig, bool) {
	if wl.ActiveConfigHash != "" {
		if wc, err := a.db.GetWorkloadConfigByHash(wl.ID, wl.ActiveConfigHash); err == nil && wc != nil && wc.Content != "" {
			return wc, true
		}
	}
	if wc, err := a.db.GetLastAppliedWorkloadConfig(wl.ID); err == nil && wc != nil && wc.Content != "" {
		return wc, true
	}
	return nil, false
}

// countMissingComponents relies on the validator's component_not_installed
// code. The string fallback keeps the plan useful if an older validator only
// exposes human-readable messages for unavailable components.
func countMissingComponents(result validator.Result) int {
	count := 0
	for _, err := range result.Errors {
		if err.Code == "component_not_installed" || strings.Contains(strings.ToLower(err.Message), "not installed") {
			count++
		}
	}
	return count
}

func appendIfMissing(items []string, item string) []string {
	for _, existing := range items {
		if existing == item {
			return items
		}
	}
	return append(items, item)
}

func renderConfigApplicationPlanMarkdown(plan models.ConfigApplicationPlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Config Safety Plan\n\n")
	fmt.Fprintf(&b, "Schema version: %s\n\n", plan.SchemaVersion)
	fmt.Fprintf(&b, "Workload: %s\n\n", plan.WorkloadID)
	fmt.Fprintf(&b, "Config hash: %s\n\n", plan.ConfigHash)
	fmt.Fprintf(&b, "Can push: %t\n\n", plan.CanPush)
	fmt.Fprintf(&b, "Apply allowed: %t\n\n", plan.ApplyAllowed)
	fmt.Fprintf(&b, "Persisted rollout: not implemented\n\n")
	fmt.Fprintf(&b, "## Summary\n\n")
	fmt.Fprintf(&b, "- Targets: %d\n", plan.Summary.TargetCount)
	fmt.Fprintf(&b, "- Collectors: %d\n", plan.Summary.CollectorTargetCount)
	fmt.Fprintf(&b, "- Remote-config capable: %d\n", plan.Summary.RemoteConfigCapableCount)
	fmt.Fprintf(&b, "- Read-only: %d\n", plan.Summary.ReadOnlyCount)
	fmt.Fprintf(&b, "- Validation OK: %d\n", plan.Summary.ValidationOKCount)
	fmt.Fprintf(&b, "- Validation failed: %d\n", plan.Summary.ValidationFailedCount)
	fmt.Fprintf(&b, "- Components missing: %d\n", plan.Summary.ComponentsMissingCount)
	fmt.Fprintf(&b, "- High-risk changes: %d\n", plan.Summary.HighRiskChangeCount)
	fmt.Fprintf(&b, "- Excluded: %d\n\n", plan.Summary.ExcludedCount)
	fmt.Fprintf(&b, "## Targets\n\n")
	for _, target := range plan.Targets {
		name := target.DisplayName
		if name == "" {
			name = target.WorkloadID
		}
		fmt.Fprintf(&b, "- %s (%s): validation=%s, read_only=%t, excluded=%t, high_risk_changes=%d\n", name, target.WorkloadID, target.ValidationStatus, target.ReadOnly, target.Excluded, target.HighRiskChangeCount)
	}
	if len(plan.HardFailures) > 0 {
		fmt.Fprintf(&b, "\n## Hard failures\n\n")
		for _, failure := range plan.HardFailures {
			fmt.Fprintf(&b, "- %s\n", failure)
		}
	}
	fmt.Fprintf(&b, "\n> Persisted rollout: not implemented in v1; export is deterministic JSON/Markdown for now.\n")
	return b.String()
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

func (a *API) handleGetWorkloadKnownGood(w http.ResponseWriter, r *http.Request) {
	workloadID := chi.URLParam(r, "id")
	kg, err := a.db.GetWorkloadKnownGood(workloadID)
	if err != nil {
		respondError(w, 500, "failed to get known-good config")
		return
	}
	if kg == nil {
		respondError(w, 404, "known-good config not found")
		return
	}
	respondJSON(w, 200, kg)
}

type markKnownGoodRequest struct {
	ReplaceReason      string  `json:"replace_reason"`
	IfCurrentKnownGood *string `json:"if_current_known_good"`
	Force              bool    `json:"force"`
}

func runtimeOptionsForWorkload(wl models.Workload) validator.RuntimeOptions {
	runtimeOpts := validator.RuntimeOptionsFromEnv()
	runtimeOpts.TargetVersion = wl.Version
	if runtimeOpts.TargetVersion != "" {
		runtimeOpts.TargetVersionSource = "workload"
	}
	return runtimeOpts
}

func currentKnownGoodID(current *models.WorkloadKnownGoodConfig) string {
	if current == nil {
		return ""
	}
	return current.ConfigID
}

func knownGoodMarkResult(result models.SetKnownGoodResult) string {
	if !result.Changed {
		return "unchanged"
	}
	if result.ReplacedConfigID != "" {
		return "replaced"
	}
	return "created"
}

func auditDetail(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func (a *API) handleMarkWorkloadConfigKnownGood(w http.ResponseWriter, r *http.Request) {
	workloadID := chi.URLParam(r, "id")
	hash := chi.URLParam(r, "hash")

	var req markKnownGoodRequest
	if r.Body != nil {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			respondError(w, 400, "invalid JSON body")
			return
		}
	}
	reason := strings.TrimSpace(req.ReplaceReason)
	if len(reason) > 256 {
		respondError(w, 400, "replace_reason too long (max 256 chars)")
		return
	}

	wl, err := a.db.GetWorkload(workloadID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondError(w, 404, "workload not found")
			return
		}
		respondError(w, 500, "failed to load workload")
		return
	}
	if wl.ArchivedAt != nil {
		respondJSON(w, http.StatusConflict, map[string]string{"error": "workload is archived", "code": "workload_archived"})
		return
	}
	wc, err := a.db.GetWorkloadConfigByHash(workloadID, hash)
	if err != nil {
		respondError(w, 500, "failed to load config")
		return
	}
	if wc == nil {
		respondError(w, 404, "config not found in this workload's history")
		return
	}
	if wc.Status != "applied" {
		respondJSON(w, http.StatusConflict, map[string]string{"error": "config is not applied", "code": "not_applied"})
		return
	}
	if wc.Content == "" {
		respondJSON(w, http.StatusGone, map[string]string{"error": "config content is no longer available", "code": "config_content_unavailable"})
		return
	}
	runtimeOpts := runtimeOptionsForWorkload(wl)
	if result := validator.ValidateWithRuntime(r.Context(), []byte(wc.Content), wl.AvailableComponents, runtimeOpts); !result.Valid {
		respondJSON(w, 400, map[string]any{"error": "configuration failed validation", "validation_errors": result.Errors})
		return
	}
	current, err := a.db.GetWorkloadKnownGood(workloadID)
	if err != nil {
		respondError(w, 500, "failed to load known-good config")
		return
	}
	markedBy := ""
	if info := ext.UserInfoFromContext(r.Context()); info != nil {
		markedBy = info.Email
	}
	kg, setResult, err := a.db.SetWorkloadKnownGoodWithPrecondition(workloadID, hash, markedBy, reason, req.IfCurrentKnownGood, req.Force)
	if err != nil {
		respondError(w, 500, "failed to mark known-good")
		return
	}
	if setResult.PreconditionFailed {
		detail := auditDetail(map[string]any{
			"result":                 "conflict",
			"target_hash":            hash,
			"current_known_good":     setResult.CurrentConfigID,
			"if_current_known_good":  req.IfCurrentKnownGood,
			"force":                  req.Force,
			"replace_reason":         reason,
			"required_precondition":  true,
			"side_effect":            "none",
			"source_applied_at":      wc.AppliedAt,
			"source_content_present": wc.Content != "",
		})
		if err := audit.Emit(r.Context(), a.audit, "config.known_good.conflict", "workload", workloadID, detail); err != nil {
			respondAuditUnavailable(w, sideEffectNone)
			return
		}
		respondJSON(w, http.StatusConflict, map[string]string{
			"error":              "known-good precondition failed",
			"code":               "known_good_conflict",
			"current_known_good": setResult.CurrentConfigID,
		})
		return
	}
	detail := auditDetail(map[string]any{
		"result":                knownGoodMarkResult(setResult),
		"changed":               setResult.Changed,
		"target_hash":           hash,
		"previous_known_good":   currentKnownGoodID(current),
		"replaced_config_id":    setResult.ReplacedConfigID,
		"replace_reason":        reason,
		"source_applied_at":     kg.SourceAppliedAt,
		"if_current_known_good": req.IfCurrentKnownGood,
		"force":                 req.Force,
		"side_effect":           "known_good_pointer_recorded",
	})
	if err := audit.Emit(r.Context(), a.audit, "config.known_good.mark", "workload", workloadID, detail); err != nil {
		respondAuditUnavailable(w, sideEffectApplied)
		return
	}
	status := http.StatusOK
	if setResult.Changed && setResult.ReplacedConfigID == "" {
		status = http.StatusCreated
	}
	respondJSON(w, status, map[string]any{
		"changed":            setResult.Changed,
		"replaced_config_id": setResult.ReplacedConfigID,
		"known_good":         kg,
	})
}

func (a *API) handleClearWorkloadKnownGood(w http.ResponseWriter, r *http.Request) {
	workloadID := chi.URLParam(r, "id")
	wl, err := a.db.GetWorkload(workloadID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondError(w, 404, "workload not found")
			return
		}
		respondError(w, 500, "failed to load workload")
		return
	}
	if wl.ArchivedAt != nil {
		respondJSON(w, http.StatusConflict, map[string]string{"error": "workload is archived", "code": "workload_archived"})
		return
	}
	old, err := a.db.ClearWorkloadKnownGood(workloadID)
	if err != nil {
		respondError(w, 500, "failed to clear known-good")
		return
	}
	detail := auditDetail(map[string]any{"result": "unchanged", "changed": false, "side_effect": "none"})
	if old != nil {
		detail = auditDetail(map[string]any{
			"result":            "cleared",
			"changed":           true,
			"cleared_config_id": old.ConfigID,
			"source_applied_at": old.SourceAppliedAt,
			"marked_by":         old.MarkedBy,
			"replace_reason":    old.ReplaceReason,
			"content_available": old.ContentAvailable,
			"side_effect":       "known_good_pointer_deleted",
		})
	}
	if err := audit.Emit(r.Context(), a.audit, "config.known_good.clear", "workload", workloadID, detail); err != nil {
		respondAuditUnavailable(w, sideEffectApplied)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleRollbackWorkloadDefault(w http.ResponseWriter, r *http.Request) {
	workloadID := chi.URLParam(r, "id")
	if a.opamp == nil {
		respondError(w, 503, "OpAMP server not available")
		return
	}
	wl, err := a.db.GetWorkload(workloadID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondError(w, 404, "workload not found")
			return
		}
		respondError(w, 500, "failed to load workload")
		return
	}
	if wl.ArchivedAt != nil {
		respondJSON(w, http.StatusConflict, map[string]string{"error": "workload is archived", "code": "workload_archived"})
		return
	}
	if wl.Type != "" && wl.Type != "collector" {
		respondJSON(w, http.StatusConflict, map[string]string{"error": "rollback is supported only for collector workloads", "code": "workload_not_collector"})
		return
	}
	if !wl.AcceptsRemoteConfig {
		respondJSON(w, http.StatusConflict, map[string]string{"error": "workload does not accept remote config", "code": "remote_config_unsupported"})
		return
	}
	if concurrent, err := a.db.GetLatestPendingOrApplyingWorkloadConfig(workloadID); err != nil {
		respondError(w, 500, "failed to check pending config changes")
		return
	} else if concurrent != nil {
		respondJSON(w, http.StatusConflict, map[string]string{"error": "a config change is already pending or applying for this workload", "code": "concurrent_config_change"})
		return
	}
	excludeHash := wl.ActiveConfigHash
	if excludeHash == "" {
		if last, err := a.db.GetLastAppliedWorkloadConfig(workloadID); err == nil && last != nil {
			excludeHash = last.ConfigID
		} else if err != nil {
			respondError(w, 500, "failed to resolve current config")
			return
		}
	}
	target, err := a.db.GetRollbackTarget(workloadID, excludeHash)
	if err != nil {
		respondError(w, 500, "failed to resolve rollback target")
		return
	}
	if target == nil {
		respondJSON(w, http.StatusConflict, map[string]string{"error": "no rollback target", "code": "no_rollback_target"})
		return
	}
	if target.Config.Content == "" {
		respondJSON(w, http.StatusGone, map[string]string{"error": "config content is no longer available", "code": "config_content_unavailable"})
		return
	}
	body := []byte(target.Config.Content)
	runtimeOpts := runtimeOptionsForWorkload(wl)
	if result := validator.ValidateWithRuntime(r.Context(), body, wl.AvailableComponents, runtimeOpts); !result.Valid {
		respondJSON(w, 400, map[string]any{"error": "configuration failed validation", "validation_errors": result.Errors})
		return
	}
	pushedBy := ""
	if info := ext.UserInfoFromContext(r.Context()); info != nil {
		pushedBy = info.Email
	}
	submittedAt := time.Now().UTC()
	if err := a.db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: workloadID, ConfigID: target.Config.ConfigID, AppliedAt: submittedAt, SubmittedAt: submittedAt, Status: models.PushStatusSubmitted, PushedBy: pushedBy, InstanceStatuses: initialPushInstanceStatuses(target.Config.ConfigID, submittedAt, a.opamp.Instances(workloadID))}); err != nil {
		respondError(w, 500, "failed to record push")
		return
	}
	if err := a.opamp.PushConfig(r.Context(), workloadID, body, ""); err != nil {
		sanitized := models.SanitizeRemoteConfigErrorMessage(err.Error())
		_ = a.db.UpdateWorkloadConfigStatus(workloadID, target.Config.ConfigID, models.PushStatusFailed, sanitized)
		respondError(w, 502, sanitized)
		return
	}
	_ = a.db.MarkWorkloadConfigSent(workloadID, target.Config.ConfigID, time.Now().UTC())
	detail := auditDetail(map[string]any{
		"result":       "initiated",
		"target_hash":  target.Config.ConfigID,
		"target_kind":  target.Kind,
		"source_hash":  excludeHash,
		"pushed_by":    pushedBy,
		"side_effect":  "opamp_push_sent",
		"submitted_at": submittedAt,
	})
	if err := audit.Emit(r.Context(), a.audit, "config.rollback", "workload", workloadID, detail); err != nil {
		respondAuditUnavailable(w, sideEffectApplied)
		return
	}
	respondJSON(w, 202, map[string]string{"status": "rollback initiated", "config_hash": target.Config.ConfigID, "target_kind": target.Kind})
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
		respondRollbackError(w, 503, "OpAMP server not available", "opamp_unavailable", true, "none", nil)
		return
	}

	wc, err := a.db.GetWorkloadConfigByHash(workloadID, hash)
	if err != nil {
		respondRollbackError(w, 500, "failed to load past config", "internal_error", true, "none", nil)
		return
	}
	if wc == nil {
		respondRollbackError(w, 404, "config not found in this workload's history", "target_not_found", false, "none", nil)
		return
	}
	if wc.Status != models.PushStatusApplied {
		respondRollbackError(w, http.StatusConflict, "rollback target must be an applied config", "target_not_applied", false, "none", map[string]string{"target_status": wc.Status})
		return
	}
	if wc.Content == "" {
		// History rows JOIN configs.content; an empty string means the
		// underlying configs row is missing. Refuse to rollback to a
		// body we cannot reconstruct.
		respondRollbackError(w, 410, "config content is no longer available", "target_content_unavailable", false, "none", nil)
		return
	}

	wl, err := a.db.GetWorkload(workloadID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondRollbackError(w, 404, "workload not found", "workload_not_found", false, "none", nil)
			return
		}
		respondRollbackError(w, 500, "failed to load workload", "internal_error", true, "none", nil)
		return
	}
	if wl.ArchivedAt != nil {
		respondRollbackError(w, http.StatusConflict, "workload is archived", "workload_archived", false, "none", nil)
		return
	}
	if wl.Type != "" && wl.Type != "collector" {
		respondRollbackError(w, http.StatusConflict, "rollback is supported only for collector workloads", "workload_not_collector", false, "none", nil)
		return
	}
	if !wl.AcceptsRemoteConfig {
		respondRollbackError(w, http.StatusConflict, "workload does not accept remote config", "remote_config_unsupported", false, "none", nil)
		return
	}
	if concurrent, err := a.db.GetLatestPendingOrApplyingWorkloadConfig(workloadID); err != nil {
		respondRollbackError(w, 500, "failed to check pending config changes", "internal_error", true, "none", nil)
		return
	} else if concurrent != nil {
		respondRollbackError(w, http.StatusConflict, "a config change is already pending or applying for this workload", "concurrent_config_change", false, "none", nil)
		return
	}

	body := []byte(wc.Content)

	var available *models.AvailableComponents
	if err == nil && wl.AvailableComponents != nil {
		available = wl.AvailableComponents
	}
	// Re-validate against the workload's *current* AvailableComponents
	// rather than trusting that the past hash is still semantically valid.
	// A rollback to a config that referenced a now-uninstalled exporter
	// must fail loudly, not silently break the agent.
	runtimeOpts := validator.RuntimeOptionsFromEnv()
	runtimeOpts.TargetVersion = wl.Version
	if runtimeOpts.TargetVersion != "" {
		runtimeOpts.TargetVersionSource = "workload"
	}
	if result := validator.ValidateWithRuntime(r.Context(), body, available, runtimeOpts); !result.Valid {
		validation := a.buildRollbackValidation(wl, wc)
		code := "validation_failed"
		status := http.StatusBadRequest
		if unavailable, ok := validation["unavailable_components"].([]unavailableComponentWarning); ok && len(unavailable) > 0 {
			code = "component_not_installed"
			status = http.StatusConflict
		} else if len(result.Errors) > 0 && result.Errors[0].Code == "yaml_parse" {
			code = "target_yaml_invalid"
		}
		respondRollbackError(w, status, "configuration failed validation", code, false, "none", map[string]any{"validation": validation})
		return
	}

	pushedBy := ""
	if info := ext.UserInfoFromContext(r.Context()); info != nil {
		pushedBy = info.Email
	}

	appliedAt := time.Now().UTC()
	if err := a.db.RecordWorkloadConfig(models.WorkloadConfig{
		WorkloadID:       workloadID,
		ConfigID:         hash,
		AppliedAt:        appliedAt,
		SubmittedAt:      appliedAt,
		Status:           models.PushStatusSubmitted,
		PushedBy:         pushedBy,
		InstanceStatuses: initialPushInstanceStatuses(hash, appliedAt, a.opamp.Instances(workloadID)),
	}); err != nil {
		respondRollbackError(w, 500, "failed to record push", "record_push_failed", true, "none", nil)
		return
	}

	if err := a.opamp.PushConfig(r.Context(), workloadID, body, ""); err != nil {
		sanitized := models.SanitizeRemoteConfigErrorMessage(err.Error())
		_ = a.db.UpdateWorkloadConfigStatus(workloadID, hash, models.PushStatusFailed, sanitized)
		respondRollbackError(w, 502, sanitized, "push_failed", true, "applied", nil)
		return
	}
	_ = a.db.MarkWorkloadConfigSent(workloadID, hash, time.Now().UTC())

	requestID := newRollbackRequestID(workloadID, hash, appliedAt)
	currentHash := wl.ActiveConfigHash
	if currentHash == "" {
		if current := a.resolveCurrentConfig(wl); current != nil {
			currentHash = current.ConfigID
		}
	}
	if err := audit.Emit(r.Context(), a.audit, "config.rollback", "workload", workloadID, rollbackAuditDetail("hash", requestID, currentHash, hash)); err != nil {
		respondAuditUnavailable(w, sideEffectApplied)
		return
	}
	respondJSON(w, 202, map[string]any{
		"schema_version": "guided-rollback-action.v1",
		"request_id":     requestID,
		"status":         "accepted",
		"message":        "rollback initiated",
		"workload_id":    workloadID,
		"target_hash":    hash,
		"config_hash":    hash,
		"history_row": map[string]any{
			"workload_id": workloadID,
			"config_id":   hash,
			"applied_at":  appliedAt,
			"status":      "pending",
			"pushed_by":   pushedBy,
		},
		"status_url":      fmt.Sprintf("/api/workloads/%s/rollback/status?request_id=%s", workloadID, requestID),
		"timeout_seconds": rollbackTimeoutSeconds,
		"audit":           map[string]any{"event": "config.rollback", "emitted": true},
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
