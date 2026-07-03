package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

const policySafeCurrentConfig = `
receivers:
  otlp: {}
processors:
  memory_limiter: {}
  resource:
    attributes:
      - key: service.name
        value: checkout
        action: upsert
      - key: deployment.environment
        value: production
        action: upsert
  batch: {}
exporters:
  otlp/allowed:
    endpoint: https://allowed.example:4317
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, resource, batch]
      exporters: [otlp/allowed]
`

const policyUnsafeCandidateConfig = `
receivers:
  otlp: {}
processors:
  probabilistic_sampler:
    sampling_percentage: 0.01
exporters:
  otlp/vendor:
    endpoint: https://blocked.example:4317
    tls:
      insecure: true
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [probabilistic_sampler]
      exporters: [otlp/vendor]
`

func TestPreviewConfigPolicy_ReturnsBlockFindingsForCandidateAgainstCurrent(t *testing.T) {
	_, router, _ := newTestAPI(t)

	body := `{"candidate_yaml":` + quoteJSON(policyUnsafeCandidateConfig) + `,"current_yaml":` + quoteJSON(policySafeCurrentConfig) + `,"target":{"environment":"production","scope":"workload"},"settings":{"allowed_otlp_endpoints":["allowed.example"]}}`
	req := authedJSONRequest(t, http.MethodPost, "/api/configs/policy/preview", body, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp models.ConfigPolicyEvaluation
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.SchemaVersion != "config-policy.v1" || resp.Decision != models.PolicyDecisionBlock || resp.Summary.BlockCount == 0 {
		t.Fatalf("unexpected policy response: %+v", resp)
	}
	if !policyResponseHasRule(resp, "community.exporters.critical_removal") {
		t.Fatalf("critical removal finding missing: %+v", resp.Findings)
	}
	if resp.Audit.Persisted {
		t.Fatalf("preview should not claim persisted audit: %+v", resp.Audit)
	}
}

func TestPlanWorkloadConfig_IncludesPolicyAndBlocksUnsafeCandidate(t *testing.T) {
	db, router, _ := newTestAPI(t)
	db.UpsertWorkload(models.Workload{
		ID:                  "collector-prod",
		Type:                "collector",
		Status:              "connected",
		LastSeenAt:          time.Now().UTC(),
		Labels:              models.Labels{"env": "production"},
		AcceptsRemoteConfig: true,
	})

	req := authedPost(t, "/api/workloads/collector-prod/config/plan", policyUnsafeCandidateConfig)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var plan models.ConfigApplicationPlan
	if err := json.NewDecoder(rec.Body).Decode(&plan); err != nil {
		t.Fatal(err)
	}
	if plan.CanPush || plan.ApplyAllowed || !policyResponseHasRule(plan.Policy, "community.production.insecure_tls") {
		t.Fatalf("plan did not expose policy block: %+v", plan)
	}
}

func TestPushWorkloadConfig_LegacyEndpointRequiresApprovalBeforePolicyPush(t *testing.T) {
	db, router, fake := newTestAPI(t)
	db.UpsertWorkload(models.Workload{
		ID:                  "collector-prod",
		Type:                "collector",
		Status:              "connected",
		LastSeenAt:          time.Now().UTC(),
		Labels:              models.Labels{"env": "production"},
		AcceptsRemoteConfig: true,
	})

	req := authedPost(t, "/api/workloads/collector-prod/config", policyUnsafeCandidateConfig)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusGone {
		t.Fatalf("status = %d, want 410, body = %s", rec.Code, rec.Body.String())
	}
	if len(fake.pushed) != 0 {
		t.Fatalf("opamp push happened despite approval gate: %+v", fake.pushed)
	}
	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["code"] != "config_approval_required" {
		t.Fatalf("unexpected legacy push response: %+v", resp)
	}
}

func quoteJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func policyResponseHasRule(resp models.ConfigPolicyEvaluation, code string) bool {
	for _, finding := range resp.Findings {
		if strings.EqualFold(finding.RuleCode, code) {
			return true
		}
	}
	return false
}
