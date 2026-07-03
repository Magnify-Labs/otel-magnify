package configpolicy

import "testing"

const productionUnsafeConfig = `
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
processors:
  probabilistic_sampler:
    sampling_percentage: 0.01
exporters:
  otlp/vendor:
    endpoint: https://telemetry.evil.example:4317
    tls:
      insecure: true
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [probabilistic_sampler]
      exporters: [otlp/vendor]
`

func TestDefaultEngineBlocksUnsafeProductionConfigWithDeterministicFindings(t *testing.T) {
	result := NewDefaultEngine().Evaluate(EvaluationRequest{
		CandidateYAML: productionUnsafeConfig,
		Target:        PolicyTarget{Environment: "production", Scope: "workload"},
		Settings: PolicySettings{
			AllowedOTLPEndpoints:       []string{"allowed.example"},
			RequiredResourceAttributes: []string{"service.name", "deployment.environment"},
			Sampling:                   SamplingPolicySettings{MaxPercentage: 50},
		},
	})

	if result.Decision != DecisionBlock || result.Allowed || result.Valid || result.Severity != "critical" {
		t.Fatalf("decision = %q, want %q; findings=%+v", result.Decision, DecisionBlock, result.Findings)
	}
	if result.SchemaVersion != "config-policy.v1" {
		t.Fatalf("schema_version = %q", result.SchemaVersion)
	}
	assertFinding(t, result, "community.production.insecure_tls", "exporters.otlp/vendor.tls.insecure")
	assertFinding(t, result, "community.processors.memory_limiter.required", "processors.memory_limiter")
	assertFinding(t, result, "community.pipelines.batch.required", "service.pipelines.traces.processors")
	assertFinding(t, result, "community.exporters.otlp_endpoint.allowlist", "exporters.otlp/vendor.endpoint")
	assertFinding(t, result, "community.resource.attributes.required", "processors.resource.attributes")
	assertFinding(t, result, "community.sampling.percentage.safe_range", "processors.probabilistic_sampler.sampling_percentage")
}

func TestDefaultEngineAllowsSafeProductionConfig(t *testing.T) {
	result := NewDefaultEngine().Evaluate(EvaluationRequest{
		CandidateYAML: safeProductionConfig,
		Target:        PolicyTarget{Environment: "production", Scope: "workload"},
		Settings: PolicySettings{
			AllowedOTLPEndpoints:       []string{"allowed.example"},
			RequiredResourceAttributes: []string{"service.name", "deployment.environment"},
			Sampling:                   SamplingPolicySettings{MaxPercentage: 50},
		},
	})

	if !result.Valid || !result.Allowed || result.Decision != DecisionPass || len(result.Findings) != 0 {
		t.Fatalf("safe config result = %+v", result)
	}
}

func TestDefaultEngineDoesNotApplyProductionOnlyRulesWithoutProductionContext(t *testing.T) {
	result := NewDefaultEngine().Evaluate(EvaluationRequest{
		CandidateYAML: productionUnsafeConfig,
		Target:        PolicyTarget{Environment: "development", Scope: "workload"},
		Settings: PolicySettings{
			AllowedOTLPEndpoints:       []string{"telemetry.evil.example"},
			RequiredResourceAttributes: []string{"service.name", "deployment.environment"},
			Sampling:                   SamplingPolicySettings{MaxPercentage: 50},
		},
	})

	assertNoFinding(t, result, "community.production.insecure_tls")
	assertNoFinding(t, result, "community.processors.memory_limiter.required")
}

func TestDefaultEngineDoesNotEnforceEndpointAllowlistWhenUnset(t *testing.T) {
	result := NewDefaultEngine().Evaluate(EvaluationRequest{
		CandidateYAML: safeProductionConfig,
		Target:        PolicyTarget{Environment: "production", Scope: "workload"},
		Settings:      PolicySettings{RequiredResourceAttributes: []string{"service.name", "deployment.environment"}},
	})

	assertNoFinding(t, result, "community.exporters.otlp_endpoint.allowlist")
}

