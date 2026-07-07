package validator

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

const validMinimal = `
receivers:
  otlp:
    protocols:
      grpc: {}
processors:
  batch: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [logging]
`

func TestValidate_Valid(t *testing.T) {
	t.Parallel()

	r := Validate([]byte(validMinimal), nil)
	if !r.Valid {
		t.Fatalf("expected valid, got errors: %+v", r.Errors)
	}
}

func TestValidate_InvalidYAML(t *testing.T) {
	t.Parallel()

	r := Validate([]byte("receivers: [oops\n"), nil)
	if r.Valid || len(r.Errors) == 0 || r.Errors[0].Code != "yaml_parse" {
		t.Fatalf("expected yaml_parse error, got %+v", r)
	}
}

func TestValidate_MissingService(t *testing.T) {
	t.Parallel()

	r := Validate([]byte("receivers: {}\n"), nil)
	if r.Valid {
		t.Fatal("expected invalid")
	}
	if r.Errors[0].Code != "missing_service" {
		t.Errorf("first error = %+v, want missing_service", r.Errors[0])
	}
}

func TestValidate_UndefinedComponent(t *testing.T) {
	t.Parallel()

	yaml := `
receivers:
  otlp: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [logging]
`
	r := Validate([]byte(yaml), nil)
	if r.Valid {
		t.Fatal("expected invalid")
	}
	found := false
	for _, e := range r.Errors {
		if e.Code == "undefined_component" && strings.Contains(e.Message, "batch") {
			found = true
		}
	}
	if !found {
		t.Errorf("undefined_component for 'batch' not reported, got %+v", r.Errors)
	}
}

func TestValidate_MissingPipelineSection(t *testing.T) {
	t.Parallel()

	yaml := `
receivers:
  otlp: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
`
	r := Validate([]byte(yaml), nil)
	if r.Valid {
		t.Fatal("expected invalid")
	}
	found := false
	for _, e := range r.Errors {
		if e.Code == "missing_pipeline_section" && strings.Contains(e.Message, "exporters") {
			found = true
		}
	}
	if !found {
		t.Errorf("missing exporters not reported, got %+v", r.Errors)
	}
}

func TestValidate_ComponentNotInstalled(t *testing.T) {
	t.Parallel()

	available := &models.AvailableComponents{
		Components: map[string][]string{
			"receivers":  {"otlp"},
			"processors": {"batch"},
			"exporters":  {"logging"},
		},
	}
	yaml := `
receivers:
  jaeger: {}
processors:
  batch: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [jaeger]
      processors: [batch]
      exporters: [logging]
`
	r := Validate([]byte(yaml), available)
	if r.Valid {
		t.Fatal("expected invalid")
	}
	found := false
	for _, e := range r.Errors {
		if e.Code == "component_not_installed" && strings.Contains(e.Message, "jaeger") {
			found = true
		}
	}
	if !found {
		t.Errorf("jaeger not-installed not reported, got %+v", r.Errors)
	}
}

func TestValidateWithRuntime_ReturnsEnrichedChecksAndStableTopLevelFields(t *testing.T) {
	available := &models.AvailableComponents{
		Components: map[string][]string{
			"receivers":  {"otlp"},
			"processors": {"batch"},
			"exporters":  {"logging"},
		},
	}

	result := ValidateWithRuntime(t.Context(), []byte(validMinimal), available, RuntimeOptions{
		TargetVersion:  "0.150.1",
		MinimumVersion: "0.100.0",
	})

	if !result.Valid || result.OverallStatus != "warning" {
		t.Fatalf("runtime-disabled result should be warning-valid, got %+v", result)
	}
	var payload struct {
		Valid                  bool      `json:"valid"`
		OverallStatus          string    `json:"overall_status"`
		Summary                string    `json:"summary"`
		TargetCollectorVersion string    `json:"target_collector_version"`
		ValidatedAt            string    `json:"validated_at"`
		Errors                 []Error   `json:"errors"`
		Warnings               []Message `json:"warnings"`
		Checks                 []Check   `json:"checks"`
	}
	body, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Valid || payload.OverallStatus != "warning" || payload.Summary == "" || payload.TargetCollectorVersion != "0.150.1" || payload.ValidatedAt == "" {
		t.Fatalf("top-level response fields missing or wrong: %s", body)
	}
	for _, id := range []string{"yaml_static", "collector_structure", "component_availability", "collector_version_compatibility", "otelcol_runtime"} {
		check := findCheck(t, result, id)
		if check.Label == "" || check.Source == "" || check.Status == "" || check.Messages == nil || check.Metadata == nil {
			t.Fatalf("check %s is not fully populated: %+v", id, check)
		}
	}
	if len(payload.Errors) != 0 {
		t.Fatalf("expected no blocking errors, got %+v", payload.Errors)
	}
	if !hasMessage(findCheck(t, result, "otelcol_runtime"), "otelcol_runtime_disabled") {
		t.Fatalf("runtime disabled warning missing: %+v", findCheck(t, result, "otelcol_runtime"))
	}
}

