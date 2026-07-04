package migrationassistant

import (
	"strings"
	"testing"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestAssistantPreviewConvertsSupportedVendors(t *testing.T) {
	tests := []struct {
		name           string
		vendor         string
		sourceFormat   string
		source         string
		wantReceiver   string
		wantPipeline   string
		wantEvidence   string
		forbiddenValue string
	}{
		{
			name:         "datadog file log",
			vendor:       models.ConfigMigrationVendorDatadogAgent,
			sourceFormat: "yaml",
			source: `api_key: dd-secret-token
site: datadoghq.eu
env: prod
logs:
  - type: file
    path: /var/log/app.log
    service: checkout
    source: go
    log_processing_rules:
      - type: mask_sequences
`,
			wantReceiver:   "filelog/datadog_logs_0",
			wantPipeline:   "logs:",
			wantEvidence:   "datadog.logs.file.path_to_filelog.include",
			forbiddenValue: "dd-secret-token",
		},
		{
			name:         "fluent bit tail",
			vendor:       models.ConfigMigrationVendorFluentBit,
			sourceFormat: "conf",
			source: `[INPUT]
    Name tail
    Path /var/log/containers/*.log
    Tag kube.*
    Parser json
[FILTER]
    Name kubernetes
    Match kube.*
[OUTPUT]
    Name opentelemetry
    Host otel-collector
`,
			wantReceiver: "filelog/fluentbit_tail_0",
			wantPipeline: "logs:",
			wantEvidence: "fluentbit.input.tail.path_to_filelog.include",
		},
		{
			name:         "splunk monitor",
			vendor:       models.ConfigMigrationVendorSplunkForwarder,
			sourceFormat: "ini",
			source: `[monitor:///var/log/app/*.log]
disabled = 0
sourcetype = app:json
index = main
host = app-host

[monitor:///var/log/disabled.log]
disabled = 1

[httpout]
uri = https://splunk.example:8088
token = splunk-secret-token
`,
			wantReceiver:   "filelog/splunk_monitor_0",
			wantPipeline:   "logs:",
			wantEvidence:   "splunk.monitor.path_to_filelog.include",
			forbiddenValue: "splunk-secret-token",
		},
		{
			name:         "new relic infra",
			vendor:       models.ConfigMigrationVendorNewRelicInfra,
			sourceFormat: "yaml",
			source: `license_key: nr-secret-key
display_name: checkout-host
custom_attributes:
  deployment.environment: prod
  team: payments
log_file: /var/log/newrelic-infra/newrelic-infra.log
metrics_system_sample_rate: 15
`,
			wantReceiver:   "filelog/newrelic_log_0",
			wantPipeline:   "metrics:",
			wantEvidence:   "newrelic.log_file.path_to_filelog.include",
			forbiddenValue: "nr-secret-key",
		},
	}

	assistant := NewAssistant()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := assistant.Preview(models.ConfigMigrationPreviewRequest{
				Vendor:       tt.vendor,
				SourceFormat: tt.sourceFormat,
				Source:       tt.source,
				Context: models.ConfigMigrationContext{
					OTLPEndpoint: "${OTLP_EXPORT_ENDPOINT}",
				},
			})
			if err != nil {
				t.Fatalf("Preview returned error: %v", err)
			}
			if resp.SchemaVersion != models.ConfigMigrationPreviewSchemaVersion {
				t.Fatalf("schema_version = %q", resp.SchemaVersion)
			}
			if !strings.Contains(resp.DraftYAML, tt.wantReceiver) || !strings.Contains(resp.DraftYAML, tt.wantPipeline) {
				t.Fatalf("draft_yaml missing receiver/pipeline, draft:\n%s", resp.DraftYAML)
			}
			if resp.Validation == nil || !resp.Validation.Valid {
				t.Fatalf("validation = %+v, want valid", resp.Validation)
			}
			if !hasEvidence(resp.Evidence, tt.wantEvidence) {
				t.Fatalf("evidence missing rule %q: %+v", tt.wantEvidence, resp.Evidence)
			}
			if len(resp.Warnings) == 0 {
				t.Fatalf("expected partial conversion warning")
			}
			if tt.forbiddenValue != "" {
				assertResponseDoesNotContain(t, resp, tt.forbiddenValue)
				if len(resp.Redactions) == 0 {
					t.Fatalf("expected secret redaction")
				}
			}
		})
	}
}

