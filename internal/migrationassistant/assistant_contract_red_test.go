package migrationassistant

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestAssistantPreviewContractFixturesExposePartialConversionEvidence(t *testing.T) {
	assistant := NewAssistant()
	tests := []struct {
		name                 string
		vendor               string
		sourceFormat         string
		fixture              string
		wantEvidenceRule     string
		wantUnsupportedPath  string
		wantRedactionPath    string
		wantRedactionToken   string
		wantDraftContains    string
		wantForbiddenSecrets []string
	}{
		{
			name:                "datadog agent logs config unsupported container collection is explicit",
			vendor:              models.ConfigMigrationVendorDatadogAgent,
			sourceFormat:        "yaml",
			fixture:             "testdata/datadog_logs.yaml",
			wantEvidenceRule:    "datadog.logs.file.path_to_filelog.include",
			wantUnsupportedPath: "logs_config.container_collect_all",
			wantRedactionPath:   "api_key",
			wantRedactionToken:  "${DATADOG_API_KEY}",
			wantDraftContains:   "filelog/datadog_logs_0",
			wantForbiddenSecrets: []string{
				"dd-red-fixture-secret",
			},
		},
		{
			name:                "fluent bit unsupported lua filter is explicit",
			vendor:              models.ConfigMigrationVendorFluentBit,
			sourceFormat:        "conf",
			fixture:             "testdata/fluentbit_tail.conf",
			wantEvidenceRule:    "fluentbit.input.tail.path_to_filelog.include",
			wantUnsupportedPath: "[FILTER] Name lua",
			wantDraftContains:   "filelog/fluentbit_tail_0",
		},
		{
			name:                "splunk props fragment unsupported timestamp extraction is explicit",
			vendor:              models.ConfigMigrationVendorSplunkForwarder,
			sourceFormat:        "ini",
			fixture:             "testdata/splunk_inputs.conf",
			wantEvidenceRule:    "splunk.monitor.path_to_filelog.include",
			wantUnsupportedPath: "[source::/var/log/app.log].TIME_FORMAT",
			wantRedactionPath:   "httpout.token",
			wantRedactionToken:  "${SPLUNK_HEC_TOKEN}",
			wantDraftContains:   "filelog/splunk_monitor_0",
			wantForbiddenSecrets: []string{
				"splunk-red-fixture-token",
			},
		},
		{
			name:                "new relic infra unsupported process toggle is explicit",
			vendor:              models.ConfigMigrationVendorNewRelicInfra,
			sourceFormat:        "yaml",
			fixture:             "testdata/newrelic_infra.yml",
			wantEvidenceRule:    "newrelic.log_file.path_to_filelog.include",
			wantUnsupportedPath: "enable_process_metrics",
			wantRedactionPath:   "license_key",
			wantRedactionToken:  "${NEW_RELIC_LICENSE_KEY}",
			wantDraftContains:   "filelog/newrelic_log_0",
			wantForbiddenSecrets: []string{
				"nr-red-fixture-license",
				"nr-red-fixture-password",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := assistant.Preview(models.ConfigMigrationPreviewRequest{
				Vendor:       tt.vendor,
				SourceFormat: tt.sourceFormat,
				Source:       readMigrationFixture(t, tt.fixture),
				Context: models.ConfigMigrationContext{
					OTLPEndpoint: "${OTLP_EXPORT_ENDPOINT}",
				},
			})
			if err != nil {
				t.Fatalf("Preview returned error: %v", err)
			}
			if resp.SchemaVersion != models.ConfigMigrationPreviewSchemaVersion {
				t.Fatalf("schema_version = %q, want %q", resp.SchemaVersion, models.ConfigMigrationPreviewSchemaVersion)
			}
			if resp.Confidence != models.ConfigMigrationConfidenceMedium {
				t.Fatalf("confidence = %q, want medium for partial-but-safe fixture response: %+v", resp.Confidence, resp)
			}
			if resp.Validation == nil || !resp.Validation.Valid {
				t.Fatalf("validation = %+v, want valid generated Collector draft", resp.Validation)
			}
			if !strings.Contains(resp.DraftYAML, tt.wantDraftContains) {
				t.Fatalf("draft_yaml missing %q:\n%s", tt.wantDraftContains, resp.DraftYAML)
			}
			if !hasEvidence(resp.Evidence, tt.wantEvidenceRule) {
				t.Fatalf("evidence missing rule %q: %+v", tt.wantEvidenceRule, resp.Evidence)
			}
			if !hasWarning(resp.Warnings, "partial_conversion") {
				t.Fatalf("partial conversion warning missing: %+v", resp.Warnings)
			}
			if !hasUnsupportedPath(resp.UnsupportedKeys, tt.wantUnsupportedPath) {
				t.Fatalf("unsupported path %q missing: %+v", tt.wantUnsupportedPath, resp.UnsupportedKeys)
			}
			if tt.wantRedactionPath != "" && !hasRedaction(resp.Redactions, tt.wantRedactionPath, tt.wantRedactionToken) {
				t.Fatalf("redaction path=%q placeholder=%q missing: %+v", tt.wantRedactionPath, tt.wantRedactionToken, resp.Redactions)
			}
			for _, secret := range tt.wantForbiddenSecrets {
				assertResponseDoesNotContain(t, resp, secret)
			}
		})
	}
}

func TestAssistantPreviewInvalidTextReturnsLowConfidenceSkeletonWithParseWarning(t *testing.T) {
	resp, err := NewAssistant().Preview(models.ConfigMigrationPreviewRequest{
		Vendor:       models.ConfigMigrationVendorFluentBit,
		SourceFormat: "conf",
		Source:       "this is not a fluent bit stanza\nand has no supported directives",
	})
	if err != nil {
		t.Fatalf("Preview returned error for invalid text fallback: %v", err)
	}
	if resp.Confidence != models.ConfigMigrationConfidenceLow {
		t.Fatalf("confidence = %q, want low for invalid text fallback", resp.Confidence)
	}
	if resp.Validation == nil || !resp.Validation.Valid || strings.TrimSpace(resp.DraftYAML) == "" {
		t.Fatalf("expected safe valid skeleton draft, validation=%+v draft=%q", resp.Validation, resp.DraftYAML)
	}
	if !hasWarning(resp.Warnings, "parse_fallback") {
		t.Fatalf("parse fallback warning missing for invalid text: %+v", resp.Warnings)
	}
	if len(resp.Evidence) != 0 {
		t.Fatalf("invalid text should not claim mapping evidence: %+v", resp.Evidence)
	}
}

func TestAssistantPreviewRejectsSourceAboveLimitBeforeParsing(t *testing.T) {
	_, err := NewAssistant().Preview(models.ConfigMigrationPreviewRequest{
		Vendor: models.ConfigMigrationVendorDatadogAgent,
		Source: strings.Repeat("x", maxSourceBytes+1),
	})
	if !errors.Is(err, ErrSourceTooLarge) {
		t.Fatalf("error = %v, want ErrSourceTooLarge", err)
	}
}

func readMigrationFixture(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func hasWarning(items []models.ConfigMigrationWarning, code string) bool {
	for _, item := range items {
		if item.Code == code {
			return true
		}
	}
	return false
}

func hasUnsupportedPath(items []models.ConfigMigrationUnsupportedKey, path string) bool {
	for _, item := range items {
		if item.Path == path {
			return true
		}
	}
	return false
}
