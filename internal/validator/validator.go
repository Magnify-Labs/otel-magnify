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
	Valid         bool      `json:"valid"`
	OverallStatus string    `json:"overall_status,omitempty"`
	Summary       string    `json:"summary,omitempty"`
	Errors        []Error   `json:"errors,omitempty"`
	Warnings      []Message `json:"warnings,omitempty"`
	Checks        []Check   `json:"checks,omitempty"`
}

// RuntimeOptions controls optional validation through a local otelcol binary.
// Runtime validation is opt-in because it executes an operator-configured local
// binary. Empty BinaryPath defaults to "otelcol" when Enabled is true.
type RuntimeOptions struct {
	Enabled        bool
	BinaryPath     string
	Timeout        time.Duration
	TargetVersion  string
	MinimumVersion string
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
// only structural checks are performed. Returns a Result with Valid=true and
// no errors if everything checks out.
func Validate(yamlContent []byte, available *models.AvailableComponents) Result {
	var root map[string]any
	if err := yaml.Unmarshal(yamlContent, &root); err != nil {
		return Result{Errors: []Error{{
			Code:    "yaml_parse",
			Message: fmt.Sprintf("invalid YAML: %v", err),
		}}}
	}
	if root == nil {
		return Result{Errors: []Error{{
			Code:    "empty_config",
			Message: "configuration is empty",
		}}}
	}

	var errs []Error

	definedByCategory := map[string]map[string]bool{
		"receivers":  extractDefined(root, "receivers"),
		"processors": extractDefined(root, "processors"),
		"exporters":  extractDefined(root, "exporters"),
		"connectors": extractDefined(root, "connectors"),
		"extensions": extractDefined(root, "extensions"),
	}

	service, ok := root["service"].(map[string]any)
	if !ok {
		errs = append(errs, Error{
			Code: "missing_service", Message: "'service' section is required", Path: "service",
		})
		return Result{Errors: errs}
	}

	pipelines, ok := service["pipelines"].(map[string]any)
	if !ok || len(pipelines) == 0 {
		errs = append(errs, Error{
			Code: "missing_pipelines", Message: "'service.pipelines' must define at least one pipeline", Path: "service.pipelines",
		})
		return Result{Errors: errs}
	}

	// Sort pipeline names for deterministic error ordering.
	pipelineNames := make([]string, 0, len(pipelines))
	for name := range pipelines {
		pipelineNames = append(pipelineNames, name)
	}
	sort.Strings(pipelineNames)

	for _, name := range pipelineNames {
		pipelineRaw := pipelines[name]
		pipeline, ok := pipelineRaw.(map[string]any)
		if !ok {
			errs = append(errs, Error{
				Code: "invalid_pipeline", Message: fmt.Sprintf("pipeline %q is not an object", name),
				Path: "service.pipelines." + name,
			})
			continue
		}

		// A pipeline needs at least receivers and exporters.
		for _, required := range []string{"receivers", "exporters"} {
			if _, present := pipeline[required]; !present {
				errs = append(errs, Error{
					Code:    "missing_pipeline_section",
					Message: fmt.Sprintf("pipeline %q is missing '%s'", name, required),
					Path:    "service.pipelines." + name + "." + required,
				})
			}
		}

		for section, category := range pipelineSectionToCategory {
			refs := toStringSlice(pipeline[section])
			for i, id := range refs {
				path := fmt.Sprintf("service.pipelines.%s.%s[%d]", name, section, i)
				if _, defined := definedByCategory[category][id]; !defined {
					// A component referenced in a pipeline but not defined top-level is a hard error.
					errs = append(errs, Error{
						Code:    "undefined_component",
						Message: fmt.Sprintf("pipeline %q references %s %q which is not defined under top-level '%s'", name, singular(section), id, category),
						Path:    path,
					})
					continue
				}
				if available != nil {
					if msg := checkInstalled(category, id, available); msg != "" {
						errs = append(errs, Error{
							Code: "component_not_installed", Message: msg, Path: path,
						})
					}
				}
			}
		}
	}

	// Extensions declared in service.extensions must be defined too.
	if extRefs := toStringSlice(service["extensions"]); len(extRefs) > 0 {
		for i, id := range extRefs {
			path := fmt.Sprintf("service.extensions[%d]", i)
			if _, defined := definedByCategory["extensions"][id]; !defined {
				errs = append(errs, Error{
					Code:    "undefined_component",
					Message: fmt.Sprintf("service.extensions references %q which is not defined under top-level 'extensions'", id),
					Path:    path,
				})
				continue
			}
			if available != nil {
				if msg := checkInstalled("extensions", id, available); msg != "" {
					errs = append(errs, Error{Code: "component_not_installed", Message: msg, Path: path})
				}
			}
		}
	}

	return Result{Valid: len(errs) == 0, Errors: errs}
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
	result := Validate(yamlContent, available)
	result.Checks = append(result.Checks, staticSummaryCheck(result))

	if !result.Valid {
		result.Checks = append(result.Checks, Check{
			ID:       "otelcol_runtime",
			Label:    "Collector runtime validation",
			Source:   "otelcol.binary",
			Status:   "skipped",
			Required: false,
			Messages: []Message{{Code: "depends_on_failed_check", Severity: "info", Message: "Runtime validation skipped because static validation failed."}},
		})
		finalize(&result)
		return result
	}

	check := runRuntimeCheck(ctx, yamlContent, opts)
	result.Checks = append(result.Checks, check)
	for _, msg := range check.Messages {
		if msg.Severity == "warning" {
			result.Warnings = append(result.Warnings, msg)
		}
		if msg.Severity == "error" {
			result.Errors = append(result.Errors, Error{Code: msg.Code, Message: msg.Message, Path: msg.Path, CheckID: check.ID})
		}
	}
	result.Valid = len(result.Errors) == 0
	finalize(&result)
	return result
}

func staticSummaryCheck(result Result) Check {
	check := Check{ID: "yaml_static", Label: "YAML/static structure", Source: "server.static_yaml", Required: true}
	if result.Valid {
		check.Status = "passed"
		check.Messages = []Message{{Code: "static_validation_passed", Severity: "info", Message: "Static validation passed."}}
		return check
	}
	check.Status = "failed"
	for _, err := range result.Errors {
		check.Messages = append(check.Messages, Message{Code: err.Code, Severity: "error", Message: err.Message, Path: err.Path})
	}
	return check
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

// checkInstalled returns an empty string if the component type (everything
// before an optional "/instance_name" suffix) is listed in available
// components for the given category, otherwise a human-readable message.
// If the category is not reported at all, we assume the agent's view is
// incomplete and skip the check (stay conservative, not block push).
func checkInstalled(category, id string, available *models.AvailableComponents) string {
	installed, ok := available.Components[category]
	if !ok {
		return ""
	}
	componentType := id
	if idx := strings.Index(id, "/"); idx >= 0 {
		componentType = id[:idx]
	}
	if slices.Contains(installed, componentType) {
		return ""
	}
	return fmt.Sprintf("%s type %q is not installed on the target agent (available: %s)",
		singular(category), componentType, strings.Join(installed, ", "))
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
