package validator

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

const maxValidatorFuzzInputBytes = 64 << 10

func TestValidateIsDeterministic(t *testing.T) {
	yamlContent := []byte(`
service:
  pipelines:
    traces:
      receivers: [missing_receiver]
      processors: [missing_processor]
      exporters: [missing_exporter]
`)

	want := deterministicValidationJSON(t, Validate(yamlContent, nil))
	for range 128 {
		got := deterministicValidationJSON(t, Validate(yamlContent, nil))
		if !bytes.Equal(got, want) {
			t.Fatalf("Validate is not deterministic:\nfirst:  %s\nsecond: %s", want, got)
		}
	}
}

func FuzzValidate(f *testing.F) {
	seeds := []struct {
		content       []byte
		withAvailable bool
	}{
		{content: []byte(validMinimal), withAvailable: false},
		{content: []byte(validMinimal), withAvailable: true},
		{content: []byte{}, withAvailable: false},
		{content: []byte("receivers: [oops\n"), withAvailable: false},
		{
			content: []byte(`
service:
  pipelines:
    traces:
      receivers: [
`),
			withAvailable: true,
		},
		{
			content: []byte(`
service:
  pipelines:
    traces:
      receivers: [missing_receiver]
      processors: [missing_processor]
      exporters: [missing_exporter]
`),
			withAvailable: false,
		},
		{
			content: []byte(`
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: localhost:4317
processors:
  batch:
    metadata:
      nested:
        values: [one, two, three]
exporters:
  otlp:
    endpoint: https://collector.example:4317
extensions:
  health_check: {}
service:
  extensions: [health_check]
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlp]
`),
			withAvailable: true,
		},
	}

	for _, seed := range seeds {
		f.Add(seed.content, seed.withAvailable)
	}

	f.Fuzz(func(t *testing.T, yamlContent []byte, withAvailable bool) {
		if len(yamlContent) > maxValidatorFuzzInputBytes {
			return
		}

		first := Validate(yamlContent, fuzzAvailableComponents(withAvailable))
		assertValidationResultConsistency(t, first)
		firstJSON := deterministicValidationJSON(t, first)

		second := Validate(yamlContent, fuzzAvailableComponents(withAvailable))
		assertValidationResultConsistency(t, second)
		secondJSON := deterministicValidationJSON(t, second)

		if !bytes.Equal(firstJSON, secondJSON) {
			t.Fatalf("Validate is not deterministic:\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
		}
	})
}

func fuzzAvailableComponents(enabled bool) *models.AvailableComponents {
	if !enabled {
		return nil
	}

	return &models.AvailableComponents{
		Components: map[string][]string{
			"receivers":  {"jaeger", "otlp"},
			"processors": {"attributes", "batch", "memory_limiter"},
			"exporters":  {"debug", "logging", "otlp"},
			"connectors": {"forward"},
			"extensions": {"health_check"},
		},
	}
}

func assertValidationResultConsistency(t *testing.T, result Result) {
	t.Helper()

	if result.Errors == nil || result.Warnings == nil || result.Checks == nil {
		t.Fatalf("result collections must be non-nil: %#v", result)
	}
	if result.ValidatedAt.IsZero() {
		t.Fatal("validated_at must be populated")
	}
	if result.Summary == "" {
		t.Fatal("summary must be populated")
	}

	expectedStatus := "passed"
	if len(result.Errors) > 0 {
		expectedStatus = "failed"
	} else if len(result.Warnings) > 0 || fuzzHasSkippedCheck(result.Checks) {
		expectedStatus = "warning"
	}

	if result.OverallStatus != expectedStatus {
		t.Fatalf("overall_status = %q, want %q: %#v", result.OverallStatus, expectedStatus, result)
	}
	if result.Valid != (len(result.Errors) == 0) {
		t.Fatalf("valid = %t with %d blocking errors", result.Valid, len(result.Errors))
	}

	for _, check := range result.Checks {
		switch check.Status {
		case "passed", "warning", "failed", "skipped":
		default:
			t.Fatalf("unknown check status %q in %#v", check.Status, check)
		}

		for _, message := range check.Messages {
			switch message.Severity {
			case "info", "warning", "error":
			default:
				t.Fatalf("unknown message severity %q in %#v", message.Severity, message)
			}
		}
	}
}

func fuzzHasSkippedCheck(checks []Check) bool {
	for _, check := range checks {
		if check.Status == "skipped" {
			return true
		}
	}
	return false
}

func deterministicValidationJSON(t *testing.T, result Result) []byte {
	t.Helper()

	result.ValidatedAt = time.Time{}
	body, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal validation result: %v", err)
	}
	return body
}
