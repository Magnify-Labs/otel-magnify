package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/magnify-labs/otel-magnify/internal/validator"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

const rollbackTimeoutSeconds = 30

type rollbackRequestRef struct {
	WorkloadID string    `json:"workload_id"`
	TargetHash string    `json:"target_hash"`
	StartedAt  time.Time `json:"started_at"`
}

type rollbackFinding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Path     string `json:"path,omitempty"`
	Blocking bool   `json:"blocking"`
	Source   string `json:"source"`
}

type unavailableComponentWarning struct {
	Category      string   `json:"category"`
	ComponentID   string   `json:"component_id"`
	ComponentType string   `json:"component_type"`
	Path          string   `json:"path,omitempty"`
	Available     []string `json:"available,omitempty"`
	Blocking      bool     `json:"blocking"`
}

func (a *API) handlePrepareRollback(w http.ResponseWriter, r *http.Request) {
	workloadID := chi.URLParam(r, "id")
	targetHash := strings.TrimSpace(r.URL.Query().Get("target_hash"))
	targetSelector := strings.TrimSpace(r.URL.Query().Get("target_selector"))
	if (targetHash == "") == (targetSelector == "") {
		respondRollbackError(w, http.StatusBadRequest, "exactly one of target_hash or target_selector is required", "invalid_request", false, "none", nil)
		return
	}
	if targetSelector != "" && targetSelector != "known_good" {
		respondRollbackError(w, http.StatusBadRequest, "unsupported rollback target selector", "invalid_request", false, "none", nil)
		return
	}

	wl, err := a.db.GetWorkload(workloadID)
	if err != nil {
		respondRollbackError(w, http.StatusNotFound, "workload not found", "workload_not_found", false, "none", nil)
		return
	}

	target, targetSource, knownGoodSource, err := a.resolveRollbackTarget(workloadID, targetHash, targetSelector)
	if err != nil {
		if errors.Is(err, errKnownGoodNotFound) {
			respondRollbackError(w, http.StatusNotFound, "No known-good config marked for this workload", "known_good_not_found", false, "none", map[string]string{"helper_text": "No known-good config marked yet. Mark a successful revision as known-good from the push history."})
			return
		}
		respondRollbackError(w, http.StatusInternalServerError, "failed to resolve rollback target", "internal_error", true, "none", nil)
		return
	}
	if target == nil {
		respondRollbackError(w, http.StatusNotFound, "config not found in this workload's history", "target_not_found", false, "none", nil)
		return
	}
	if wl.ArchivedAt != nil {
		respondRollbackError(w, http.StatusConflict, "workload is archived", "workload_archived", false, "none", nil)
		return
	}
	if target.Status != models.PushStatusApplied {
		respondRollbackError(w, http.StatusConflict, "rollback target must be an applied config", "target_not_applied", false, "none", map[string]string{"target_status": target.Status})
		return
	}

	current := a.resolveCurrentConfig(wl)
	validation := a.buildRollbackValidation(wl, target)
	findings := validation["findings"].([]rollbackFinding)
	warnings, blockers := splitRollbackFindings(findings)
	if concurrent, err := a.db.GetLatestPendingOrApplyingWorkloadConfig(workloadID); err == nil && concurrent != nil {
		f := rollbackFinding{Code: "concurrent_config_change", Severity: "error", Message: "A config change is already pending or applying for this workload.", Blocking: true, Source: "concurrency"}
		findings = append(findings, f)
		blockers = append(blockers, f)
		validation["findings"] = findings
		validation["status"] = "invalid"
		validation["valid"] = false
		validation["can_confirm"] = false
	}

	confirmationLabel := "Confirm rollback"
	if len(warnings) > 0 {
		confirmationLabel = "Confirm rollback with warnings"
	}
	if len(blockers) > 0 {
		validation["status"] = "invalid"
		validation["valid"] = false
		validation["can_confirm"] = false
	}

	targetSnapshot := rollbackConfigSnapshot(target, "history")
	if targetSelector == "known_good" {
		if metadata, ok := targetSnapshot["metadata"].(map[string]any); ok {
			metadata["known_good"] = true
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"schema_version": "guided-rollback-prepare.v1",
		"workload":       rollbackWorkloadSnapshot(wl),
		"target_ref": map[string]any{
			"selector":          selectorName(targetSelector),
			"source":            targetSource,
			"workload_id":       workloadID,
			"target_hash":       target.ConfigID,
			"known_good":        targetSelector == "known_good" || isKnownGood(target),
			"known_good_source": knownGoodSource,
		},
		"current_config": rollbackConfigSnapshot(current, "active_config"),
		"target_config":  targetSnapshot,
		"diff":           buildRollbackDiff(current, target),
		"validation":     validation,
		"action": map[string]any{
			"can_submit":            len(blockers) == 0,
			"submit_url":            fmt.Sprintf("/api/workloads/%s/configs/%s/rollback", workloadID, target.ConfigID),
			"method":                "POST",
			"requires_confirmation": true,
			"confirmation_label":    confirmationLabel,
			"blocking_reasons":      blockers,
			"warnings":              warnings,
		},
		"status_context": map[string]any{"initial_remote_config_status": wl.RemoteConfigStatus, "timeout_seconds": rollbackTimeoutSeconds},
	})
}