func TestValidateWithRuntime_InvalidYAMLSkipsDependentChecksWithCheckIDs(t *testing.T) {
	result := ValidateWithRuntime(t.Context(), []byte("receivers: [oops\n"), nil, RuntimeOptions{})

	if result.Valid || result.OverallStatus != "failed" || len(result.Errors) == 0 {
		t.Fatalf("expected failed result with errors, got %+v", result)
	}
	if result.Errors[0].Code != "yaml_parse" || result.Errors[0].CheckID != "yaml_static" {
		t.Fatalf("yaml error should carry check_id yaml_static, got %+v", result.Errors[0])
	}
	if findCheck(t, result, "yaml_static").Status != "failed" {
		t.Fatalf("yaml_static should fail: %+v", findCheck(t, result, "yaml_static"))
	}
	for _, id := range []string{"collector_structure", "component_availability", "collector_version_compatibility", "otelcol_runtime"} {
		check := findCheck(t, result, id)
		if check.Status != "skipped" || !hasMessage(check, "depends_on_failed_check") || check.Metadata["depends_on_failed_check"] != "yaml_static" {
			t.Fatalf("dependent check %s should be skipped with dependency metadata: %+v", id, check)
		}
	}
}

func TestValidateWithRuntime_AvailableComponentsMissingIsWarningOnly(t *testing.T) {
	result := ValidateWithRuntime(t.Context(), []byte(validMinimal), nil, RuntimeOptions{})

	check := findCheck(t, result, "component_availability")
	if !result.Valid || check.Status != "warning" || !hasMessage(check, "available_components_missing") {
		t.Fatalf("missing AvailableComponents should be warning-only: result=%+v check=%+v", result, check)
	}
}

func TestValidate_NamedInstanceStripsSuffix(t *testing.T) {
	t.Parallel()

	available := &models.AvailableComponents{
		Components: map[string][]string{
			"receivers": {"otlp"},
			"exporters": {"logging"},
		},
	}
	yaml := `
receivers:
  otlp/secondary: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp/secondary]
      exporters: [logging]
`
	r := Validate([]byte(yaml), available)
	if !r.Valid {
		t.Fatalf("named instance should resolve to base type, got errors: %+v", r.Errors)
	}
}

func TestValidate_SkipsCategoryNotReported(t *testing.T) {
	t.Parallel()

	// If AvailableComponents only reports receivers, we must not flag an
	// unknown processor as not-installed — we just don't know.
	available := &models.AvailableComponents{
		Components: map[string][]string{
			"receivers": {"otlp"},
			"exporters": {"logging"},
		},
	}
	r := Validate([]byte(validMinimal), available)
	if !r.Valid {
		t.Fatalf("expected valid when processor category not reported, got %+v", r.Errors)
	}
}

func BenchmarkValidateMinimalConfig(b *testing.B) {
	yaml := []byte(validMinimal)
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		r := Validate(yaml, nil)
		if !r.Valid {
			b.Fatalf("expected valid, got errors: %+v", r.Errors)
		}
	}
}
