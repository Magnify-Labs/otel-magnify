// Package validator performs lightweight static validation of OTel Collector
// configurations before they are pushed to an agent.
//
// It parses the YAML, checks the mandatory "service.pipelines" shape, verifies
// that every pipeline component reference points to a definition in the matching
// top-level section, and (if provided) that each component's type is installed
// on the target agent according to its reported AvailableComponents.
//
// This is explicitly not a substitute for "otelcol validate": we don't resolve
// factories or check per-component option schemas. The goal is to catch the
// common mistakes — typos, missing definitions, components not built into the
// target collector — without running the collector binary.
package validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// Error is a single validation failure.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Path is a dotted YAML path to the offending node, e.g. "service.pipelines.traces.receivers[0]".
	Path string `json:"path,omitempty"`
	// CheckID identifies the enriched validation check that produced this error.
	CheckID string `json:"check_id,omitempty"`
}

// Message is a structured validation note attached to a check.
type Message struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Path     string `json:"path,omitempty"`
	CheckID  string `json:"check_id,omitempty"`
}

// Check describes one validation check in the enriched response contract.
type Check struct {
	ID       string         `json:"id"`
	Label    string         `json:"label"`
	Source   string         `json:"source"`
	Status   string         `json:"status"`
	Required bool           `json:"required"`
	Messages []Message      `json:"messages,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Result is the outcome of a validation pass.
type Result struct {
	Valid                  bool      `json:"valid"`
	OverallStatus          string    `json:"overall_status"`
	Summary                string    `json:"summary"`
	TargetCollectorVersion string    `json:"target_collector_version,omitempty"`
	ValidatedAt            time.Time `json:"validated_at"`
	Errors                 []Error   `json:"errors"`
	Warnings               []Message `json:"warnings"`
	Checks                 []Check   `json:"checks"`
}

// RuntimeOptions controls optional validation through a local otelcol binary.
// Runtime validation is opt-in because it executes an operator-configured local
// binary. Empty BinaryPath defaults to "otelcol" when Enabled is true.
type RuntimeOptions struct {
	Enabled             bool
	BinaryPath          string
	Timeout             time.Duration
	TargetVersion       string
	TargetVersionSource string
	MinimumVersion      string
}

const defaultRuntimeTimeout = 5 * time.Second

// pipelineSectionToCategory maps the plural section name used inside a pipeline
// to the corresponding top-level section (both happen to match, but we keep
// the mapping explicit so callers don't rely on the identity).
var pipelineSectionToCategory = map[string]string{
	"receivers":  "receivers",
	"processors": "processors",
	"exporters":  "exporters",
}

// Validate runs the light validation. `available` may be nil, in which case
// component availability is reported as a non-blocking warning. Returns a
// Result with Valid=true when no blocking validation error is found. The
// enriched checks/errors/warnings fields are populated for API clients while the
// legacy Valid and Errors fields remain available for existing callers.
func Validate(yamlContent []byte, available *models.AvailableComponents) Result {
	result, _ := validateStatic(yamlContent, available, RuntimeOptions{})
	finalize(&result)
	return result
}

type componentRef struct {
	Category string
	ID       string
	Path     string
}

type staticValidationState struct {
	root              map[string]any
	definedByCategory map[string]map[string]bool
	refs              []componentRef
	yamlOK            bool
	structureOK       bool
}

func validateStatic(yamlContent []byte, available *models.AvailableComponents, opts RuntimeOptions) (Result, staticValidationState) {
	result := Result{
		Valid:                  true,
		TargetCollectorVersion: strings.TrimSpace(opts.TargetVersion),
		ValidatedAt:            time.Now().UTC(),
		Errors:                 []Error{},
		Warnings:               []Message{},
		Checks:                 []Check{},
	}
	state := staticValidationState{}

	yamlCheck := Check{
		ID:       "yaml_static",
		Label:    "YAML syntax",
		Source:   "server.static_yaml",
		Required: true,
		Metadata: map[string]any{"bytes": len(yamlContent)},
	}
	var root map[string]any
	if err := yaml.Unmarshal(yamlContent, &root); err != nil {
		yamlCheck.Status = "failed"
		msg := Message{Code: "yaml_parse", Severity: "error", Message: fmt.Sprintf("invalid YAML: %v", err), CheckID: yamlCheck.ID}
		yamlCheck.Messages = []Message{msg}
		result.Checks = append(result.Checks, yamlCheck)
		result.Errors = append(result.Errors, Error{Code: msg.Code, Message: msg.Message, CheckID: yamlCheck.ID})
		appendSkippedDependents(&result, "yaml_static", "YAML parsing failed; this check was not executed.", "collector_structure", "component_availability", "collector_version_compatibility")
		return result, state
	}
	if root == nil {
		yamlCheck.Status = "failed"
		msg := Message{Code: "empty_config", Severity: "error", Message: "configuration is empty", CheckID: yamlCheck.ID}
		yamlCheck.Messages = []Message{msg}
		result.Checks = append(result.Checks, yamlCheck)
		result.Errors = append(result.Errors, Error{Code: msg.Code, Message: msg.Message, CheckID: yamlCheck.ID})
		appendSkippedDependents(&result, "yaml_static", "Configuration is empty; this check was not executed.", "collector_structure", "component_availability", "collector_version_compatibility")
		return result, state
	}
	yamlCheck.Status = "passed"
	yamlCheck.Messages = []Message{{Code: "yaml_parse_ok", Severity: "info", Message: "YAML parsed successfully.", CheckID: yamlCheck.ID}}
	result.Checks = append(result.Checks, yamlCheck)
	state.root = root
	state.yamlOK = true

	structureCheck, refs, defined := validateCollectorStructure(root)
	result.Checks = append(result.Checks, structureCheck)
	state.definedByCategory = defined
	state.refs = refs
	state.structureOK = structureCheck.Status != "failed"
	collectCheckMessages(&result, structureCheck)

	if state.structureOK {
		componentCheck := validateComponentAvailability(refs, available)
		result.Checks = append(result.Checks, componentCheck)
		collectCheckMessages(&result, componentCheck)
	} else {
		appendSkippedDependents(&result, "collector_structure", "Collector structure failed; component availability was not checked.", "component_availability")
	}

	versionCheck := validateVersionCompatibility(opts.TargetVersion, opts.TargetVersionSource, opts.MinimumVersion)
	result.Checks = append(result.Checks, versionCheck)
	collectCheckMessages(&result, versionCheck)
	return result, state
}

func appendSkippedDependents(result *Result, dependsOn, message string, ids ...string) {
	for _, id := range ids {
		check := baseCheck(id)
		check.Status = "skipped"
		check.Metadata = map[string]any{"depends_on_failed_check": dependsOn}
		check.Messages = []Message{{Code: "depends_on_failed_check", Severity: "info", Message: message, CheckID: id}}
		result.Checks = append(result.Checks, check)
	}
}

func baseCheck(id string) Check {
	switch id {
	case "yaml_static":
		return Check{ID: id, Label: "YAML syntax", Source: "server.static_yaml", Required: true, Messages: []Message{}, Metadata: map[string]any{}}
	case "collector_structure":
		return Check{ID: id, Label: "Collector structure", Source: "server.structure", Required: true, Messages: []Message{}, Metadata: map[string]any{}}
	case "component_availability":
		return Check{ID: id, Label: "Components available on workload", Source: "workload.available_components", Required: true, Messages: []Message{}, Metadata: map[string]any{}}
	case "collector_version_compatibility":
		return Check{ID: id, Label: "Collector version compatibility", Source: "server.target_version_policy", Required: false, Messages: []Message{}, Metadata: map[string]any{}}
	case "otelcol_runtime":
		return Check{ID: id, Label: "Collector runtime validation", Source: "otelcol.binary", Required: false, Messages: []Message{}, Metadata: map[string]any{}}
	default:
		return Check{ID: id, Required: false, Messages: []Message{}, Metadata: map[string]any{}}
	}
}

func collectCheckMessages(result *Result, check Check) {
	for _, msg := range check.Messages {
		msg.CheckID = check.ID
		switch msg.Severity {
		case "error":
			result.Errors = append(result.Errors, Error{Code: msg.Code, Message: msg.Message, Path: msg.Path, CheckID: check.ID})
		case "warning":
			result.Warnings = append(result.Warnings, msg)
		}
	}
}

func validateCollectorStructure(root map[string]any) (Check, []componentRef, map[string]map[string]bool) {
	check := baseCheck("collector_structure")
	definedByCategory := map[string]map[string]bool{
		"receivers":  extractDefined(root, "receivers"),
		"processors": extractDefined(root, "processors"),
		"exporters":  extractDefined(root, "exporters"),
		"connectors": extractDefined(root, "connectors"),
		"extensions": extractDefined(root, "extensions"),
	}
	check.Metadata["defined_components"] = definedComponentCounts(definedByCategory)

	var refs []componentRef
	service, ok := root["service"].(map[string]any)
	if !ok {
		check.Messages = append(check.Messages, Message{Code: "missing_service", Severity: "error", Message: "'service' section is required", Path: "service"})
		check.Status = "failed"
		return check, refs, definedByCategory
	}
	pipelines, ok := service["pipelines"].(map[string]any)
	if !ok || len(pipelines) == 0 {
		check.Messages = append(check.Messages, Message{Code: "missing_pipelines", Severity: "error", Message: "'service.pipelines' must define at least one pipeline", Path: "service.pipelines"})
		check.Status = "failed"
		return check, refs, definedByCategory
	}

	pipelineNames := make([]string, 0, len(pipelines))
	for name := range pipelines {
		pipelineNames = append(pipelineNames, name)
	}
	sort.Strings(pipelineNames)
	check.Metadata["pipelines"] = pipelineNames

	for _, name := range pipelineNames {
		pipelineRaw := pipelines[name]
		pipeline, ok := pipelineRaw.(map[string]any)
		if !ok {
			check.Messages = append(check.Messages, Message{Code: "invalid_pipeline", Severity: "error", Message: fmt.Sprintf("pipeline %q is not an object", name), Path: "service.pipelines." + name})
			continue
		}
		for _, required := range []string{"receivers", "exporters"} {
			if _, present := pipeline[required]; !present {
				check.Messages = append(check.Messages, Message{Code: "missing_pipeline_section", Severity: "error", Message: fmt.Sprintf("pipeline %q is missing '%s'", name, required), Path: "service.pipelines." + name + "." + required})
			}
		}
		for section, category := range pipelineSectionToCategory {
			refsInSection := toStringSlice(pipeline[section])
			for i, id := range refsInSection {
				path := fmt.Sprintf("service.pipelines.%s.%s[%d]", name, section, i)
				if _, defined := definedByCategory[category][id]; !defined {
					check.Messages = append(check.Messages, Message{Code: "undefined_component", Severity: "error", Message: fmt.Sprintf("pipeline %q references %s %q which is not defined under top-level '%s'", name, singular(section), id, category), Path: path})
					continue
				}
				refs = append(refs, componentRef{Category: category, ID: id, Path: path})
			}
		}
	}

	if extRefs := toStringSlice(service["extensions"]); len(extRefs) > 0 {
		for i, id := range extRefs {
			path := fmt.Sprintf("service.extensions[%d]", i)
			if _, defined := definedByCategory["extensions"][id]; !defined {
				check.Messages = append(check.Messages, Message{Code: "undefined_component", Severity: "error", Message: fmt.Sprintf("service.extensions references %q which is not defined under top-level 'extensions'", id), Path: path})
				continue
			}
			refs = append(refs, componentRef{Category: "extensions", ID: id, Path: path})
		}
	}

	check.Metadata["component_refs_checked"] = len(refs)
	check.Status = statusWithWarnings(check.Messages)
	if check.Status == "passed" {
		check.Messages = []Message{{Code: "collector_structure_ok", Severity: "info", Message: "Collector service pipelines and component references are structurally valid."}}
	}
	return check, refs, definedByCategory
}

func validateComponentAvailability(refs []componentRef, available *models.AvailableComponents) Check {
	check := baseCheck("component_availability")
	check.Metadata["component_refs_checked"] = len(refs)
	if available == nil || len(available.Components) == 0 {
		check.Status = "warning"
		check.Metadata["reported_categories"] = []string{}
		check.Messages = []Message{{Code: "available_components_missing", Severity: "warning", Message: "Target workload has not reported AvailableComponents; installed component checks were not enforced."}}
		return check
	}
	reportedCategories := make([]string, 0, len(available.Components))
	for category := range available.Components {
		reportedCategories = append(reportedCategories, category)
	}
	sort.Strings(reportedCategories)
	check.Metadata["reported_categories"] = reportedCategories

	warnedCategory := map[string]bool{}
	for _, ref := range refs {
		installed, ok := available.Components[ref.Category]
		if !ok {
			if !warnedCategory[ref.Category] {
				check.Messages = append(check.Messages, Message{Code: "component_category_unreported", Severity: "warning", Message: fmt.Sprintf("AvailableComponents did not report category %q; components in that category were not enforced.", ref.Category)})
				warnedCategory[ref.Category] = true
			}
			continue
		}
		componentType := componentType(ref.ID)
		if !slices.Contains(installed, componentType) {
			check.Messages = append(check.Messages, Message{Code: "component_not_installed", Severity: "error", Message: fmt.Sprintf("%s type %q is not installed on the target workload (available: %s)", singular(ref.Category), componentType, strings.Join(installed, ", ")), Path: ref.Path})
		}
	}
	check.Status = statusWithWarnings(check.Messages)
	if check.Status == "passed" {
		check.Messages = []Message{{Code: "component_availability_ok", Severity: "info", Message: "All referenced component types are reported as available on the target workload."}}
	}
	return check
}

func validateVersionCompatibility(targetVersion, versionSource, minimumVersion string) Check {
	check := baseCheck("collector_version_compatibility")
	targetVersion = strings.TrimSpace(targetVersion)
	versionSource = strings.TrimSpace(versionSource)
	minimumVersion = strings.TrimSpace(minimumVersion)
	if versionSource == "" {
		versionSource = "unknown"
	}
	check.Metadata["version_source"] = versionSource
	if minimumVersion != "" {
		check.Metadata["minimum_supported_version"] = minimumVersion
	}
	if targetVersion == "" {
		check.Status = "warning"
		check.Messages = []Message{{Code: "target_version_unknown", Severity: "warning", Message: "No target collector version was provided or reported by the workload."}}
		return check
	}
	check.Metadata["target_version"] = targetVersion
	if versionSource == "unknown" {
		check.Metadata["version_source"] = "default"
	}
	if minimumVersion != "" && compareVersions(targetVersion, minimumVersion) < 0 {
		check.Status = "failed"
		check.Messages = []Message{{Code: "target_version_too_old", Severity: "error", Message: fmt.Sprintf("Target collector version %s is below the configured minimum %s.", targetVersion, minimumVersion)}}
		return check
	}
	check.Status = "passed"
	check.Messages = []Message{{Code: "target_version_checked", Severity: "info", Message: fmt.Sprintf("Target collector version %s is compatible with the configured validation policy.", targetVersion)}}
	return check
}

func definedComponentCounts(defined map[string]map[string]bool) map[string]int {
	out := make(map[string]int, len(defined))
	for category, items := range defined {
		out[category] = len(items)
	}
	return out
}

func componentType(id string) string {
	if idx := strings.Index(id, "/"); idx >= 0 {
		return id[:idx]
	}
	return id
}

// RuntimeOptionsFromEnv loads the local otelcol runtime validation settings.
// Operators must explicitly enable execution with
// OTELCOL_RUNTIME_VALIDATION_ENABLED=true. Optional knobs:
//   - OTELCOL_BINARY_PATH: absolute path or executable name (default: otelcol)
//   - OTELCOL_RUNTIME_VALIDATION_TIMEOUT_SECONDS: positive integer seconds (default: 5)
//   - OTELCOL_MIN_RUNTIME_VERSION: warning threshold for the local binary version
func RuntimeOptionsFromEnv() RuntimeOptions {
	return RuntimeOptions{
		Enabled:        truthy(os.Getenv("OTELCOL_RUNTIME_VALIDATION_ENABLED")),
		BinaryPath:     os.Getenv("OTELCOL_BINARY_PATH"),
		Timeout:        secondsDuration(os.Getenv("OTELCOL_RUNTIME_VALIDATION_TIMEOUT_SECONDS"), defaultRuntimeTimeout),
		MinimumVersion: os.Getenv("OTELCOL_MIN_RUNTIME_VERSION"),
	}
}

// ValidateWithRuntime runs the static validator and appends an optional
// otelcol_runtime check. Runtime validation never runs when the static YAML or
// structure validation already failed, because otelcol would only duplicate the
// known failure and could obscure the deterministic server-side errors.
func ValidateWithRuntime(ctx context.Context, yamlContent []byte, available *models.AvailableComponents, opts RuntimeOptions) Result {
	result, _ := validateStatic(yamlContent, available, opts)

	if len(result.Errors) > 0 {
		check := baseCheck("otelcol_runtime")
		check.Status = "skipped"
		check.Metadata = map[string]any{"depends_on_failed_check": firstFailedRequiredCheck(result.Checks)}
		check.Messages = []Message{{Code: "depends_on_failed_check", Severity: "info", Message: "Runtime validation skipped because a required validation check failed.", CheckID: check.ID}}
		result.Checks = append(result.Checks, check)
		finalize(&result)
		return result
	}

	check := runRuntimeCheck(ctx, yamlContent, opts)
	result.Checks = append(result.Checks, check)
	collectCheckMessages(&result, check)
	finalize(&result)
	return result
}

func firstFailedRequiredCheck(checks []Check) string {
	for _, check := range checks {
		if check.Required && check.Status == "failed" {
			return check.ID
		}
	}
	return "static_validation"
}

func runRuntimeCheck(ctx context.Context, yamlContent []byte, opts RuntimeOptions) Check {
	check := Check{
		ID:       "otelcol_runtime",
		Label:    "Collector runtime validation",
		Source:   "otelcol.binary",
		Required: false,
		Metadata: map[string]any{"command_mode": "otelcol validate --config"},
	}
	if opts.TargetVersion != "" {
		check.Metadata["target_version"] = opts.TargetVersion
	}
	if !opts.Enabled {
		check.Status = "skipped"
		check.Messages = []Message{{Code: "otelcol_runtime_disabled", Severity: "info", Message: "Runtime validation is disabled by local server configuration."}}
		return check
	}

	binaryPath := opts.BinaryPath
	if binaryPath == "" {
		binaryPath = "otelcol"
	}
	resolvedPath, err := exec.LookPath(binaryPath)
	if err != nil {
		check.Status = "skipped"
		check.Messages = []Message{{Code: "otelcol_not_found", Severity: "warning", Message: fmt.Sprintf("otelcol binary %q was not found on the server.", binaryPath)}}
		return check
	}
	check.Metadata["binary_path"] = resolvedPath

	versionOutput, versionErr := runCommand(ctx, opts.Timeout, resolvedPath, "--version")
	versionInfo := parseOtelcolVersion(versionOutput.stdout + "\n" + versionOutput.stderr)
	if versionInfo.Version != "" {
		check.Metadata["binary_version"] = versionInfo.Version
	}
	if versionInfo.Distribution != "" {
		check.Metadata["binary_distribution"] = versionInfo.Distribution
	}
	if versionErr != nil || versionInfo.Version == "" {
		check.Messages = append(check.Messages, Message{Code: "otelcol_version_unknown", Severity: "warning", Message: "Unable to determine local otelcol version; runtime validation proof is limited."})
	}

	if opts.MinimumVersion != "" && versionInfo.Version != "" && compareVersions(versionInfo.Version, opts.MinimumVersion) < 0 {
		check.Messages = append(check.Messages, Message{Code: "otelcol_too_old", Severity: "warning", Message: fmt.Sprintf("Runtime validation used otelcol %s, below configured minimum %s.", versionInfo.Version, opts.MinimumVersion)})
	}
	if opts.TargetVersion != "" && versionInfo.Version != "" && compareVersions(versionInfo.Version, opts.TargetVersion) != 0 {
		check.Messages = append(check.Messages, Message{Code: "otelcol_version_mismatch", Severity: "warning", Message: fmt.Sprintf("Runtime validation used otelcol %s while target version is %s.", versionInfo.Version, opts.TargetVersion)})
	}

	tmp, err := os.CreateTemp("", "otel-magnify-otelcol-*.yaml")
	if err != nil {
		check.Status = statusWithWarnings(check.Messages)
		check.Messages = append(check.Messages, Message{Code: "otelcol_runtime_unavailable", Severity: "warning", Message: fmt.Sprintf("Unable to create temporary config for runtime validation: %v", err)})
		return check
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	defer cleanup()
	if _, err := tmp.Write(yamlContent); err != nil {
		check.Status = statusWithWarnings(check.Messages)
		check.Messages = append(check.Messages, Message{Code: "otelcol_runtime_unavailable", Severity: "warning", Message: fmt.Sprintf("Unable to write temporary config for runtime validation: %v", err)})
		return check
	}
	if err := tmp.Close(); err != nil {
		check.Status = statusWithWarnings(check.Messages)
		check.Messages = append(check.Messages, Message{Code: "otelcol_runtime_unavailable", Severity: "warning", Message: fmt.Sprintf("Unable to close temporary config for runtime validation: %v", err)})
		return check
	}

	started := time.Now()
	out, err := runCommand(ctx, opts.Timeout, resolvedPath, "validate", "--config", tmpPath)
	check.Metadata["duration_ms"] = time.Since(started).Milliseconds()
	check.Metadata["exit_code"] = out.exitCode
	if strings.TrimSpace(out.stdout) != "" {
		check.Metadata["stdout"] = strings.TrimSpace(out.stdout)
	}
	if strings.TrimSpace(out.stderr) != "" {
		check.Metadata["stderr"] = strings.TrimSpace(out.stderr)
	}

	if err == nil {
		check.Messages = append(check.Messages, Message{Code: "otelcol_validation_passed", Severity: "info", Message: "otelcol runtime validation passed."})
		check.Status = statusWithWarnings(check.Messages)
		return check
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		check.Status = "warning"
		check.Messages = append(check.Messages, Message{Code: "otelcol_runtime_timeout", Severity: "warning", Message: fmt.Sprintf("otelcol runtime validation did not finish within %s.", runtimeTimeout(opts.Timeout))})
		return check
	}
	check.Status = "failed"
	check.Messages = append(check.Messages, Message{Code: "otelcol_validation_failed", Severity: "error", Message: compactRuntimeFailure(out, err)})
	return check
}

type commandOutput struct {
	stdout   string
	stderr   string
	exitCode int
}

func runCommand(parent context.Context, timeout time.Duration, name string, args ...string) (commandOutput, error) {
	ctx, cancel := context.WithTimeout(parent, runtimeTimeout(timeout))
	defer cancel()

	// #nosec G204: name is resolved through resolveOtelcolPath, which only accepts
	// configured paths or binaries found on PATH after executable checks.
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := commandOutput{stdout: stdout.String(), stderr: stderr.String(), exitCode: 0}
	if cmd.ProcessState != nil {
		out.exitCode = cmd.ProcessState.ExitCode()
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return out, ctxErr
	}
	return out, err
}

func runtimeTimeout(timeout time.Duration) time.Duration {
	if timeout > 0 {
		return timeout
	}
	return defaultRuntimeTimeout
}

type versionInfo struct {
	Distribution string
	Version      string
}

var versionPattern = regexp.MustCompile(`(?i)^\s*([a-z0-9_-]+).*?version\s+v?([0-9]+(?:\.[0-9]+){1,2})`)

func parseOtelcolVersion(output string) versionInfo {
	match := versionPattern.FindStringSubmatch(output)
	if len(match) != 3 {
		return versionInfo{}
	}
	return versionInfo{Distribution: match[1], Version: match[2]}
}

func statusWithWarnings(messages []Message) string {
	for _, msg := range messages {
		if msg.Severity == "error" {
			return "failed"
		}
		if msg.Severity == "warning" {
			return "warning"
		}
	}
	return "passed"
}

func compactRuntimeFailure(out commandOutput, err error) string {
	parts := []string{fmt.Sprintf("otelcol validate failed: %v", err)}
	if strings.TrimSpace(out.stderr) != "" {
		parts = append(parts, "stderr: "+strings.TrimSpace(out.stderr))
	}
	if strings.TrimSpace(out.stdout) != "" {
		parts = append(parts, "stdout: "+strings.TrimSpace(out.stdout))
	}
	return strings.Join(parts, "; ")
}

func finalize(result *Result) {
	switch {
	case len(result.Errors) > 0:
		result.Valid = false
		result.OverallStatus = "failed"
	case len(result.Warnings) > 0 || anySkipped(result.Checks):
		result.Valid = true
		result.OverallStatus = "warning"
	default:
		result.Valid = true
		result.OverallStatus = "passed"
	}
	result.Summary = validationSummary(result)
}

func anySkipped(checks []Check) bool {
	for _, check := range checks {
		if check.Status == "skipped" {
			return true
		}
	}
	return false
}

func validationSummary(result *Result) string {
	if result.OverallStatus == "failed" {
		return fmt.Sprintf("Configuration failed %d blocking validation error(s).", len(result.Errors))
	}
	if result.OverallStatus == "warning" {
		return fmt.Sprintf("Configuration passed blocking validation with %d warning(s) or skipped check(s).", len(result.Warnings))
	}
	return "Configuration passed all validation checks."
}

func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "y", "on", "enabled":
		return true
	default:
		return false
	}
}

func secondsDuration(s string, fallback time.Duration) time.Duration {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return fallback
	}
	return time.Duration(n) * time.Second
}

func compareVersions(a, b string) int {
	as := versionParts(a)
	bs := versionParts(b)
	for i := 0; i < 3; i++ {
		if as[i] < bs[i] {
			return -1
		}
		if as[i] > bs[i] {
			return 1
		}
	}
	return 0
}

func versionParts(v string) [3]int {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	for i := 0; i < len(parts) && i < len(out); i++ {
		n, _ := strconv.Atoi(parts[i])
		out[i] = n
	}
	return out
}

// extractDefined returns the set of component IDs defined under a top-level
// section (e.g. {"otlp": true, "otlp/secondary": true} for "receivers").
// Missing or malformed sections yield an empty set.
func extractDefined(root map[string]any, section string) map[string]bool {
	out := make(map[string]bool)
	m, ok := root[section].(map[string]any)
	if !ok {
		return out
	}
	for id := range m {
		out[id] = true
	}
	return out
}

// toStringSlice coerces an arbitrary YAML value into a list of strings,
// silently dropping non-string entries. YAML lists decode as []any with
// element type string for identifiers like "otlp" or "batch/custom".
func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, el := range arr {
		if s, ok := el.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func singular(section string) string {
	return strings.TrimSuffix(section, "s")
}
