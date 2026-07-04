// Package migrationassistant converts small vendor-agent config snippets into
// draft OpenTelemetry Collector YAML without external calls or persistence.
package migrationassistant

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/magnify-labs/otel-magnify/internal/validator"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

const maxSourceBytes = 1 << 20

// ErrSourceTooLarge is returned before parsing when the pasted source exceeds
// the offline preview size limit.
var ErrSourceTooLarge = errors.New("source exceeds 1 MiB limit")

// Assistant performs deterministic offline migration previews for supported
// vendor configuration snippets.
type Assistant struct{}

type draft struct {
	receivers       map[string][]string
	receiverOptions map[string][]string
	processors      map[string]struct{}
	resourceAttrs   map[string]string
	exporters       map[string]string
	pipelines       map[string]pipeline
}

type pipeline struct {
	receivers  []string
	processors []string
	exporters  []string
}

type conversionState struct {
	request      models.ConfigMigrationPreviewRequest
	sourceFormat string
	draft        draft
	warnings     []models.ConfigMigrationWarning
	unsupported  []models.ConfigMigrationUnsupportedKey
	evidence     []models.ConfigMigrationEvidence
	redactions   []models.ConfigMigrationRedaction
	mapped       int
	vendorName   string
	stack        string
}

// NewAssistant returns a stateless migration preview assistant.
func NewAssistant() *Assistant { return &Assistant{} }

// Preview converts the request source into a partial, safe Collector draft with
// warnings and evidence for every supported mapping.
func (a *Assistant) Preview(req models.ConfigMigrationPreviewRequest) (models.ConfigMigrationPreviewResponse, error) {
	req.Vendor = strings.TrimSpace(req.Vendor)
	req.SourceFormat = normalizeSourceFormat(req.SourceFormat)
	if req.Vendor == "" {
		return models.ConfigMigrationPreviewResponse{}, fmt.Errorf("vendor is required")
	}
	if !isSupportedVendor(req.Vendor) {
		return models.ConfigMigrationPreviewResponse{}, fmt.Errorf("unsupported vendor")
	}
	if len(req.Source) > maxSourceBytes {
		return models.ConfigMigrationPreviewResponse{}, ErrSourceTooLarge
	}
	if strings.TrimSpace(req.Source) == "" {
		return models.ConfigMigrationPreviewResponse{}, fmt.Errorf("source is required")
	}

	state := newConversionState(req)
	addCommonWarnings(&state)
	scanSecrets(req.Vendor, req.Source, &state)
	addLabels(req.Labels, &state)

	var err error
	switch req.Vendor {
	case models.ConfigMigrationVendorDatadogAgent:
		err = convertDatadog(&state)
	case models.ConfigMigrationVendorFluentBit:
		err = convertFluentBit(&state)
	case models.ConfigMigrationVendorSplunkForwarder:
		err = convertSplunk(&state)
	case models.ConfigMigrationVendorNewRelicInfra:
		err = convertNewRelic(&state)
	}
	if err != nil {
		state.warnings = append(state.warnings, warning("parse_fallback", "warning", "Source could not be parsed completely; generated a low-confidence skeleton for manual completion.", "source"))
	}

	ensureDefaults(&state)
	draftYAML := renderDraft(state.draft)
	validation := validator.Validate([]byte(draftYAML), nil)
	confidence := models.ConfigMigrationConfidenceLow
	if state.mapped > 0 && err == nil {
		confidence = models.ConfigMigrationConfidenceHigh
		if len(state.unsupported) > 0 || len(state.warnings) > 1 {
			confidence = models.ConfigMigrationConfidenceMedium
		}
	}
	return models.ConfigMigrationPreviewResponse{
		SchemaVersion:   models.ConfigMigrationPreviewSchemaVersion,
		Vendor:          req.Vendor,
		SourceFormat:    state.sourceFormat,
		DraftYAML:       draftYAML,
		DraftName:       "Migrated " + state.vendorName + " draft",
		Confidence:      confidence,
		Summary:         summary(state),
		Warnings:        nonNilWarnings(state.warnings),
		UnsupportedKeys: nonNilUnsupported(state.unsupported),
		Evidence:        nonNilEvidence(state.evidence),
		Redactions:      nonNilRedactions(state.redactions),
		Validation:      &models.ConfigMigrationValidation{Valid: validation.Valid, OverallStatus: validation.OverallStatus, Summary: validation.Summary, ValidatedAt: validation.ValidatedAt},
		SaveHint:        models.ConfigMigrationSaveHint{Kind: models.ConfigKindDraft, SourceType: "migration_assistant", Tags: []string{"migration", req.Vendor}, Category: "migration", Stack: state.stack},
	}, nil
}

func newConversionState(req models.ConfigMigrationPreviewRequest) conversionState {
	vendorName := map[string]string{
		models.ConfigMigrationVendorDatadogAgent:    "Datadog Agent",
		models.ConfigMigrationVendorFluentBit:       "Fluent Bit",
		models.ConfigMigrationVendorSplunkForwarder: "Splunk forwarder",
		models.ConfigMigrationVendorNewRelicInfra:   "New Relic infra",
	}[req.Vendor]
	stack := map[string]string{
		models.ConfigMigrationVendorDatadogAgent:    "datadog",
		models.ConfigMigrationVendorFluentBit:       "fluent-bit",
		models.ConfigMigrationVendorSplunkForwarder: "splunk",
		models.ConfigMigrationVendorNewRelicInfra:   "new-relic",
	}[req.Vendor]
	return conversionState{request: req, sourceFormat: req.SourceFormat, vendorName: vendorName, stack: stack, draft: draft{
		receivers: map[string][]string{}, receiverOptions: map[string][]string{}, processors: map[string]struct{}{}, resourceAttrs: map[string]string{}, exporters: map[string]string{}, pipelines: map[string]pipeline{},
	}}
}

