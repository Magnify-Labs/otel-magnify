package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

const configDriftPendingThreshold = 15 * time.Minute

func (a *API) handleListConfigDrift(w http.ResponseWriter, _ *http.Request) {
	now := time.Now().UTC()
	workloads, err := a.db.ListWorkloads(false)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list workloads")
		return
	}
	alerts, err := a.db.ListAlerts(false)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list alerts")
		return
	}

	alertRules := unresolvedAlertRulesByWorkload(alerts)
	groupKeys := map[string]map[string]struct{}{}
	items := make([]models.ConfigDriftItem, 0, len(workloads))

	for _, wl := range workloads {
		if wl.Type != "collector" {
			continue
		}
		a.hydrateCurrentConfigPush(&wl)
		item := a.buildConfigDriftItem(wl, alertRules[wl.ID], now)
		items = append(items, item)

		group := configDriftGroupKey(item)
		if group != "" {
			if _, ok := groupKeys[group]; !ok {
				groupKeys[group] = map[string]struct{}{}
			}
			groupKeys[group][configStateKey(item)] = struct{}{}
		}
	}

	heterogeneousGroups := map[string]bool{}
	for group, keys := range groupKeys {
		if len(keys) > 1 {
			heterogeneousGroups[group] = true
		}
	}

	summary := models.ConfigDriftSummary{TotalCollectors: len(items), HeterogeneousGroups: len(heterogeneousGroups)}
	for i := range items {
		group := configDriftGroupKey(items[i])
		if heterogeneousGroups[group] {
			items[i].GroupHeterogeneousConfig = true
			items[i].DriftReasons = appendUnique(items[i].DriftReasons, "group_heterogeneous_config")
		}
		summarizeConfigDriftItem(&summary, items[i])
	}
	sort.Slice(items, func(i, j int) bool {
		if rankConfigDriftStatus(items[i]) != rankConfigDriftStatus(items[j]) {
			return rankConfigDriftStatus(items[i]) < rankConfigDriftStatus(items[j])
		}
		return strings.ToLower(items[i].Collector) < strings.ToLower(items[j].Collector)
	})

	respondJSON(w, http.StatusOK, models.ConfigDriftDashboard{GeneratedAt: now, Summary: summary, Items: items})
}

func (a *API) buildConfigDriftItem(wl models.Workload, alerts map[string]bool, now time.Time) models.ConfigDriftItem {
	instances := []string{}
	if a.opamp != nil {
		for _, inst := range a.opamp.Instances(wl.ID) {
			if h := strings.TrimSpace(inst.EffectiveConfigHash); h != "" {
				instances = append(instances, h)
			}
		}
	}
	effectiveHashes := uniqueSortedStrings(instances)
	if len(effectiveHashes) == 0 && wl.RemoteConfigStatus != nil && strings.TrimSpace(wl.RemoteConfigStatus.ConfigHash) != "" {
		effectiveHashes = []string{strings.TrimSpace(wl.RemoteConfigStatus.ConfigHash)}
	}
	effective := ""
	if len(effectiveHashes) == 1 {
		effective = effectiveHashes[0]
	}
	expected := strings.TrimSpace(wl.ActiveConfigHash)
	if expected == "" && wl.CurrentConfigPush != nil {
		expected = strings.TrimSpace(wl.CurrentConfigPush.ConfigID)
	}

	item := models.ConfigDriftItem{
		WorkloadID:                  wl.ID,
		Collector:                   collectorDisplayName(wl),
		Env:                         workloadEnv(wl),
		Version:                     wl.Version,
		ExpectedConfigHash:          expected,
		EffectiveConfigHash:         effective,
		EffectiveConfigHashes:       effectiveHashes,
		AcceptsRemoteConfig:         wl.AcceptsRemoteConfig,
		HasConfigDriftAlert:         alerts["config_drift"],
		HasVersionOutdatedAlert:     alerts["version_outdated"],
		UnknownIncompleteComponents: hasUnknownIncompleteComponents(wl),
	}
	if wl.CurrentConfigPush != nil {
		item.LastPush = safeConfigDriftLastPush(wl.CurrentConfigPush)
		pushTime := configPushReferenceTime(*wl.CurrentConfigPush)
		if !pushTime.IsZero() {
			item.LastPushAgeSeconds = int64(now.Sub(pushTime).Seconds())
		}
		item.PendingTooLong = isPendingPush(*wl.CurrentConfigPush) && now.Sub(pushTime) > configDriftPendingThreshold
	}

	item.MissingEffectiveConfig = expected != "" && len(effectiveHashes) == 0
	item.DriftStatus = deriveDriftStatus(item)
	item.DriftReasons = deriveDriftReasons(item)
	item.Actions = configDriftActions(item)
	return item
}

