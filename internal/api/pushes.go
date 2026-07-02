package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"github.com/magnify-labs/otel-magnify/internal/validator"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

type pushGroupSelector struct {
	MatchLabels  map[string]string `json:"match_labels,omitempty"`
	Types        []string          `json:"types,omitempty"`
	Versions     []string          `json:"versions,omitempty"`
	Capabilities []string          `json:"capabilities,omitempty"`
}

type pushGroup struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Selector    pushGroupSelector `json:"selector"`
}

var savedPushGroups = []pushGroup{
	{
		ID:          "prod-eu",
		Name:        "Production EU collectors",
		Description: "Production collectors in the EU cluster",
		Selector:    pushGroupSelector{MatchLabels: map[string]string{"cluster": "prod-eu", "env": "prod"}, Types: []string{"collector"}},
	},
	{
		ID:          "staging",
		Name:        "Staging collectors",
		Description: "Collectors serving staging environments",
		Selector:    pushGroupSelector{MatchLabels: map[string]string{"env": "staging"}, Types: []string{"collector"}},
	},
	{
		ID:          "edge",
		Name:        "Edge collectors",
		Description: "Collectors deployed at the edge tier",
		Selector:    pushGroupSelector{MatchLabels: map[string]string{"tier": "edge"}, Types: []string{"collector"}},
	},
	{
		ID:          "payments",
		Name:        "Payments collectors",
		Description: "Production payment pipeline collectors",
		Selector:    pushGroupSelector{MatchLabels: map[string]string{"team": "payments", "env": "prod"}, Types: []string{"collector"}},
	},
}

type pushPreviewRequest struct {
	GroupID       string             `json:"group_id"`
	Selector      *pushGroupSelector `json:"selector"`
	ConfigContent string             `json:"config_content"`
	// Backward-compatible aliases accepted by early backend drafts.
	ConfigID string `json:"config_id"`
	YAML     string `json:"yaml"`
}

type pushPreviewBreakdown struct {
	RemoteConfigCapable int `json:"remote_config_capable"`
	ReadOnly            int `json:"read_only"`
	Incompatible        int `json:"incompatible"`
	Offline             int `json:"offline"`
}

type pushPreviewTarget struct {
	WorkloadID          string `json:"workload_id"`
	DisplayName         string `json:"display_name"`
	Type                string `json:"type"`
	Version             string `json:"version,omitempty"`
	Status              string `json:"status"`
	Bucket              string `json:"bucket"`
	Reason              string `json:"reason,omitempty"`
	AcceptsRemoteConfig bool   `json:"accepts_remote_config"`
	LastSeenUnix        int64  `json:"last_seen_unix,omitempty"`
}

type pushPreviewResponse struct {
	GroupID       string               `json:"group_id,omitempty"`
	Selector      pushGroupSelector    `json:"selector"`
	TargetedCount int                  `json:"targeted_count"`
	Breakdown     pushPreviewBreakdown `json:"breakdown"`
	Targets       []pushPreviewTarget  `json:"targets"`
}

// handleListPushActivity returns per-day push counts for the dashboard chart.
// Only window=7d is supported today; the query param exists to future-proof
// the endpoint without breaking the client contract.
func (a *API) handleListPushActivity(w http.ResponseWriter, r *http.Request) {
	window := r.URL.Query().Get("window")
	if window == "" {
		window = "7d"
	}
	if window != "7d" {
		respondError(w, http.StatusBadRequest, "unsupported window; only 7d is supported")
		return
	}

	points, err := a.db.GetPushActivity(7)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to compute push activity")
		return
	}
	respondJSON(w, http.StatusOK, points)
}

func (a *API) handleListPushGroups(w http.ResponseWriter, _ *http.Request) {
	respondJSON(w, http.StatusOK, savedPushGroups)
}

func (a *API) handlePreviewPush(w http.ResponseWriter, r *http.Request) {
	var req pushPreviewRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	//nolint:errcheck // deferred cleanup of fully-read request body; net/http server also closes it
	defer r.Body.Close()

	selector, groupID, ok := resolvePushPreviewSelector(req)
	if !ok {
		respondError(w, http.StatusNotFound, "push group not found")
		return
	}
	if selector == nil {
		respondError(w, http.StatusBadRequest, "group_id or selector is required")
		return
	}

	configContent := req.ConfigContent
	if configContent == "" {
		configContent = req.YAML
	}
	if configContent == "" && req.ConfigID != "" {
		cfg, err := a.db.GetConfig(req.ConfigID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				respondError(w, http.StatusNotFound, "config not found")
				return
			}
			respondError(w, http.StatusInternalServerError, "failed to load config")
			return
		}
		configContent = cfg.Content
	}

	workloads, err := a.db.ListWorkloads(false)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list workloads")
		return
	}

	respondJSON(w, http.StatusOK, buildPushPreview(groupID, *selector, workloads, []byte(configContent)))
}

