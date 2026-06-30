package oteldiff

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func hasRule(items []RiskItem, rule string) bool {
	for _, item := range items {
		if item.Rule == rule {
			return true
		}
	}
	return false
}

func hasSecurityRule(items []SecurityDiff, rule string) bool {
	for _, item := range items {
		for _, r := range item.Rules {
			if r == rule {
				return true
			}
		}
	}
	return false
}

func assertNoSecretLeak(t *testing.T, diff ConfigDiff, secrets ...string) {
	t.Helper()
	b, err := json.Marshal(diff)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.ToLower(string(b))
	for _, secret := range secrets {
		if strings.Contains(body, strings.ToLower(secret)) {
			t.Fatalf("diff response leaked secret %q in JSON: %s", secret, string(b))
		}
	}
}

func TestCompareOTelConfigsLowMediumHighFixtures(t *testing.T) {
	tests := []struct {
		name         string
		target       string
		want         Risk
		wantEndpoint bool
		wantRules    []string
	}{
		{name: "benign processor added", target: "low-target.yaml", want: RiskLow},
		{name: "receiver endpoint exposure changed", target: "medium-target.yaml", want: RiskMedium, wantEndpoint: true, wantRules: []string{"receiver_endpoint_exposure_changed"}},
		{name: "dangerous removals and security changes", target: "high-target.yaml", want: RiskHigh, wantEndpoint: true, wantRules: []string{"batch_processor_removed_from_pipeline", "memory_limiter_removed_from_pipeline", "pipeline_removed", "pipeline_broken", "otlp_endpoint_changed", "transport_security_weakened", "auth_header_modified"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Compare(loadFixture(t, "base.yaml"), loadFixture(t, tt.target))
			if !got.Valid {
				t.Fatalf("expected valid diff, diagnostics=%v", got.Diagnostics)
			}
			if got.SchemaVersion != "otel-config-diff.v1" {
				t.Fatalf("unexpected schema version %q", got.SchemaVersion)
			}
			if got.Summary.OverallRisk != tt.want {
				t.Fatalf("overall risk = %q, want %q", got.Summary.OverallRisk, tt.want)
			}
			if got.Components == nil || got.Pipelines == nil || got.Endpoints == nil || got.Security == nil || got.RiskItems == nil || got.Diagnostics == nil {
				t.Fatalf("DTO sections must be non-nil: %#v", got)
			}
			if tt.wantEndpoint && len(got.Endpoints) == 0 {
				t.Fatalf("expected endpoint diff")
			}
			for _, rule := range tt.wantRules {
				if !hasRule(got.RiskItems, rule) {
					t.Fatalf("missing risk rule %q in %#v", rule, got.RiskItems)
				}
			}
		})
	}
}

func TestCompareDetectsStrongSamplingChange(t *testing.T) {
	got := Compare(loadFixture(t, "sampling-base.yaml"), loadFixture(t, "sampling-target.yaml"))
	if got.Summary.OverallRisk != RiskHigh {
		t.Fatalf("risk = %q, want high", got.Summary.OverallRisk)
	}
	if !hasRule(got.RiskItems, "sampling_rate_strongly_changed") {
		t.Fatalf("missing sampling risk item: %#v", got.RiskItems)
	}
}

func TestCompareRedactsAuthHeadersAndEndpointCredentials(t *testing.T) {
	got := Compare(loadFixture(t, "base.yaml"), loadFixture(t, "high-target.yaml"))
	if !hasSecurityRule(got.Security, "auth_header_modified") {
		t.Fatalf("missing auth security diff: %#v", got.Security)
	}
	assertNoSecretLeak(t, got, "new-secret", "new-secret-token", "literal-api-key", "query-secret", "collector:new-secret", "OTEL_TOKEN:-literal")
	b, _ := json.Marshal(got)
	if !strings.Contains(strings.ToLower(string(b)), "authorization") {
		t.Fatalf("expected header name to remain visible in redacted response: %s", string(b))
	}
	if !strings.Contains(string(b), "••••masked••••") {
		t.Fatalf("expected masked sentinel in response: %s", string(b))
	}
}

func TestCompareRedactsChangedAuthorizationHeaderValues(t *testing.T) {
	got := Compare(loadFixture(t, "auth-base.yaml"), loadFixture(t, "auth-target.yaml"))
	if got.Summary.OverallRisk != RiskHigh {
		t.Fatalf("risk = %q, want high", got.Summary.OverallRisk)
	}
	assertNoSecretLeak(t, got, "old-token", "new-token")
	if !hasRule(got.RiskItems, "auth_header_modified") {
		t.Fatalf("missing auth_header_modified risk item: %#v", got.RiskItems)
	}
}

func TestCompareInvalidYAMLReturnsDiagnosticWithoutSecretEcho(t *testing.T) {
	got := Compare([]byte(`exporters:
  otlp:
    headers:
      Authorization: secret-before
    broken: [unterminated`), loadFixture(t, "base.yaml"))
	if got.Valid {
		t.Fatalf("expected invalid diff")
	}
	if got.Summary.OverallRisk != RiskNone {
		t.Fatalf("risk = %q, want none for unavailable diff", got.Summary.OverallRisk)
	}
	if len(got.Diagnostics) == 0 || got.Diagnostics[0].Code != "yaml_parse" {
		t.Fatalf("expected yaml_parse diagnostic, got %#v", got.Diagnostics)
	}
	assertNoSecretLeak(t, got, "secret-before")
}