func unresolvedAlertRulesByWorkload(alerts []models.Alert) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, alert := range alerts {
		if alert.ResolvedAt != nil {
			continue
		}
		if _, ok := out[alert.WorkloadID]; !ok {
			out[alert.WorkloadID] = map[string]bool{}
		}
		out[alert.WorkloadID][alert.Rule] = true
	}
	return out
}

func deriveDriftStatus(item models.ConfigDriftItem) string {
	switch {
	case !item.AcceptsRemoteConfig:
		return "remote_config_unsupported"
	case item.MissingEffectiveConfig:
		return "missing_effective_config"
	case len(item.EffectiveConfigHashes) > 1:
		return "heterogeneous_effective_config"
	case item.ExpectedConfigHash != "" && item.EffectiveConfigHash != "" && item.ExpectedConfigHash != item.EffectiveConfigHash:
		return "drifted"
	case item.PendingTooLong:
		return "pending_too_long"
	default:
		return "in_sync"
	}
}

func deriveDriftReasons(item models.ConfigDriftItem) []string {
	reasons := []string{}
	if !item.AcceptsRemoteConfig {
		reasons = append(reasons, "remote_config_unsupported")
	}
	if item.MissingEffectiveConfig {
		reasons = append(reasons, "missing_effective_config")
	}
	if len(item.EffectiveConfigHashes) > 1 {
		reasons = append(reasons, "heterogeneous_effective_config")
	}
	if item.ExpectedConfigHash != "" && item.EffectiveConfigHash != "" && item.ExpectedConfigHash != item.EffectiveConfigHash {
		reasons = append(reasons, "drifted")
	}
	if item.PendingTooLong {
		reasons = append(reasons, "pending_too_long")
	}
	if item.HasConfigDriftAlert {
		reasons = append(reasons, "config_drift_alert")
	}
	if item.HasVersionOutdatedAlert {
		reasons = append(reasons, "version_outdated")
	}
	if item.UnknownIncompleteComponents {
		reasons = append(reasons, "unknown_incomplete_components")
	}
	return reasons
}

func configDriftActions(item models.ConfigDriftItem) map[string]models.ConfigDriftAction {
	actions := map[string]models.ConfigDriftAction{
		"view_diff":        disabledAction("diff_requires_config_content"),
		"validate_current": disabledAction("validate_from_workload_detail"),
		"push_expected":    disabledAction("review_expected_before_push"),
		"rollback":         disabledAction("rollback_from_workload_detail"),
		"mark_ignored":     disabledAction("ignore_not_implemented"),
	}
	if !item.AcceptsRemoteConfig {
		actions["push_expected"] = disabledAction("remote_config_unsupported")
		actions["rollback"] = disabledAction("remote_config_unsupported")
	}
	if item.AcceptsRemoteConfig && item.PendingTooLong {
		actions["push_expected"] = disabledAction("push_pending_too_long")
	}
	return actions
}

func disabledAction(reason string) models.ConfigDriftAction {
	return models.ConfigDriftAction{Enabled: false, Reason: reason}
}

