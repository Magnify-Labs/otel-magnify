package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/magnify-labs/otel-magnify/internal/validator"
	"github.com/magnify-labs/otel-magnify/internal/version"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

const fleetVersionIntelligenceSchema = "fleet-version-intelligence.v1"

func (a *API) handleFleetVersionIntelligence(w http.ResponseWriter, r *http.Request) {
	recommended := strings.TrimSpace(r.URL.Query().Get("recommended_version"))
	recommendedComparable := false
	if recommended != "" {
		_, recommendedComparable = version.Compare(recommended, recommended)
	}
	items, err := a.db.ListWorkloads(false)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list workloads")
		return
	}

	out := models.FleetVersionIntelligence{
		SchemaVersion:               fleetVersionIntelligenceSchema,
		RecommendedVersion:          recommended,
		VersionMatrix:               []models.FleetVersionMatrixEntry{},
		CollectorsBelowRecommended:  []models.FleetCollectorVersionFinding{},
		UnsupportedConfigComponents: []models.FleetUnsupportedComponentFinding{},
		InvalidVersions:             []models.FleetInvalidVersionFinding{},
		Recommendations:             []models.FleetVersionRecommendation{},
	}

	matrix := make(map[string]*models.FleetVersionMatrixEntry)
	for _, wl := range items {
		group := workloadGroup(wl)
		versionValue := strings.TrimSpace(wl.Version)
		if versionValue == "" {
			versionValue = "unknown"
		}
		versionStatus := fleetVersionStatus(wl, recommended, recommendedComparable)
		key := group + "\x00" + wl.Type + "\x00" + wl.Status + "\x00" + versionValue
		entry := matrix[key]
		if entry == nil {
			entry = &models.FleetVersionMatrixEntry{
				Group:         group,
				Type:          wl.Type,
				Status:        wl.Status,
				Version:       versionValue,
				VersionStatus: versionStatus,
				WorkloadIDs:   []string{},
			}
			matrix[key] = entry
		}
		entry.Count++
		entry.WorkloadIDs = append(entry.WorkloadIDs, wl.ID)

		if wl.Type != "collector" {
			continue
		}
		if recommendedComparable && strings.TrimSpace(wl.Version) == "" {
			out.InvalidVersions = append(out.InvalidVersions, models.FleetInvalidVersionFinding{
				WorkloadID: wl.ID, DisplayName: wl.DisplayName, Version: wl.Version, Reason: "empty_version",
			})
		} else if recommendedComparable {
			cmp, ok := version.Compare(wl.Version, recommended)
			if !ok {
				out.InvalidVersions = append(out.InvalidVersions, models.FleetInvalidVersionFinding{
					WorkloadID: wl.ID, DisplayName: wl.DisplayName, Version: wl.Version, Reason: "invalid_semver",
				})
			} else if cmp < 0 {
				finding := models.FleetCollectorVersionFinding{
					WorkloadID: wl.ID, DisplayName: wl.DisplayName, Group: group, Version: wl.Version, RecommendedVersion: recommended,
				}
				out.CollectorsBelowRecommended = append(out.CollectorsBelowRecommended, finding)
				out.Recommendations = append(out.Recommendations, models.FleetVersionRecommendation{
					Action: "upgrade_collector", WorkloadID: wl.ID,
					Reason: fmt.Sprintf("collector version %s is below recommended %s", wl.Version, recommended),
				})
			}
		}

		unsupported := a.unsupportedComponentsForWorkload(wl)
		out.UnsupportedConfigComponents = append(out.UnsupportedConfigComponents, unsupported...)
		for _, finding := range unsupported {
			out.Recommendations = append(out.Recommendations,
				models.FleetVersionRecommendation{
					Action: "choose_older_config", WorkloadID: finding.WorkloadID, ConfigHash: finding.ConfigHash,
					Components: []string{finding.ComponentType},
					Reason:     "current collector capabilities do not support this config component",
				},
				models.FleetVersionRecommendation{
					Action: "remove_component", WorkloadID: finding.WorkloadID, ConfigHash: finding.ConfigHash,
					Components: []string{finding.ComponentType},
					Reason:     "remove or replace unsupported component before pushing this config",
				},
			)
		}
	}

	for _, entry := range matrix {
		sort.Strings(entry.WorkloadIDs)
		out.VersionMatrix = append(out.VersionMatrix, *entry)
	}
	sort.Slice(out.VersionMatrix, func(i, j int) bool {
		a, b := out.VersionMatrix[i], out.VersionMatrix[j]
		return strings.Join([]string{a.Group, a.Type, a.Status, a.Version}, "\x00") < strings.Join([]string{b.Group, b.Type, b.Status, b.Version}, "\x00")
	})
	respondJSON(w, http.StatusOK, out)
}