var errKnownGoodNotFound = errors.New("known-good target not found")

func (a *API) resolveRollbackTarget(workloadID, targetHash, targetSelector string) (*models.WorkloadConfig, string, string, error) {
	if targetSelector == "known_good" {
		wc, err := a.db.GetLatestKnownGoodWorkloadConfig(workloadID)
		if err != nil {
			return nil, "latest_known_good", "first_class_marker", err
		}
		if wc == nil {
			return nil, "latest_known_good", "none", errKnownGoodNotFound
		}
		return wc, "latest_known_good", "first_class_marker", nil
	}
	wc, err := a.db.GetWorkloadConfigByHash(workloadID, targetHash)
	return wc, "push_history_row", knownGoodSource(wc), err
}

func (a *API) resolveCurrentConfig(wl models.Workload) *models.WorkloadConfig {
	if wl.ActiveConfigHash == "" {
		return &models.WorkloadConfig{WorkloadID: wl.ID, ConfigID: wl.ActiveConfigHash}
	}
	wc, err := a.db.GetWorkloadConfigByHash(wl.ID, wl.ActiveConfigHash)
	if err != nil || wc == nil {
		return &models.WorkloadConfig{WorkloadID: wl.ID, ConfigID: wl.ActiveConfigHash}
	}
	return wc
}

func rollbackWorkloadSnapshot(wl models.Workload) map[string]any {
	return map[string]any{
		"id":                    wl.ID,
		"display_name":          wl.DisplayName,
		"type":                  wl.Type,
		"status":                wl.Status,
		"accepts_remote_config": wl.AcceptsRemoteConfig,
		"active_config_id":      wl.ActiveConfigID,
		"active_config_hash":    wl.ActiveConfigHash,
		"remote_config_status":  wl.RemoteConfigStatus,
		"available_components":  wl.AvailableComponents,
	}
}

func rollbackConfigSnapshot(wc *models.WorkloadConfig, source string) map[string]any {
	if wc == nil {
		return map[string]any{"content_available": false, "source": source, "metadata": map[string]any{"known_good": false}}
	}
	metadata := map[string]any{"known_good": isKnownGood(wc)}
	if wc.Label != nil && *wc.Label != "" {
		metadata["label"] = *wc.Label
	}
	if !wc.AppliedAt.IsZero() {
		metadata["applied_at"] = wc.AppliedAt
	}
	if wc.PushedBy != "" {
		metadata["pushed_by"] = wc.PushedBy
	}
	if wc.Status != "" {
		metadata["previous_status"] = wc.Status
	}
	if wc.ErrorMessage != "" {
		metadata["error_message"] = wc.ErrorMessage
	}
	out := map[string]any{
		"hash":              wc.ConfigID,
		"content_available": wc.Content != "",
		"source":            source,
		"metadata":          metadata,
	}
	if wc.Content != "" {
		out["content"] = wc.Content
		out["content_sha256"] = sha256Hex([]byte(wc.Content))
	}
	return out
}