func TestDefaultEngineRequiresConfiguredResourceAttributesOnlyWhenEnabled(t *testing.T) {
	result := NewDefaultEngine().Evaluate(EvaluationRequest{
		CandidateYAML: safeProductionConfig,
		Target:        PolicyTarget{Environment: "production", Scope: "workload"},
		Settings: PolicySettings{
			AllowedOTLPEndpoints:       []string{"allowed.example"},
			RequiredResourceAttributes: []string{"service.name", "deployment.environment", "service.namespace"},
		},
	})

	assertFinding(t, result, "community.resource.attributes.required", "processors.resource.attributes")
}

func TestDefaultEngineBlocksRemovalOfCriticalExportersFromCurrentConfig(t *testing.T) {
	current := `
receivers:
  otlp: {}
processors:
  memory_limiter: {}
  batch: {}
  resource:
    attributes:
      - key: service.name
        value: checkout
        action: upsert
      - key: deployment.environment
        value: production
        action: upsert
exporters:
  otlp/critical:
    endpoint: https://allowed.example:4317
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, resource, batch]
      exporters: [otlp/critical]
`
	candidate := `
receivers:
  otlp: {}
processors:
  memory_limiter: {}
  batch: {}
  resource:
    attributes:
      - key: service.name
        value: checkout
        action: upsert
      - key: deployment.environment
        value: production
        action: upsert
exporters:
  debug: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, resource, batch]
      exporters: [debug]
`

	result := NewDefaultEngine().Evaluate(EvaluationRequest{
		CurrentYAML:   current,
		CandidateYAML: candidate,
		Target:        PolicyTarget{Environment: "production", Scope: "workload"},
		Settings:      PolicySettings{AllowedOTLPEndpoints: []string{"allowed.example"}},
	})

	if result.Decision != DecisionBlock {
		t.Fatalf("decision = %q, want %q; findings=%+v", result.Decision, DecisionBlock, result.Findings)
	}
	assertFinding(t, result, "community.exporters.critical_removal", "service.pipelines.traces.exporters")
}

func TestDefaultEngineNormalizesOTLPEndpointAllowlistEntries(t *testing.T) {
	allowedEndpoints := []string{
		"allowed.example",
		"allowed.example:4317",
		"https://allowed.example:4317/v1/traces",
		"https://user:secret@allowed.example:4317/v1/traces",
		"[2001:db8::1]:4317",
		"https://2001-db8.example:4317",
		"",
		"http://[::1",
	}
	for _, endpoint := range []string{
		"allowed.example",
		"allowed.example:4317",
		"https://allowed.example:4317",
		"https://sub.allowed.example:4317/v1/traces",
		"https://user:secret@allowed.example:4317/v1/traces",
		"[2001:db8::1]:4317",
		"https://2001-db8.example:4317",
	} {
		if !endpointAllowed(endpoint, allowedEndpoints) {
			t.Fatalf("endpointAllowed(%q) = false, want true", endpoint)
		}
	}
	for _, endpoint := range []string{"blocked.example:4317", "https://allowed.example.evil:4317", "http://[::1"} {
		if endpointAllowed(endpoint, allowedEndpoints) {
			t.Fatalf("endpointAllowed(%q) = true, want false", endpoint)
		}
	}
}

func assertFinding(t *testing.T, result EvaluationResult, code, path string) {
	t.Helper()
	for _, finding := range result.Findings {
		if finding.RuleID == code && len(finding.Paths) == 1 && finding.Paths[0] == path {
			if finding.RuleID == "" || finding.PolicyID == "" || finding.PolicyName == "" || finding.Message == "" || finding.Remediation == "" || finding.Severity == "" || finding.Decision == "" || finding.Packaging == "" || finding.Tier == "" {
				t.Fatalf("finding missing required contract fields: %+v", finding)
			}
			return
		}
	}
	t.Fatalf("finding %s at %s not found in %+v", code, path, result.Findings)
}

func assertNoFinding(t *testing.T, result EvaluationResult, code string) {
	t.Helper()
	for _, finding := range result.Findings {
		if finding.RuleID == code {
			t.Fatalf("unexpected finding %s in %+v", code, result.Findings)
		}
	}
}

const safeProductionConfig = `
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
  probabilistic_sampler:
    sampling_percentage: 10
  batch: {}
exporters:
  otlp/vendor:
    endpoint: https://allowed.example:4317
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, resource, probabilistic_sampler, batch]
      exporters: [otlp/vendor]
`