func fleetVersionStatus(wl models.Workload, recommended string, recommendedComparable bool) string {
	if wl.Type != "collector" {
		return "not_applicable"
	}
	if strings.TrimSpace(recommended) == "" || !recommendedComparable || strings.TrimSpace(wl.Version) == "" {
		return "unknown"
	}
	cmp, ok := version.Compare(wl.Version, recommended)
	if !ok {
		return "unknown"
	}
	switch {
	case cmp < 0:
		return "below_recommended"
	case cmp > 0:
		return "above_recommended"
	default:
		return "at_recommended"
	}
}

func workloadGroup(wl models.Workload) string {
	for _, key := range []string{"group", "service.name", "app.kubernetes.io/name", "k8s.deployment.name", "k8s.daemonset.name", "k8s.statefulset.name"} {
		if v := strings.TrimSpace(wl.Labels[key]); v != "" {
			return v
		}
		if v := strings.TrimSpace(wl.FingerprintKeys[key]); v != "" {
			return v
		}
	}
	return "ungrouped"
}

func (a *API) unsupportedComponentsForWorkload(wl models.Workload) []models.FleetUnsupportedComponentFinding {
	if wl.AvailableComponents == nil {
		return nil
	}
	history, err := a.db.GetWorkloadConfigHistory(wl.ID)
	if err != nil {
		return nil
	}
	var target *models.WorkloadConfig
	for i := range history {
		if history[i].Content == "" {
			continue
		}
		if history[i].ConfigID == wl.ActiveConfigHash || target == nil && history[i].Status == "applied" {
			target = &history[i]
			if history[i].ConfigID == wl.ActiveConfigHash {
				break
			}
		}
	}
	if target == nil {
		return nil
	}
	result := validator.Validate([]byte(target.Content), wl.AvailableComponents)
	if result.Valid {
		return nil
	}
	findings := []models.FleetUnsupportedComponentFinding{}
	seen := map[string]bool{}
	for _, validationError := range result.Errors {
		if validationError.Code != "component_not_installed" {
			continue
		}
		category := categoryFromPath(validationError.Path)
		componentType := componentTypeFromMessage(validationError.Message)
		if componentType == "" {
			componentType = "unknown"
		}
		key := target.ConfigID + "\x00" + category + "\x00" + componentType + "\x00" + validationError.Path
		if seen[key] {
			continue
		}
		seen[key] = true
		findings = append(findings, models.FleetUnsupportedComponentFinding{
			WorkloadID: wl.ID, DisplayName: wl.DisplayName, ConfigHash: target.ConfigID,
			Category: category, ComponentType: componentType, Path: validationError.Path,
			AvailableHash: wl.AvailableComponents.Hash, AvailableTypes: append([]string(nil), wl.AvailableComponents.Components[category]...),
		})
	}
	return findings
}

func categoryFromPath(path string) string {
	for _, category := range []string{"receivers", "processors", "exporters", "extensions", "connectors"} {
		if strings.Contains(path, "."+category+"[") || strings.HasPrefix(path, category+".") {
			return category
		}
	}
	return "unknown"
}

func componentTypeFromMessage(message string) string {
	parts := strings.SplitN(message, " type \"", 2)
	if len(parts) != 2 {
		return ""
	}
	component, _, _ := strings.Cut(parts[1], "\"")
	return component
}