func (a *API) buildRollbackValidation(wl models.Workload, target *models.WorkloadConfig) map[string]any {
	checkedAt := time.Now().UTC()
	inputs := map[string]any{"workload_id": wl.ID, "workload_type": wl.Type, "accepts_remote_config": wl.AcceptsRemoteConfig}
	if wl.AvailableComponents != nil {
		inputs["available_components_hash"] = wl.AvailableComponents.Hash
		inputs["available_components"] = wl.AvailableComponents
	}
	if target != nil {
		inputs["target_hash"] = target.ConfigID
		if target.Content != "" {
			inputs["target_content_sha256"] = sha256Hex([]byte(target.Content))
		}
	}
	findings := make([]rollbackFinding, 0)
	unavailable := make([]unavailableComponentWarning, 0)
	if wl.Type != "" && wl.Type != "collector" {
		findings = append(findings, rollbackFinding{Code: "workload_not_collector", Severity: "error", Message: "Rollback is supported only for collector workloads.", Blocking: true, Source: "remote_config"})
	}
	if !wl.AcceptsRemoteConfig {
		findings = append(findings, rollbackFinding{Code: "remote_config_unsupported", Severity: "error", Message: "Workload does not accept remote config.", Blocking: true, Source: "remote_config"})
	}
	if target == nil || target.Content == "" {
		findings = append(findings, rollbackFinding{Code: "target_content_unavailable", Severity: "error", Message: "Rollback target content is unavailable. This revision cannot be used for rollback.", Blocking: true, Source: "target"})
	} else {
		result := validator.Validate([]byte(target.Content), wl.AvailableComponents)
		for _, err := range result.Errors {
			f := rollbackFinding{Code: err.Code, Severity: "error", Message: err.Message, Path: err.Path, Blocking: true, Source: validationSource(err.Code)}
			findings = append(findings, f)
			if err.Code == "component_not_installed" {
				unavailable = append(unavailable, unavailableFromValidationError(err, target.Content, wl.AvailableComponents))
			}
		}
	}
	status, valid := validationStatus(findings)
	return map[string]any{
		"status":                 status,
		"valid":                  valid,
		"can_confirm":            valid,
		"checked_at":             checkedAt,
		"validator_version":      "light-validator.v1",
		"inputs":                 inputs,
		"findings":               findings,
		"unavailable_components": unavailable,
	}
}

func validationStatus(findings []rollbackFinding) (string, bool) {
	warn := false
	for _, f := range findings {
		if f.Blocking {
			return "invalid", false
		}
		if f.Severity == "warning" {
			warn = true
		}
	}
	if warn {
		return "valid_with_warnings", true
	}
	return "valid", true
}

func splitRollbackFindings(findings []rollbackFinding) ([]rollbackFinding, []rollbackFinding) {
	warnings := make([]rollbackFinding, 0)
	blockers := make([]rollbackFinding, 0)
	for _, f := range findings {
		if f.Blocking {
			blockers = append(blockers, f)
		} else if f.Severity == "warning" {
			warnings = append(warnings, f)
		}
	}
	return warnings, blockers
}

func buildRollbackDiff(current, target *models.WorkloadConfig) map[string]any {
	out := map[string]any{"direction": "current_to_target"}
	if target != nil {
		out["target_hash"] = target.ConfigID
	}
	if current != nil && current.ConfigID != "" {
		out["base_hash"] = current.ConfigID
	}
	if current == nil || current.Content == "" || target == nil || target.Content == "" {
		out["status"] = "unavailable"
		out["computation"] = "frontend_raw_inputs_only"
		out["inputs"] = map[string]any{"current_content_available": current != nil && current.Content != "", "target_content_available": target != nil && target.Content != ""}
		out["message"] = "Diff unavailable because one side of the rollback comparison is not available."
		return out
	}
	diffText := unifiedDiff(current.Content, target.Content, "current", "rollback target")
	status := "available"
	if diffText == "" {
		status = "empty"
	}
	out["status"] = status
	out["computation"] = "backend_raw"
	out["raw_diff"] = map[string]any{"format": "unified", "language": "yaml", "base_label": "Current " + shortHash(current.ConfigID), "target_label": "Rollback target " + shortHash(target.ConfigID), "text": diffText, "truncated": false}
	return out
}

