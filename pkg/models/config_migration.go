package models

import "time"

const (
	// ConfigMigrationPreviewRequestSchemaVersion identifies preview requests.
	ConfigMigrationPreviewRequestSchemaVersion = "config_migration_preview_request.v1"
	// ConfigMigrationPreviewSchemaVersion identifies preview responses.
	ConfigMigrationPreviewSchemaVersion = "config_migration_preview.v1"

	// ConfigMigrationVendorDatadogAgent identifies Datadog Agent snippets.
	ConfigMigrationVendorDatadogAgent = "datadog_agent"
	// ConfigMigrationVendorFluentBit identifies Fluent Bit snippets.
	ConfigMigrationVendorFluentBit = "fluent_bit"
	// ConfigMigrationVendorSplunkForwarder identifies Splunk forwarder snippets.
	ConfigMigrationVendorSplunkForwarder = "splunk_forwarder"
	// ConfigMigrationVendorNewRelicInfra identifies New Relic infra snippets.
	ConfigMigrationVendorNewRelicInfra = "new_relic_infra"

	// ConfigMigrationConfidenceLow indicates a skeleton or minimally mapped draft.
	ConfigMigrationConfidenceLow = "low"
	// ConfigMigrationConfidenceMedium indicates a supported-but-partial draft.
	ConfigMigrationConfidenceMedium = "medium"
	// ConfigMigrationConfidenceHigh indicates mapped input with no extra unsupported directives.
	ConfigMigrationConfidenceHigh = "high"
)

// ConfigMigrationPreviewRequest is the stateless API request for converting a
// pasted vendor config snippet into a Collector draft.
type ConfigMigrationPreviewRequest struct {
	SchemaVersion string                 `json:"schema_version,omitempty"`
	Vendor        string                 `json:"vendor"`
	Source        string                 `json:"source"`
	SourceFormat  string                 `json:"source_format,omitempty"`
	Labels        map[string]string      `json:"labels,omitempty"`
	Context       ConfigMigrationContext `json:"context,omitempty"`
}

// ConfigMigrationContext carries optional target hints for draft rendering.
type ConfigMigrationContext struct {
	TargetSignal          string `json:"target_signal,omitempty"`
	TargetExporter        string `json:"target_exporter,omitempty"`
	OTLPEndpoint          string `json:"otlp_endpoint,omitempty"`
	CollectorDistribution string `json:"collector_distribution,omitempty"`
	Notes                 string `json:"notes,omitempty"`
}

// ConfigMigrationPreviewResponse is the safe draft response returned by the
// migration assistant preview endpoint.
type ConfigMigrationPreviewResponse struct {
	SchemaVersion   string                          `json:"schema_version"`
	Vendor          string                          `json:"vendor"`
	SourceFormat    string                          `json:"source_format"`
	DraftYAML       string                          `json:"draft_yaml"`
	DraftName       string                          `json:"draft_name"`
	Confidence      string                          `json:"confidence"`
	Summary         string                          `json:"summary"`
	Warnings        []ConfigMigrationWarning        `json:"warnings"`
	UnsupportedKeys []ConfigMigrationUnsupportedKey `json:"unsupported_keys"`
	Evidence        []ConfigMigrationEvidence       `json:"evidence"`
	Redactions      []ConfigMigrationRedaction      `json:"redactions"`
	Validation      *ConfigMigrationValidation      `json:"validation"`
	SaveHint        ConfigMigrationSaveHint         `json:"save_hint"`
}

// ConfigMigrationWarning describes a non-blocking limitation of the generated draft.
type ConfigMigrationWarning struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Path     string `json:"path,omitempty"`
}

// ConfigMigrationUnsupportedKey reports a source directive that needs manual migration.
type ConfigMigrationUnsupportedKey struct {
	Path       string `json:"path"`
	Reason     string `json:"reason"`
	Suggestion string `json:"suggestion,omitempty"`
}

// ConfigMigrationEvidence links a source directive to a generated Collector field.
type ConfigMigrationEvidence struct {
	SourcePath  string `json:"source_path"`
	TargetPath  string `json:"target_path"`
	RuleID      string `json:"rule_id"`
	Explanation string `json:"explanation"`
}

// ConfigMigrationRedaction records a secret-looking value that was intentionally omitted.
type ConfigMigrationRedaction struct {
	Path        string `json:"path"`
	Placeholder string `json:"placeholder"`
	Reason      string `json:"reason"`
}

// ConfigMigrationValidation summarizes Collector YAML validation for the draft.
type ConfigMigrationValidation struct {
	Valid         bool      `json:"valid"`
	OverallStatus string    `json:"overall_status"`
	Summary       string    `json:"summary"`
	ValidatedAt   time.Time `json:"validated_at"`
}

// ConfigMigrationSaveHint suggests metadata for saving the reviewed draft.
type ConfigMigrationSaveHint struct {
	Kind       string   `json:"kind"`
	SourceType string   `json:"source_type"`
	Tags       []string `json:"tags"`
	Category   string   `json:"category"`
	Stack      string   `json:"stack"`
}
