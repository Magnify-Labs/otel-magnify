package api

import "net/http"

const (
	// FeatureConfigSafetyApprovals gates config approval workflow endpoints.
	FeatureConfigSafetyApprovals = "config_safety.approvals"
	// FeatureConfigSafetyGuidedRollback gates rollback and known-good endpoints.
	FeatureConfigSafetyGuidedRollback = "config_safety.guided_rollback"
	// FeatureConfigSafetyCanaryRollout gates canary validation and lifecycle endpoints.
	FeatureConfigSafetyCanaryRollout = "config_safety.canary_rollout"
	// FeatureConfigSafetyScopedPush gates scoped push preview endpoints.
	FeatureConfigSafetyScopedPush = "config_safety.scoped_push"
	// FeatureConfigSafetyDriftDashboard gates fleet drift dashboard endpoints.
	FeatureConfigSafetyDriftDashboard = "config_safety.drift_dashboard"
	// FeatureConfigSafetyVersionIntelligence gates fleet version intelligence endpoints.
	FeatureConfigSafetyVersionIntelligence = "config_safety.version_intelligence"
	// FeatureConfigSafetyGitOpsExport gates GitOps import/export and webhook endpoints.
	FeatureConfigSafetyGitOpsExport = "config_safety.gitops_export"
	// FeatureConfigSafetyPolicyPreview gates config policy/plan preview endpoints.
	FeatureConfigSafetyPolicyPreview = "config_safety.policy_preview"
	// FeatureReportsEvidencePack gates evidence-pack report preview/export endpoints.
	FeatureReportsEvidencePack = "reports.evidence_pack"
	// FeatureAuditViewer gates audit viewer and CSV export endpoints.
	FeatureAuditViewer = "audit.viewer"
)

// RequireFeature blocks access to edition extension endpoint groups unless
// the static API feature map or the optional license checker enables the named
// capability. The response body is intentionally stable and machine-readable so
// clients can render disabled-feature states without scraping prose.
func (a *API) RequireFeature(feature string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !a.featureEnabled(feature) {
				respondFeatureDisabled(w, feature)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (a *API) featureEnabled(feature string) bool {
	if a == nil || feature == "" {
		return false
	}
	if a.capabilities.Enabled(feature) {
		return true
	}
	return a.licenseChecker != nil && a.licenseChecker.FeatureEnabled(feature)
}

func respondFeatureDisabled(w http.ResponseWriter, feature string) {
	respondJSON(w, http.StatusForbidden, map[string]string{
		"error":   "feature disabled",
		"code":    "feature_disabled",
		"feature": feature,
	})
}