func unifiedDiff(a, b, from, to string) string {
	if a == b {
		return ""
	}
	var buf bytes.Buffer
	buf.WriteString("--- " + from + "\n+++ " + to + "\n")
	aLines := strings.Split(strings.TrimSuffix(a, "\n"), "\n")
	bLines := strings.Split(strings.TrimSuffix(b, "\n"), "\n")
	fmt.Fprintf(&buf, "@@ -1,%d +1,%d @@\n", len(aLines), len(bLines))
	for _, line := range aLines {
		buf.WriteString("-" + line + "\n")
	}
	for _, line := range bLines {
		buf.WriteString("+" + line + "\n")
	}
	return buf.String()
}

func (a *API) handleRollbackStatus(w http.ResponseWriter, r *http.Request) {
	workloadID := chi.URLParam(r, "id")
	ref, err := decodeRollbackRequestID(r.URL.Query().Get("request_id"))
	if err != nil || ref.WorkloadID != workloadID {
		respondRollbackError(w, http.StatusBadRequest, "invalid rollback request id", "invalid_request", false, "none", nil)
		return
	}
	history, err := a.db.GetWorkloadConfigHistory(workloadID)
	if err != nil {
		respondRollbackError(w, http.StatusInternalServerError, "failed to load rollback status", "internal_error", true, "none", nil)
		return
	}
	var row *models.WorkloadConfig
	for i := range history {
		if history[i].ConfigID == ref.TargetHash && history[i].AppliedAt.Equal(ref.StartedAt) {
			row = &history[i]
			break
		}
	}
	if row == nil {
		respondRollbackError(w, http.StatusNotFound, "rollback request not found", "target_not_found", false, "none", nil)
		return
	}
	wl, _ := a.db.GetWorkload(workloadID)
	now := time.Now().UTC()
	elapsed := now.Sub(ref.StartedAt)
	apply := "accepted"
	terminal := false
	terminalStatus := ""
	last := row.Status
	if row.Status == "applying" || row.Status == "applied" || row.Status == "failed" {
		apply = row.Status
	}
	if wl.RemoteConfigStatus != nil && wl.RemoteConfigStatus.ConfigHash == ref.TargetHash {
		if wl.RemoteConfigStatus.Status == "applying" || wl.RemoteConfigStatus.Status == "applied" || wl.RemoteConfigStatus.Status == "failed" {
			apply = wl.RemoteConfigStatus.Status
			last = wl.RemoteConfigStatus.Status
		}
	}
	if apply == "applied" || apply == "failed" {
		terminal = true
		terminalStatus = apply
	} else if elapsed > rollbackTimeoutSeconds*time.Second {
		apply = "unknown"
		terminal = true
		terminalStatus = "unknown"
	}
	report := map[string]any{
		"schema_version":      "guided-rollback-status.v1",
		"request_id":          r.URL.Query().Get("request_id"),
		"workload_id":         workloadID,
		"target_hash":         ref.TargetHash,
		"target_status":       row.Status,
		"target_applied_at":   row.AppliedAt,
		"target_submitted_at": row.SubmittedAt,
		"target_pushed_by":    row.PushedBy,
		"request_status":      "accepted",
		"apply_status":        apply,
		"terminal":            terminal,
		"started_at":          ref.StartedAt,
		"elapsed_ms":          elapsed.Milliseconds(),
		"timeout_seconds":     rollbackTimeoutSeconds,
		"timed_out":           apply == "unknown",
		"last_known_status":   last,
	}
	if terminalStatus != "" {
		report["terminal_status"] = terminalStatus
	}
	if row.Label != nil {
		report["target_label"] = *row.Label
	}
	respondJSON(w, http.StatusOK, report)
}

func newRollbackRequestID(workloadID, targetHash string, startedAt time.Time) string {
	b, _ := json.Marshal(rollbackRequestRef{WorkloadID: workloadID, TargetHash: targetHash, StartedAt: startedAt.UTC().Truncate(time.Microsecond)})
	return "rb_" + base64.RawURLEncoding.EncodeToString(b)
}

