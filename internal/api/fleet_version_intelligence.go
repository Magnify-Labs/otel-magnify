package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

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
		CompatibilitySummary:        models.FleetCompatibilitySummary{NotRunnableCollectors: []models.FleetCompatibilityCollectorSummary{}},
		CompatibilityMatrix:         []models.FleetCompatibilityMatrixEntry{},
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
		out.CompatibilitySummary.TotalCollectors++
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

		compatibility := a.compatibilityEntryForWorkload(wl, group, versionStatus, unsupported)
		out.CompatibilityMatrix = append(out.CompatibilityMatrix, compatibility)
		if compatibility.Runnable {
			out.CompatibilitySummary.RunnableCount++
		} else {
			out.CompatibilitySummary.NotRunnableCount++
			out.CompatibilitySummary.NotRunnableCollectors = append(out.CompatibilitySummary.NotRunnableCollectors, models.FleetCompatibilityCollectorSummary{
				WorkloadID: wl.ID, DisplayName: wl.DisplayName, BlockingReasons: compatibility.BlockingReasons,
			})
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
	sort.Slice(out.CompatibilityMatrix, func(i, j int) bool {
		return out.CompatibilityMatrix[i].WorkloadID < out.CompatibilityMatrix[j].WorkloadID
	})
	sort.Slice(out.CompatibilitySummary.NotRunnableCollectors, func(i, j int) bool {
		return out.CompatibilitySummary.NotRunnableCollectors[i].WorkloadID < out.CompatibilitySummary.NotRunnableCollectors[j].WorkloadID
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
	target := a.targetConfigForWorkload(wl)
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

func (a *API) targetConfigForWorkload(wl models.Workload) *models.WorkloadConfig {
	if wl.ActiveConfigHash != "" {
		if wc, err := a.db.GetWorkloadConfigByHash(wl.ID, wl.ActiveConfigHash); err == nil && wc != nil && wc.Content != "" {
			return wc
		}
	}
	if wc, err := a.db.GetLastAppliedWorkloadConfig(wl.ID); err == nil && wc != nil && wc.Content != "" {
		return wc
	}
	return nil
}

func (a *API) compatibilityEntryForWorkload(wl models.Workload, group, versionStatus string, unsupported []models.FleetUnsupportedComponentFinding) models.FleetCompatibilityMatrixEntry {
	target := a.targetConfigForWorkload(wl)
	entry := models.FleetCompatibilityMatrixEntry{
		WorkloadID:          wl.ID,
		DisplayName:         wl.DisplayName,
		Group:               group,
		Status:              wl.Status,
		Version:             compatibilityVersion(wl.Version, versionStatus),
		AvailableComponents: compatibilityAvailable(wl.AvailableComponents),
		RequiredComponents:  []models.FleetCompatibilityComponent{},
		Config:              models.FleetCompatibilityConfig{Source: "none"},
		KnownIssues:         []models.FleetCompatibilityKnownIssue{},
		OpAMP:               compatibilityOpAMP(wl),
		Runnable:            true,
		BlockingReasons:     []models.FleetCompatibilityReason{},
	}
	if target != nil {
		entry.Config = models.FleetCompatibilityConfig{Hash: target.ConfigID, Source: "active_or_applied"}
		entry.RequiredComponents = requiredComponentsFromConfig(target.Content)
	}
	entry.BlockingReasons = append(
		entry.BlockingReasons,
		componentCapabilityBlockers(entry.RequiredComponents, wl.AvailableComponents)...,
	)
	if !entry.Version.Comparable {
		entry.BlockingReasons = append(entry.BlockingReasons, models.FleetCompatibilityReason{Code: entry.Version.Reason, Message: fmt.Sprintf("collector version %q cannot be compared", wl.Version)})
	}
	for _, finding := range unsupported {
		entry.BlockingReasons = append(entry.BlockingReasons, models.FleetCompatibilityReason{Code: "unsupported_component", Message: fmt.Sprintf("%s %q is required by config %s but is not installed", finding.Category, finding.ComponentType, finding.ConfigHash)})
	}
	entry.KnownIssues = compatibilityKnownIssues(wl.Version)
	for _, issue := range entry.KnownIssues {
		if issue.Severity == "blocking" {
			entry.BlockingReasons = append(entry.BlockingReasons, models.FleetCompatibilityReason{Code: "known_issue", Message: issue.Message})
		}
	}
	if !wl.AcceptsRemoteConfig {
		entry.BlockingReasons = append(entry.BlockingReasons, models.FleetCompatibilityReason{Code: "remote_config_not_accepted", Message: "collector has not reported OpAMP remote config acceptance"})
	}
	entry.Runnable = len(entry.BlockingReasons) == 0
	return entry
}

func compatibilityVersion(raw, versionStatus string) models.FleetCompatibilityVersion {
	reported := strings.TrimSpace(raw)
	if reported == "" {
		return models.FleetCompatibilityVersion{Reported: "unknown", Status: "unknown", Comparable: false, Reason: "unknown_version"}
	}
	if _, ok := version.Compare(reported, reported); !ok {
		return models.FleetCompatibilityVersion{Reported: reported, Status: "unknown", Comparable: false, Reason: "invalid_version"}
	}
	return models.FleetCompatibilityVersion{Reported: reported, Status: versionStatus, Comparable: true}
}

func compatibilityAvailable(available *models.AvailableComponents) models.FleetCompatibilityAvailable {
	out := models.FleetCompatibilityAvailable{Categories: []string{}, ComponentTypes: map[string][]string{}}
	if available == nil {
		return out
	}
	out.Hash = available.Hash
	for category, components := range available.Components {
		out.Categories = append(out.Categories, category)
		out.ComponentTypes[category] = append([]string(nil), components...)
		sort.Strings(out.ComponentTypes[category])
	}
	sort.Strings(out.Categories)
	return out
}

func componentCapabilityBlockers(required []models.FleetCompatibilityComponent, available *models.AvailableComponents) []models.FleetCompatibilityReason {
	if len(required) == 0 {
		return nil
	}
	if available == nil || len(available.Components) == 0 {
		return []models.FleetCompatibilityReason{{
			Code:    "component_capabilities_unknown",
			Message: "collector has not reported component capabilities for compatibility checks",
		}}
	}
	requiredCategories := map[string]bool{}
	for _, component := range required {
		requiredCategories[component.Category] = true
	}
	categories := make([]string, 0, len(requiredCategories))
	for category := range requiredCategories {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	blockers := []models.FleetCompatibilityReason{}
	for _, category := range categories {
		components, ok := available.Components[category]
		if !ok || len(components) == 0 {
			blockers = append(blockers, models.FleetCompatibilityReason{
				Code:    "component_capability_category_missing",
				Message: fmt.Sprintf("collector capabilities do not report %s needed by this config", category),
			})
		}
	}
	return blockers
}

func compatibilityOpAMP(wl models.Workload) models.FleetCompatibilityOpAMP {
	out := models.FleetCompatibilityOpAMP{AcceptsRemoteConfig: wl.AcceptsRemoteConfig}
	if wl.RemoteConfigStatus != nil {
		out.RemoteConfigStatus = wl.RemoteConfigStatus.Status
		out.ConfigHash = wl.RemoteConfigStatus.ConfigHash
	}
	return out
}

func compatibilityKnownIssues(rawVersion string) []models.FleetCompatibilityKnownIssue {
	if rawVersion == "" {
		return []models.FleetCompatibilityKnownIssue{}
	}
	if cmp, ok := version.Compare(rawVersion, "0.80.0"); ok && cmp < 0 {
		return []models.FleetCompatibilityKnownIssue{{
			Code:            "collector_pre_0_80_remote_config_issue",
			Severity:        "blocking",
			AffectedVersion: rawVersion,
			Message:         "collector versions before 0.80.0 are blocked by the local compatibility catalog for remote config safety",
		}}
	}
	return []models.FleetCompatibilityKnownIssue{}
}

func requiredComponentsFromConfig(content string) []models.FleetCompatibilityComponent {
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return []models.FleetCompatibilityComponent{}
	}
	out := []models.FleetCompatibilityComponent{}
	seen := map[string]bool{}
	for _, category := range []string{"receivers", "processors", "exporters", "extensions", "connectors"} {
		section, ok := doc[category].(map[string]any)
		if !ok {
			continue
		}
		ids := make([]string, 0, len(section))
		for id := range section {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			componentType := id
			if idx := strings.Index(id, "/"); idx >= 0 {
				componentType = id[:idx]
			}
			key := category + "\x00" + componentType
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, models.FleetCompatibilityComponent{Category: category, ComponentType: componentType, Path: category + "." + componentType})
		}
	}
	return out
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
