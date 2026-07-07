package oteldiff

import (
	"strings"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// BuildRiskScore reduces the full semantic diff to the stable, concise pre-push
// risk contract used by config diff and config plan APIs.
func BuildRiskScore(diff ConfigDiff, appliesToCount int) models.ConfigRiskScore {
	reasons := make([]string, 0, len(diff.RiskItems))
	seen := map[string]bool{}
	for _, item := range diff.RiskItems {
		reason := riskReason(item)
		if reason == "" || seen[reason] {
			continue
		}
		seen[reason] = true
		reasons = append(reasons, reason)
	}
	return models.ConfigRiskScore{Severity: string(diff.Summary.OverallRisk), Reasons: reasons, AppliesToCount: appliesToCount}
}

func riskReason(item RiskItem) string {
	switch item.Rule {
	case "pipeline_removed":
		if len(item.AffectedPipelines) > 0 {
			return "Pipeline " + item.AffectedPipelines[0] + " removed"
		}
		return "Pipeline removed"
	case "otlp_endpoint_changed":
		return "OTLP endpoint changed"
	case "memory_limiter_removed_from_pipeline":
		return "Memory limiter removed from pipeline"
	case "batch_processor_removed_from_pipeline":
		return "Batch processor removed from pipeline"
	case "pipeline_broken":
		return "Pipeline broken"
	case "transport_security_weakened":
		return "Transport security weakened"
	case "auth_header_modified":
		return "Auth/header modified"
	case "exporter_removed":
		return "Exporter removed"
	case "sampling_guard_removed":
		return "Sampling guard removed"
	case "sampling_rate_strongly_changed":
		return "Sampling rate strongly changed"
	case "receiver_endpoint_exposure_changed":
		return "Receiver endpoint exposure changed"
	case "pipeline_exporter_set_changed":
		return "Pipeline exporter set changed"
	default:
		return strings.TrimSpace(item.Title)
	}
}