func rollbackAuditDetail(targetKind, requestID, currentHash, targetHash string) string {
	detail := map[string]string{
		"target_kind":  targetKind,
		"request_id":   requestID,
		"current_hash": currentHash,
		"target_hash":  targetHash,
	}
	b, err := json.Marshal(detail)
	if err != nil {
		return targetHash
	}
	return string(b)
}

func decodeRollbackRequestID(id string) (rollbackRequestRef, error) {
	var ref rollbackRequestRef
	if !strings.HasPrefix(id, "rb_") {
		return ref, errors.New("bad request id prefix")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, "rb_"))
	if err != nil {
		return ref, err
	}
	if err := json.Unmarshal(b, &ref); err != nil {
		return ref, err
	}
	return ref, nil
}

func respondRollbackError(w http.ResponseWriter, status int, msg, code string, retryable bool, sideEffect string, details any) {
	body := map[string]any{"error": msg, "code": code, "retryable": retryable}
	if sideEffect != "" && sideEffect != "none" {
		body["side_effect_status"] = sideEffect
	}
	if details != nil {
		body["details"] = details
	}
	respondJSON(w, status, body)
}

func selectorName(selector string) string {
	if selector == "known_good" {
		return "known_good"
	}
	return "hash"
}

func isKnownGood(wc *models.WorkloadConfig) bool {
	return knownGoodSource(wc) != "none"
}

func knownGoodSource(wc *models.WorkloadConfig) string {
	if wc == nil || wc.Label == nil {
		return "none"
	}
	s := strings.ToLower(strings.TrimSpace(*wc.Label))
	if s == "known-good" || s == "known_good" || s == "known good" {
		return "label_convention"
	}
	return "none"
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func shortHash(hash string) string {
	if len(hash) <= 8 {
		return hash
	}
	return hash[:8]
}

func validationSource(code string) string {
	switch code {
	case "component_not_installed":
		return "capabilities"
	case "yaml_parse", "empty_config", "missing_service", "missing_pipelines", "invalid_pipeline", "missing_pipeline_section", "undefined_component":
		return "yaml"
	default:
		return "target"
	}
}

var componentTypeRE = regexp.MustCompile(`type "([^"]+)"`)

func unavailableFromValidationError(err validator.Error, content string, available *models.AvailableComponents) unavailableComponentWarning {
	category := categoryFromValidationPath(err.Path)
	componentID := componentIDFromValidationPath(content, err.Path)
	componentType := componentID
	if m := componentTypeRE.FindStringSubmatch(err.Message); len(m) == 2 {
		componentType = m[1]
	} else if idx := strings.Index(componentType, "/"); idx >= 0 {
		componentType = componentType[:idx]
	}
	var installed []string
	if available != nil && available.Components != nil {
		installed = available.Components[category]
	}
	return unavailableComponentWarning{Category: category, ComponentID: componentID, ComponentType: componentType, Path: err.Path, Available: installed, Blocking: true}
}

func categoryFromValidationPath(path string) string {
	for _, category := range []string{"receivers", "processors", "exporters", "extensions", "connectors"} {
		if strings.Contains(path, "."+category+"[") || strings.Contains(path, "."+category+".") || strings.Contains(path, category+"[") {
			return category
		}
	}
	return ""
}

func componentIDFromValidationPath(content, path string) string {
	var root map[string]any
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		return ""
	}
	parts := strings.Split(path, ".")
	var cur any = root
	for _, part := range parts {
		name := part
		idx := -1
		if open := strings.Index(part, "["); open >= 0 && strings.HasSuffix(part, "]") {
			name = part[:open]
			if _, err := fmt.Sscanf(part[open+1:len(part)-1], "%d", &idx); err != nil {
				return ""
			}
		}
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = m[name]
		if idx >= 0 {
			arr, ok := cur.([]any)
			if !ok || idx >= len(arr) {
				return ""
			}
			cur = arr[idx]
		}
	}
	if s, ok := cur.(string); ok {
		return s
	}
	return ""
}