func TestAssistantPreviewRejectsBadRequests(t *testing.T) {
	assistant := NewAssistant()
	for _, req := range []models.ConfigMigrationPreviewRequest{
		{Vendor: "", Source: "logs: []"},
		{Vendor: "unsupported", Source: "logs: []"},
		{Vendor: models.ConfigMigrationVendorDatadogAgent, Source: "   "},
	} {
		if _, err := assistant.Preview(req); err == nil {
			t.Fatalf("Preview(%+v) returned nil error", req)
		}
	}
}

func TestAssistantPreviewRedactsSecretLookingValuesEverywhere(t *testing.T) {
	assistant := NewAssistant()
	resp, err := assistant.Preview(models.ConfigMigrationPreviewRequest{
		Vendor: models.ConfigMigrationVendorDatadogAgent,
		Source: `api_key: super-secret-value
tags:
  - token:tag-secret-value
logs:
  - type: file
    path: /var/log/app.log
    token: nested-token-value
`,
		Labels: map[string]string{"password": "label-secret-value"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertResponseDoesNotContain(t, resp, "super-secret-value")
	assertResponseDoesNotContain(t, resp, "nested-token-value")
	assertResponseDoesNotContain(t, resp, "tag-secret-value")
	assertResponseDoesNotContain(t, resp, "label-secret-value")
	if !hasRedaction(resp.Redactions, "api_key", "${DATADOG_API_KEY}") {
		t.Fatalf("api_key redaction missing: %+v", resp.Redactions)
	}
}

func TestAssistantPreviewValidatesDatadogDogstatsdPortValues(t *testing.T) {
	assistant := NewAssistant()
	tests := []struct {
		name             string
		source           string
		wantStatsd       bool
		wantEndpoint     string
		forbiddenInDraft []string
	}{
		{
			name:         "valid numeric port",
			source:       "dogstatsd_port: 8125\n",
			wantStatsd:   true,
			wantEndpoint: "endpoint: 0.0.0.0:8125",
		},
		{
			name:         "valid maximum port",
			source:       "dogstatsd_port: 65535\n",
			wantStatsd:   true,
			wantEndpoint: "endpoint: 0.0.0.0:65535",
		},
		{
			name:             "newline cannot inject yaml keys",
			source:           "dogstatsd_port: \"8125\n    injected: true\"\n",
			forbiddenInDraft: []string{"injected: true", "statsd:", "0.0.0.0:8125"},
		},
		{
			name:             "non numeric port",
			source:           "dogstatsd_port: not-a-port\n",
			forbiddenInDraft: []string{"statsd:", "not-a-port"},
		},
		{
			name:             "zero port",
			source:           "dogstatsd_port: 0\n",
			forbiddenInDraft: []string{"statsd:", "0.0.0.0:0"},
		},
		{
			name:             "negative port",
			source:           "dogstatsd_port: -1\n",
			forbiddenInDraft: []string{"statsd:", "0.0.0.0:-1"},
		},
		{
			name:             "too large port",
			source:           "dogstatsd_port: 65536\n",
			forbiddenInDraft: []string{"statsd:", "0.0.0.0:65536"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := assistant.Preview(models.ConfigMigrationPreviewRequest{
				Vendor: models.ConfigMigrationVendorDatadogAgent,
				Source: tt.source,
			})
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantStatsd {
				if !strings.Contains(resp.DraftYAML, "statsd:") || !strings.Contains(resp.DraftYAML, tt.wantEndpoint) {
					t.Fatalf("draft missing statsd endpoint %q: %s", tt.wantEndpoint, resp.DraftYAML)
				}
				if hasUnsupportedPath(resp.UnsupportedKeys, "dogstatsd_port") {
					t.Fatalf("valid dogstatsd_port should not be unsupported: %+v", resp.UnsupportedKeys)
				}
				return
			}

			for _, forbidden := range tt.forbiddenInDraft {
				if strings.Contains(resp.DraftYAML, forbidden) {
					t.Fatalf("draft contains forbidden value %q: %s", forbidden, resp.DraftYAML)
				}
			}
			if !hasUnsupportedPath(resp.UnsupportedKeys, "dogstatsd_port") {
				t.Fatalf("unsupported dogstatsd_port missing: %+v", resp.UnsupportedKeys)
			}
			if !unsupportedReasonContains(resp.UnsupportedKeys, "dogstatsd_port", "1-65535") {
				t.Fatalf("unsupported dogstatsd_port should explain valid range: %+v", resp.UnsupportedKeys)
			}
		})
	}
}

func TestAssistantPreviewDoesNotCopySecretCustomAttributes(t *testing.T) {
	assistant := NewAssistant()
	resp, err := assistant.Preview(models.ConfigMigrationPreviewRequest{
		Vendor: models.ConfigMigrationVendorNewRelicInfra,
		Source: `license_key: nr-license-secret
display_name: checkout-host
custom_attributes:
  password: custom-secret-value
  team: payments
log_file: /var/log/app.log
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertResponseDoesNotContain(t, resp, "nr-license-secret")
	assertResponseDoesNotContain(t, resp, "custom-secret-value")
	if !strings.Contains(resp.DraftYAML, "payments") {
		t.Fatalf("safe custom attribute was not preserved: %s", resp.DraftYAML)
	}
}

func TestAssistantPreviewUsesSplunkHECRegardlessOfSectionOrder(t *testing.T) {
	assistant := NewAssistant()
	resp, err := assistant.Preview(models.ConfigMigrationPreviewRequest{
		Vendor: models.ConfigMigrationVendorSplunkForwarder,
		Source: `[httpout]
uri = https://splunk.example:8088
token = splunk-secret-token

[monitor:///var/log/app.log]
disabled = 0
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertResponseDoesNotContain(t, resp, "splunk-secret-token")
	if !strings.Contains(resp.DraftYAML, "splunk_hec:") || !strings.Contains(resp.DraftYAML, "exporters: [splunk_hec]") {
		t.Fatalf("draft did not route logs through splunk_hec exporter: %s", resp.DraftYAML)
	}
}

func TestAssistantPreviewSanitizesContextOTLPEndpoint(t *testing.T) {
	assistant := NewAssistant()
	resp, err := assistant.Preview(models.ConfigMigrationPreviewRequest{
		Vendor: models.ConfigMigrationVendorFluentBit,
		Source: `[INPUT]
    Name tail
    Path /var/log/app.log
`,
		Context: models.ConfigMigrationContext{
			OTLPEndpoint: "https://user:otlp-password@collector.example:4317/v1/traces?token=otlp-token&team=payments&client_secret=client-secret",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertResponseDoesNotContain(t, resp, "user:otlp-password")
	assertResponseDoesNotContain(t, resp, "otlp-password")
	assertResponseDoesNotContain(t, resp, "otlp-token")
	assertResponseDoesNotContain(t, resp, "client-secret")
	if !strings.Contains(resp.DraftYAML, "https://collector.example:4317/v1/traces") || !strings.Contains(resp.DraftYAML, "team=payments") {
		t.Fatalf("sanitized endpoint did not preserve safe endpoint material: %s", resp.DraftYAML)
	}
	for _, wantPath := range []string{"context.otlp_endpoint.userinfo", "context.otlp_endpoint.query.token", "context.otlp_endpoint.query.client_secret"} {
		if !hasRedaction(resp.Redactions, wantPath, "${REDACTED_SECRET}") {
			t.Fatalf("endpoint redaction missing path %q: %+v", wantPath, resp.Redactions)
		}
	}
}

func TestAssistantPreviewSanitizesSplunkHECEndpoint(t *testing.T) {
	assistant := NewAssistant()
	resp, err := assistant.Preview(models.ConfigMigrationPreviewRequest{
		Vendor: models.ConfigMigrationVendorSplunkForwarder,
		Source: `[httpout]
uri = https://admin:splunk-password@splunk.example:8088/services/collector?api_key=splunk-api-key&channel=main&license_key=splunk-license

[monitor:///var/log/app.log]
disabled = 0
`,
	})
	if err != nil {
		t.Fatal(err)
	}

	assertResponseDoesNotContain(t, resp, "admin:splunk-password")
	assertResponseDoesNotContain(t, resp, "splunk-password")
	assertResponseDoesNotContain(t, resp, "splunk-api-key")
	assertResponseDoesNotContain(t, resp, "splunk-license")
	if !strings.Contains(resp.DraftYAML, "splunk.example:8088/services/collector") || !strings.Contains(resp.DraftYAML, "channel=main") {
		t.Fatalf("sanitized Splunk endpoint did not preserve safe endpoint material: %s", resp.DraftYAML)
	}
	for _, wantPath := range []string{"[httpout].uri.userinfo", "[httpout].uri.query.api_key", "[httpout].uri.query.license_key"} {
		if !hasRedaction(resp.Redactions, wantPath, "${REDACTED_SECRET}") {
			t.Fatalf("endpoint redaction missing path %q: %+v", wantPath, resp.Redactions)
		}
	}
}

func hasEvidence(items []models.ConfigMigrationEvidence, ruleID string) bool {
	for _, item := range items {
		if item.RuleID == ruleID {
			return true
		}
	}
	return false
}

func hasRedaction(items []models.ConfigMigrationRedaction, path, placeholder string) bool {
	for _, item := range items {
		if item.Path == path && item.Placeholder == placeholder {
			return true
		}
	}
	return false
}

func unsupportedReasonContains(items []models.ConfigMigrationUnsupportedKey, path, want string) bool {
	for _, item := range items {
		if item.Path == path && strings.Contains(item.Reason, want) {
			return true
		}
	}
	return false
}

func assertResponseDoesNotContain(t *testing.T, resp models.ConfigMigrationPreviewResponse, forbidden string) {
	t.Helper()
	if strings.Contains(resp.DraftYAML, forbidden) || strings.Contains(resp.Summary, forbidden) {
		t.Fatalf("response leaked forbidden value %q: %+v", forbidden, resp)
	}
	for _, item := range resp.Warnings {
		if strings.Contains(item.Message, forbidden) || strings.Contains(item.Path, forbidden) {
			t.Fatalf("warning leaked forbidden value %q: %+v", forbidden, item)
		}
	}
	for _, item := range resp.UnsupportedKeys {
		if strings.Contains(item.Path, forbidden) || strings.Contains(item.Reason, forbidden) || strings.Contains(item.Suggestion, forbidden) {
			t.Fatalf("unsupported key leaked forbidden value %q: %+v", forbidden, item)
		}
	}
	for _, item := range resp.Evidence {
		if strings.Contains(item.SourcePath, forbidden) || strings.Contains(item.TargetPath, forbidden) || strings.Contains(item.Explanation, forbidden) {
			t.Fatalf("evidence leaked forbidden value %q: %+v", forbidden, item)
		}
	}
	for _, item := range resp.Redactions {
		if strings.Contains(item.Reason, forbidden) || strings.Contains(item.Placeholder, forbidden) || strings.Contains(item.Path, forbidden) {
			t.Fatalf("redaction leaked forbidden value %q: %+v", forbidden, item)
		}
	}
}
