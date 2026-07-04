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

func hasHumanSummaryText(items []HumanSummaryItem, text string) bool {
	for _, item := range items {
		if item.Text == text {
			return true
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

func assertNoHumanSummarySecretLeak(t *testing.T, diff ConfigDiff, secrets ...string) {
	t.Helper()
	b, err := json.Marshal(diff.HumanSummary)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.ToLower(string(b))
	for _, secret := range secrets {
		if strings.Contains(body, strings.ToLower(secret)) {
			t.Fatalf("human_summary leaked secret %q in JSON: %s", secret, string(b))
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

func TestCompareBuildsHumanSummaryForOTelChanges(t *testing.T) {
	base := []byte(`receivers:
  otlp:
    protocols:
      grpc: {}
processors:
  batch:
    timeout: 5s
exporters:
  debug: {}
  otlp:
    endpoint: https://tempo.example.com:4317
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlp]
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [debug]
`)
	target := []byte(`receivers:
  otlp:
    protocols:
      grpc: {}
processors:
  batch:
    timeout: 10s
exporters:
  loki:
    endpoint: https://loki.example.com/loki/api/v1/push?tenant=prod
    headers:
      Authorization: not-for-summary
  otlp:
    endpoint: https://tempo.example.com:4317
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlp]
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [loki]
`)

	got := Compare(base, target)

	if !got.Valid {
		t.Fatalf("expected valid diff, diagnostics=%v", got.Diagnostics)
	}
	wantTexts := []string{
		"Adds Loki exporter",
		"Routes logs to Loki",
		"Keeps traces unchanged",
		"Removes debug exporter",
		"Changes batch timeout",
	}
	for _, text := range wantTexts {
		if !hasHumanSummaryText(got.HumanSummary, text) {
			t.Fatalf("missing human summary %q in %#v", text, got.HumanSummary)
		}
	}
	if len(got.HumanSummary) != len(wantTexts) {
		t.Fatalf("human summary length = %d, want %d: %#v", len(got.HumanSummary), len(wantTexts), got.HumanSummary)
	}
	assertNoSecretLeak(t, got, "not-for-summary")
}

func TestCompareHumanSummaryRedactsSecretLikeComponentRefs(t *testing.T) {
	base := []byte(`receivers:
  otlp:
    protocols:
      grpc: {}
processors:
  batch:
    timeout: 5s
exporters:
  debug: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [debug]
`)
	target := []byte(`receivers:
  otlp:
    protocols:
      grpc: {}
processors:
  batch:
    timeout: 5s
exporters:
  debug: {}
  debug/super-secret-token:
    endpoint: https://tempo.example.com:4317
    headers:
      Authorization: SECRET_LITERAL
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [debug, debug/super-secret-token]
`)

	got := Compare(base, target)

	if !got.Valid {
		t.Fatalf("expected valid diff, diagnostics=%v", got.Diagnostics)
	}
	assertNoHumanSummarySecretLeak(t, got, "super-secret-token", "SECRET_LITERAL", "Authorization")
	if !hasHumanSummaryText(got.HumanSummary, "Adds "+MaskedValue+" exporter") {
		t.Fatalf("missing redacted component addition summary in %#v", got.HumanSummary)
	}
	if !hasHumanSummaryText(got.HumanSummary, "Routes traces to "+MaskedValue) {
		t.Fatalf("missing redacted routing summary in %#v", got.HumanSummary)
	}
}

func TestCompareRedactsAuthHeadersAndEndpointCredentials(t *testing.T) {
	got := Compare(loadFixture(t, "base.yaml"), loadFixture(t, "high-target.yaml"))
	if got.RiskScore.Severity != string(RiskHigh) {
		t.Fatalf("risk score severity = %q, want high", got.RiskScore.Severity)
	}
	for _, want := range []string{"Pipeline logs removed", "OTLP endpoint changed", "Memory limiter removed from pipeline"} {
		if !containsString(got.RiskScore.Reasons, want) {
			t.Fatalf("risk score missing reason %q in %#v", want, got.RiskScore.Reasons)
		}
	}
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

func TestCompareRiskScoreNoneHasEmptyReasons(t *testing.T) {
	got := Compare(loadFixture(t, "base.yaml"), loadFixture(t, "base.yaml"))
	if got.RiskScore.Severity != string(RiskNone) {
		t.Fatalf("risk score severity = %q, want none", got.RiskScore.Severity)
	}
	if len(got.RiskScore.Reasons) != 0 {
		t.Fatalf("risk score reasons = %#v, want empty", got.RiskScore.Reasons)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
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

func TestCompareWithContextPopulatesBlastRadius(t *testing.T) {
	base := []byte(`receivers:
  otlp: {}
processors:
  batch: {}
exporters:
  otlp/prod:
    endpoint: https://old.example:4317?token=old-secret
  debug: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlp/prod]
    metrics:
      receivers: [otlp]
      exporters: [debug]
`)
	target := []byte(`receivers:
  otlp: {}
processors:
  batch: {}
exporters:
  otlp/prod:
    endpoint: https://new.example:4317?token=new-secret
  otlp/archive:
    endpoint: https://archive.example:4317
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlp/archive]
    logs:
      receivers: [otlp]
      exporters: [otlp/archive]
`)

	got := CompareWithContext(base, target, BlastRadiusContext{
		Workload: BlastRadiusWorkload{
			ID:          "wl-prod-collector",
			DisplayName: "prod collector",
			Type:        "collector",
			Status:      "degraded",
			Labels: map[string]string{
				"service.name":           "checkout-api",
				"k8s.namespace.name":     "checkout",
				"k8s.cluster.name":       "prod-eu-1",
				"deployment.environment": "production",
				"authorization":          "Bearer should-not-leak",
			},
			FingerprintKeys: map[string]string{"service.name": "checkout-api", "cluster": "prod-eu-1"},
		},
		FleetPeers: []BlastRadiusWorkload{
			{
				ID:          "wl-payments",
				DisplayName: "payments collector",
				Type:        "collector",
				Status:      "connected",
				Labels:      map[string]string{"service": "payments-api", "cluster": "prod-eu-1", "tier": "critical"},
			},
			{
				ID:          "wl-fraud",
				DisplayName: "fraud collector",
				Type:        "collector",
				Status:      "connected",
				Labels:      map[string]string{"service": "fraud-api", "cluster": "staging-us-1", "tier": "critical"},
			},
		},
	})

	if got.BlastRadius.SchemaVersion != BlastRadiusSchemaVersion {
		t.Fatalf("blast radius schema = %q", got.BlastRadius.SchemaVersion)
	}
	assertStringSet(t, got.BlastRadius.AffectedSignals, []string{"logs", "metrics", "traces"})
	assertStringSet(t, got.BlastRadius.TouchedExporters, []string{"debug", "otlp/archive", "otlp/prod"})
	if len(got.BlastRadius.ImpactedServices) != 2 {
		t.Fatalf("impacted services = %#v, want checkout-api and payments-api", got.BlastRadius.ImpactedServices)
	}
	assertStringSet(t, got.BlastRadius.ImpactedClusters, []string{"checkout", "prod-eu-1", "production"})
	if len(got.BlastRadius.CriticalCollectors) != 2 {
		t.Fatalf("critical collectors = %#v, want workload and peer", got.BlastRadius.CriticalCollectors)
	}
	for _, svc := range got.BlastRadius.ImpactedServices {
		if svc.ServiceName == "fraud-api" || svc.WorkloadID == "wl-fraud" {
			t.Fatalf("unrelated peer leaked into impacted services: %#v", got.BlastRadius.ImpactedServices)
		}
	}
	for _, collector := range got.BlastRadius.CriticalCollectors {
		if collector.WorkloadID == "wl-fraud" {
			t.Fatalf("unrelated peer leaked into critical collectors: %#v", got.BlastRadius.CriticalCollectors)
		}
	}
	assertNoSecretLeak(t, got, "old-secret", "new-secret", "should-not-leak")
}

func TestCompareBlastRadiusEmptySafeWithoutContext(t *testing.T) {
	got := Compare([]byte(`receivers:
  otlp: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: []
`), []byte(`receivers:
  otlp: {}
service:
  pipelines:
    metrics:
      receivers: [otlp]
      exporters: []
`))

	if got.BlastRadius.SchemaVersion != BlastRadiusSchemaVersion {
		t.Fatalf("blast radius schema = %q", got.BlastRadius.SchemaVersion)
	}
	assertStringSet(t, got.BlastRadius.AffectedSignals, []string{"metrics", "traces"})
	if got.BlastRadius.TouchedExporters == nil || got.BlastRadius.ImpactedServices == nil || got.BlastRadius.ImpactedClusters == nil || got.BlastRadius.CriticalCollectors == nil {
		t.Fatalf("blast radius arrays must be non-nil: %#v", got.BlastRadius)
	}
	if len(got.BlastRadius.TouchedExporters) != 0 || len(got.BlastRadius.ImpactedServices) != 0 || len(got.BlastRadius.ImpactedClusters) != 0 || len(got.BlastRadius.CriticalCollectors) != 0 {
		t.Fatalf("unexpected context-derived blast radius without context: %#v", got.BlastRadius)
	}
}

func assertStringSet(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("strings = %#v, want %#v", got, want)
	}
	seen := map[string]bool{}
	for _, v := range got {
		seen[v] = true
	}
	for _, v := range want {
		if !seen[v] {
			t.Fatalf("strings = %#v, missing %q", got, v)
		}
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