func resolvePushPreviewSelector(req pushPreviewRequest) (*pushGroupSelector, string, bool) {
	if req.GroupID == "" {
		return req.Selector, "", true
	}
	for _, group := range savedPushGroups {
		if group.ID == req.GroupID {
			selector := group.Selector
			return &selector, group.ID, true
		}
	}
	return nil, "", false
}

func buildPushPreview(groupID string, selector pushGroupSelector, workloads []models.Workload, configContent []byte) pushPreviewResponse {
	targets := filterPushPreviewTargets(workloads, selector)
	resp := pushPreviewResponse{
		GroupID:       groupID,
		Selector:      selector,
		TargetedCount: len(targets),
		Targets:       make([]pushPreviewTarget, 0, len(targets)),
	}
	for _, workload := range targets {
		target := classifyPushPreviewTarget(workload, configContent)
		resp.Targets = append(resp.Targets, target)
		switch target.Bucket {
		case "remote_config_capable":
			resp.Breakdown.RemoteConfigCapable++
		case "read_only":
			resp.Breakdown.ReadOnly++
		case "incompatible":
			resp.Breakdown.Incompatible++
		case "offline":
			resp.Breakdown.Offline++
		}
	}
	return resp
}

func filterPushPreviewTargets(workloads []models.Workload, selector pushGroupSelector) []models.Workload {
	matched := make([]models.Workload, 0)
	for _, workload := range workloads {
		if matchesPushSelector(workload, selector) {
			matched = append(matched, workload)
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].DisplayName < matched[j].DisplayName })
	return matched
}

func matchesPushSelector(workload models.Workload, selector pushGroupSelector) bool {
	for key, want := range selector.MatchLabels {
		if got := workload.Labels[key]; got != want {
			return false
		}
	}
	if len(selector.Types) > 0 && !pushPreviewContainsString(selector.Types, workload.Type) {
		return false
	}
	if len(selector.Versions) > 0 && !pushPreviewContainsString(selector.Versions, workload.Version) {
		return false
	}
	for _, capability := range selector.Capabilities {
		if !workloadHasPushCapability(workload, capability) {
			return false
		}
	}
	return true
}

func classifyPushPreviewTarget(workload models.Workload, configContent []byte) pushPreviewTarget {
	target := pushPreviewTarget{
		WorkloadID:          workload.ID,
		DisplayName:         workload.DisplayName,
		Type:                workload.Type,
		Version:             workload.Version,
		Status:              workload.Status,
		AcceptsRemoteConfig: workload.AcceptsRemoteConfig,
	}
	if !workload.LastSeenAt.IsZero() {
		target.LastSeenUnix = workload.LastSeenAt.UTC().Unix()
	}
	switch {
	case workload.Status == "disconnected":
		target.Bucket = "offline"
		target.Reason = "workload is not connected"
	case workload.Type != "collector" || !workload.AcceptsRemoteConfig:
		target.Bucket = "read_only"
		target.Reason = "workload does not accept remote config"
	case len(configContent) > 0:
		result := validator.Validate(configContent, workload.AvailableComponents)
		if !result.Valid {
			target.Bucket = "incompatible"
			if len(result.Errors) > 0 {
				target.Reason = result.Errors[0].Message
			} else {
				target.Reason = "configuration failed validation"
			}
			return target
		}
		fallthrough
	default:
		target.Bucket = "remote_config_capable"
	}
	return target
}

func workloadHasPushCapability(workload models.Workload, capability string) bool {
	switch capability {
	case "remote_config", "remote_config_capable", "push_config":
		return workload.AcceptsRemoteConfig
	case "report_config":
		return workload.Type == "collector"
	}
	if workload.AvailableComponents == nil {
		return false
	}
	for _, components := range workload.AvailableComponents.Components {
		if pushPreviewContainsString(components, capability) {
			return true
		}
	}
	return false
}

func pushPreviewContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