func isSupportedVendor(v string) bool {
	switch v {
	case models.ConfigMigrationVendorDatadogAgent, models.ConfigMigrationVendorFluentBit, models.ConfigMigrationVendorSplunkForwarder, models.ConfigMigrationVendorNewRelicInfra:
		return true
	default:
		return false
	}
}

func normalizeSourceFormat(format string) string {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		return "auto"
	}
	return format
}

func addCommonWarnings(state *conversionState) {
	state.warnings = append(state.warnings, warning("partial_conversion", "warning", "This deterministic assistant produces a draft only; vendor-specific semantics may not be preserved.", "source"))
}

func addLabels(labels map[string]string, state *conversionState) {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		if safeAttributeKey(k) && !secretKeyPattern.MatchString(k) && strings.TrimSpace(labels[k]) != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		state.draft.resourceAttrs[k] = strings.TrimSpace(labels[k])
	}
}

func ensureDefaults(state *conversionState) {
	state.draft.processors["batch"] = struct{}{}
	if len(state.draft.exporters) == 0 {
		state.draft.exporters["otlp"] = exporterEndpoint(state)
	}
	if len(state.draft.pipelines) == 0 {
		state.draft.receivers["otlp"] = nil
		state.draft.pipelines["traces"] = pipeline{receivers: []string{"otlp"}, processors: processorList(state), exporters: []string{defaultExporterName(state)}}
	}
	for name, p := range state.draft.pipelines {
		p.processors = processorList(state)
		if len(p.exporters) == 0 {
			p.exporters = []string{defaultExporterName(state)}
		}
		state.draft.pipelines[name] = p
	}
}

func processorList(state *conversionState) []string {
	state.draft.processors["batch"] = struct{}{}
	if len(state.draft.resourceAttrs) > 0 {
		state.draft.processors["resource"] = struct{}{}
		return []string{"resource", "batch"}
	}
	return []string{"batch"}
}

func defaultExporterName(state *conversionState) string {
	if _, ok := state.draft.exporters["splunk_hec"]; ok {
		return "splunk_hec"
	}
	return "otlp"
}

func exporterEndpoint(state *conversionState) string {
	if strings.TrimSpace(state.request.Context.OTLPEndpoint) != "" {
		return sanitizeEndpoint(state.request.Context.OTLPEndpoint, "context.otlp_endpoint", state)
	}
	return "${OTLP_EXPORT_ENDPOINT}"
}

func addLogReceiver(state *conversionState, name string, paths []string, sourcePath, targetPath, ruleID, explanation string) {
	state.draft.receivers[name] = paths
	state.draft.pipelines["logs"] = pipeline{receivers: append(state.draft.pipelines["logs"].receivers, name), exporters: []string{defaultExporterName(state)}}
	state.evidence = append(state.evidence, models.ConfigMigrationEvidence{SourcePath: sourcePath, TargetPath: targetPath, RuleID: ruleID, Explanation: explanation})
	state.mapped++
}

func addResourceAttr(state *conversionState, key, value, sourcePath, ruleID string) {
	if !safeAttributeKey(key) || secretKeyPattern.MatchString(key) || strings.TrimSpace(value) == "" {
		return
	}
	state.draft.resourceAttrs[key] = strings.TrimSpace(value)
	state.evidence = append(state.evidence, models.ConfigMigrationEvidence{SourcePath: sourcePath, TargetPath: "processors.resource.attributes." + key, RuleID: ruleID, Explanation: "Source metadata maps to a Collector resource attribute."})
}

func warning(code, severity, message, path string) models.ConfigMigrationWarning {
	return models.ConfigMigrationWarning{Code: code, Severity: severity, Message: message, Path: path}
}

func unsupported(path, reason, suggestion string) models.ConfigMigrationUnsupportedKey {
	return models.ConfigMigrationUnsupportedKey{Path: path, Reason: reason, Suggestion: suggestion}
}

func summary(state conversionState) string {
	return fmt.Sprintf("Converted %d supported mapping(s) from %s into a draft Collector config. Review %d warning(s), %d unsupported item(s), and %d redaction(s) before use.", state.mapped, state.vendorName, len(state.warnings), len(state.unsupported), len(state.redactions))
}

func safeAttributeKey(key string) bool {
	if key = strings.TrimSpace(key); key == "" {
		return false
	}
	return regexp.MustCompile(`^[A-Za-z0-9_.\-/]+$`).MatchString(key)
}

func nonNilWarnings(in []models.ConfigMigrationWarning) []models.ConfigMigrationWarning {
	if in == nil {
		return []models.ConfigMigrationWarning{}
	}
	return in
}
func nonNilUnsupported(in []models.ConfigMigrationUnsupportedKey) []models.ConfigMigrationUnsupportedKey {
	if in == nil {
		return []models.ConfigMigrationUnsupportedKey{}
	}
	return in
}
func nonNilEvidence(in []models.ConfigMigrationEvidence) []models.ConfigMigrationEvidence {
	if in == nil {
		return []models.ConfigMigrationEvidence{}
	}
	return in
}
func nonNilRedactions(in []models.ConfigMigrationRedaction) []models.ConfigMigrationRedaction {
	if in == nil {
		return []models.ConfigMigrationRedaction{}
	}
	return in
}