func summarizeConfigDriftItem(summary *models.ConfigDriftSummary, item models.ConfigDriftItem) {
	if item.DriftStatus == "drifted" || item.HasConfigDriftAlert {
		summary.DriftedCollectors++
	}
	if item.PendingTooLong {
		summary.PendingTooLong++
	}
	if item.MissingEffectiveConfig {
		summary.MissingEffectiveConfig++
	}
	if !item.AcceptsRemoteConfig {
		summary.RemoteConfigUnsupported++
	}
	if item.HasVersionOutdatedAlert {
		summary.OutdatedVersions++
	}
	if item.UnknownIncompleteComponents {
		summary.UnknownIncompleteComponents++
	}
}

func rankConfigDriftStatus(item models.ConfigDriftItem) int {
	if item.HasConfigDriftAlert || item.DriftStatus == "drifted" {
		return 0
	}
	switch item.DriftStatus {
	case "missing_effective_config", "heterogeneous_effective_config", "remote_config_unsupported":
		return 1
	case "pending_too_long":
		return 2
	default:
		return 3
	}
}

func uniqueSortedStrings(in []string) []string {
	seen := map[string]struct{}{}
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			seen[s] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func collectorDisplayName(wl models.Workload) string {
	if wl.DisplayName != "" {
		return wl.DisplayName
	}
	return wl.ID
}

func workloadEnv(wl models.Workload) string {
	for _, key := range []string{"env", "environment", "deployment.environment"} {
		if v := strings.TrimSpace(wl.Labels[key]); v != "" {
			return v
		}
	}
	return "unknown"
}

func hasUnknownIncompleteComponents(wl models.Workload) bool {
	if wl.AvailableComponents == nil || len(wl.AvailableComponents.Components) == 0 {
		return true
	}
	for _, category := range []string{"receivers", "processors", "exporters"} {
		if _, ok := wl.AvailableComponents.Components[category]; !ok {
			return true
		}
	}
	return false
}

func configPushReferenceTime(push models.WorkloadConfig) time.Time {
	if push.SentAt != nil {
		return *push.SentAt
	}
	if !push.SubmittedAt.IsZero() {
		return push.SubmittedAt
	}
	return push.AppliedAt
}

func isPendingPush(push models.WorkloadConfig) bool {
	switch push.Status {
	case models.PushStatusSubmitted, models.PushStatusSent, models.PushStatusApplying:
		return true
	default:
		return false
	}
}

func safeConfigDriftLastPush(push *models.WorkloadConfig) *models.WorkloadConfig {
	if push == nil {
		return nil
	}
	return &models.WorkloadConfig{
		WorkloadID:                    push.WorkloadID,
		ConfigID:                      push.ConfigID,
		ConfigHash:                    push.ConfigHash,
		AppliedAt:                     push.AppliedAt,
		Status:                        push.Status,
		PushID:                        push.PushID,
		SubmittedAt:                   push.SubmittedAt,
		SentAt:                        push.SentAt,
		UpdatedAt:                     push.UpdatedAt,
		OpAMPStatusTimeoutAt:          push.OpAMPStatusTimeoutAt,
		TimedOutWaitingForOpAMPStatus: push.TimedOutWaitingForOpAMPStatus,
		TimeoutMessage:                push.TimeoutMessage,
		RollbackOfPushID:              push.RollbackOfPushID,
		TargetCount:                   push.TargetCount,
		AppliedCount:                  push.AppliedCount,
		FailedCount:                   push.FailedCount,
		PendingCount:                  push.PendingCount,
		TimedOutCount:                 push.TimedOutCount,
		NoStatusCount:                 push.NoStatusCount,
		IsCurrent:                     push.IsCurrent,
		IsPrevious:                    push.IsPrevious,
		IsLastKnownGood:               push.IsLastKnownGood,
		IsFailedCandidate:             push.IsFailedCandidate,
		ContentAvailable:              push.ContentAvailable,
	}
}

func configDriftGroupKey(item models.ConfigDriftItem) string {
	if item.Env == "" || item.Env == "unknown" {
		return ""
	}
	return item.Env
}

func configStateKey(item models.ConfigDriftItem) string {
	return item.ExpectedConfigHash + "|" + strings.Join(item.EffectiveConfigHashes, ",")
}
