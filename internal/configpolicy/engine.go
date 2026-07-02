// Package configpolicy evaluates OpenTelemetry Collector configs against deterministic safety rules.
package configpolicy

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

const (
	// DecisionPass means the candidate config satisfies policy.
	DecisionPass = models.PolicyDecisionPass
	// DecisionWarn means the candidate config is allowed with warnings.
	DecisionWarn = models.PolicyDecisionWarn
	// DecisionBlock means the candidate config must not be pushed.
	DecisionBlock = models.PolicyDecisionBlock
)

// PolicyTarget is the scope used when evaluating config safety rules.
type PolicyTarget = models.ConfigPolicyTarget

// PolicySettings carries server-side policy knobs for paid-edition hooks.
type PolicySettings = models.ConfigPolicySettings

// SamplingPolicySettings bounds sampling percentages when configured.
type SamplingPolicySettings = models.SamplingPolicySettings

// EvaluationResult is the deterministic policy evaluation API contract.
type EvaluationResult = models.ConfigPolicyEvaluation

// Finding is one policy rule outcome.
type Finding = models.ConfigPolicyFinding

// EvaluationRequest is the input consumed by the default policy engine.
type EvaluationRequest struct {
	CurrentYAML   string
	CandidateYAML string
	Target        PolicyTarget
	Settings      PolicySettings
}

// Engine evaluates config safety policy rules.
type Engine struct{}

// NewDefaultEngine returns the immutable Community policy engine.
func NewDefaultEngine() Engine { return Engine{} }

// Evaluate applies default Community rules plus supplied server-side settings.
func (Engine) Evaluate(req EvaluationRequest) EvaluationResult {
	settings := normalizeSettings(req.Settings)
	result := EvaluationResult{
		SchemaVersion: "config-policy.v1",
		Decision:      DecisionPass,
		Severity:      models.PolicySeverityInfo,
		Valid:         true,
		Allowed:       true,
		Target:        req.Target,
		Settings:      settings,
		Findings:      []Finding{},
		Audit: models.ConfigPolicyAuditMeta{
			Persisted: false,
			Event:     "config.policy.evaluate",
			Reason:    "policy evaluations are returned deterministically; persistent audit storage is only used when the caller emits an audit event",
		},
	}
	candidate, err := parseConfig(req.CandidateYAML)
	if err != nil {
		result.Findings = append(result.Findings, finding("community.yaml.parse", "YAML parse", models.PolicySeverityCritical, DecisionBlock, req.Target, "", fmt.Sprintf("configuration YAML could not be parsed: %v", err), "Fix YAML syntax before evaluating policy."))
		finalize(&result)
		return result
	}

	production := isProduction(req.Target.Environment)
	if production {
		result.Findings = append(result.Findings, insecureTLSFindings(candidate, req.Target)...)
		if !hasTopLevelComponent(candidate, "processors", "memory_limiter") {
			result.Findings = append(result.Findings, finding("community.processors.memory_limiter.required", "Memory limiter required", models.PolicySeverityCritical, DecisionBlock, req.Target, "processors.memory_limiter", "production collector configs must define a memory_limiter processor", "Add a memory_limiter processor and include it in every service pipeline."))
		}
		result.Findings = append(result.Findings, missingBatchFindings(candidate, req.Target)...)
		result.Findings = append(result.Findings, otlpEndpointFindings(candidate, req.Target, settings.AllowedOTLPEndpoints)...)
		result.Findings = append(result.Findings, resourceIdentityFindings(candidate, req.Target, settings.RequiredResourceAttributes)...)
	}
	if production || req.Settings.Sampling.MinPercentage != 0 || req.Settings.Sampling.MaxPercentage != 0 {
		result.Findings = append(result.Findings, samplingFindings(candidate, req.Target, settings.Sampling.MinPercentage, settings.Sampling.MaxPercentage)...)
	}
	if strings.TrimSpace(req.CurrentYAML) != "" && (production || len(req.Settings.CriticalExporters) > 0) {
		if current, err := parseConfig(req.CurrentYAML); err == nil {
			result.Findings = append(result.Findings, criticalExporterRemovalFindings(current, candidate, req.Target, settings.CriticalExporters)...)
		}
	}
	sortFindings(result.Findings)
	finalize(&result)
	return result
}

func normalizeSettings(settings PolicySettings) PolicySettings {
	if settings.Sampling.MinPercentage == 0 {
		settings.Sampling.MinPercentage = 0.1
	}
	if settings.Sampling.MaxPercentage == 0 {
		settings.Sampling.MaxPercentage = 100
	}
	return settings
}

func finalize(result *EvaluationResult) {
	result.Summary = models.ConfigPolicySummary{}
	result.Decision = DecisionPass
	result.Severity = models.PolicySeverityInfo
	result.Valid = true
	result.Allowed = true
	for _, f := range result.Findings {
		switch f.Decision {
		case DecisionBlock:
			result.Summary.BlockCount++
			result.Decision = DecisionBlock
			result.Valid = false
			result.Allowed = false
			result.Severity = maxSeverity(result.Severity, f.Severity)
		case DecisionWarn:
			result.Summary.WarnCount++
			if result.Decision != DecisionBlock {
				result.Decision = DecisionWarn
				result.Severity = maxSeverity(result.Severity, f.Severity)
			}
		default:
			result.Summary.PassCount++
		}
	}
}

func maxSeverity(current, next string) string {
	rank := map[string]int{models.PolicySeverityInfo: 0, models.PolicySeverityWarning: 1, models.PolicySeverityCritical: 2}
	if rank[next] > rank[current] {
		return next
	}
	return current
}

func finding(code, name, severity, decision string, target PolicyTarget, path, message, remediation string) Finding {
	return Finding{PolicyID: code, PolicyName: name, RuleID: code, RuleCode: code, Severity: severity, Decision: decision, TargetScope: target.Scope, Environment: target.Environment, Path: path, Paths: []string{path}, Message: message, Remediation: remediation, Packaging: "community", Tier: "core"}
}

type configDoc map[string]any

func parseConfig(raw string) (configDoc, error) {
	var root map[string]any
	if err := yaml.Unmarshal([]byte(raw), &root); err != nil {
		return nil, err
	}
	if root == nil {
		root = map[string]any{}
	}
	return root, nil
}

func isProduction(env string) bool {
	env = strings.ToLower(strings.TrimSpace(env))
	return env == "prod" || env == "production"
}

func section(root configDoc, name string) map[string]any {
	if v, ok := root[name].(map[string]any); ok {
		return v
	}
	return map[string]any{}
}

func hasTopLevelComponent(root configDoc, category, componentType string) bool {
	for id := range section(root, category) {
		if componentBase(id) == componentType {
			return true
		}
	}
	return false
}

func pipelines(root configDoc) map[string]any {
	service := section(root, "service")
	if p, ok := service["pipelines"].(map[string]any); ok {
		return p
	}
	return map[string]any{}
}

func missingBatchFindings(root configDoc, target PolicyTarget) []Finding {
	var out []Finding
	for _, name := range sortedKeys(pipelines(root)) {
		pipeline, _ := pipelines(root)[name].(map[string]any)
		if !containsComponentType(toStringSlice(pipeline["processors"]), "batch") {
			out = append(out, finding("community.pipelines.batch.required", "Batch processor required", models.PolicySeverityCritical, DecisionBlock, target, "service.pipelines."+name+".processors", "service pipeline must include a batch processor", "Add batch to the processor chain for this pipeline."))
		}
	}
	return out
}

func insecureTLSFindings(root configDoc, target PolicyTarget) []Finding {
	var out []Finding
	for category, comps := range map[string]map[string]any{"exporters": section(root, "exporters"), "receivers": section(root, "receivers")} {
		for _, id := range sortedKeys(comps) {
			if hasInsecureTrue(comps[id]) {
				out = append(out, finding("community.production.insecure_tls", "Forbid insecure TLS in production", models.PolicySeverityCritical, DecisionBlock, target, category+"."+id+".tls.insecure", "insecure: true is not allowed for production targets", "Disable insecure TLS or target a non-production environment."))
			}
		}
	}
	return out
}

func hasInsecureTrue(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			if strings.EqualFold(k, "insecure") {
				if b, ok := child.(bool); ok && b {
					return true
				}
			}
			if hasInsecureTrue(child) {
				return true
			}
		}
	case []any:
		for _, child := range x {
			if hasInsecureTrue(child) {
				return true
			}
		}
	}
	return false
}

func otlpEndpointFindings(root configDoc, target PolicyTarget, allowlist []string) []Finding {
	if len(allowlist) == 0 {
		return nil
	}
	var out []Finding
	for _, id := range sortedKeys(section(root, "exporters")) {
		if componentBase(id) != "otlp" && componentBase(id) != "otlphttp" {
			continue
		}
		comp, _ := section(root, "exporters")[id].(map[string]any)
		endpoint, _ := comp["endpoint"].(string)
		if strings.TrimSpace(endpoint) == "" || endpointAllowed(endpoint, allowlist) {
			continue
		}
		out = append(out, finding("community.exporters.otlp_endpoint.allowlist", "OTLP endpoint allowlist", models.PolicySeverityCritical, DecisionBlock, target, "exporters."+id+".endpoint", "OTLP exporter endpoint is outside the configured allowlist", "Add the endpoint host to server-side policy settings or use an approved collector gateway."))
	}
	return out
}

func endpointAllowed(endpoint string, allowlist []string) bool {
	if len(allowlist) == 0 {
		return false
	}
	host := endpointHost(endpoint)
	if host == "" {
		return false
	}
	for _, allowed := range allowlist {
		allowed = endpointHost(allowed)
		if allowed == "" {
			continue
		}
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

func endpointHost(endpoint string) string {
	raw := strings.TrimSpace(endpoint)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "scheme://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func resourceIdentityFindings(root configDoc, target PolicyTarget, requiredAttrs []string) []Finding {
	if len(requiredAttrs) == 0 {
		return nil
	}
	required := make(map[string]bool, len(requiredAttrs))
	for _, attr := range requiredAttrs {
		if attr = strings.TrimSpace(attr); attr != "" {
			required[attr] = false
		}
	}
	if len(required) == 0 {
		return nil
	}
	for _, id := range sortedKeys(section(root, "processors")) {
		if componentBase(id) != "resource" {
			continue
		}
		comp, _ := section(root, "processors")[id].(map[string]any)
		for _, item := range toAnySlice(comp["attributes"]) {
			attr, _ := item.(map[string]any)
			key, _ := attr["key"].(string)
			if _, ok := required[key]; ok {
				required[key] = true
			}
		}
	}
	for _, ok := range required {
		if !ok {
			return []Finding{finding("community.resource.attributes.required", "Resource attributes required", models.PolicySeverityCritical, DecisionBlock, target, "processors.resource.attributes", "resource processor is missing required identity attributes", "Add a resource processor that upserts the required attributes.")}
		}
	}
	return nil
}

func samplingFindings(root configDoc, target PolicyTarget, minPercentage, maxPercentage float64) []Finding {
	var out []Finding
	for _, id := range sortedKeys(section(root, "processors")) {
		comp, _ := section(root, "processors")[id].(map[string]any)
		base := componentBase(id)
		if base == "probabilistic_sampler" {
			if pct, ok := number(comp["sampling_percentage"]); ok && (pct < minPercentage || pct > maxPercentage) {
				out = append(out, finding("community.sampling.percentage.safe_range", "Sampling percentage safe range", models.PolicySeverityWarning, DecisionBlock, target, "processors."+id+".sampling_percentage", fmt.Sprintf("sampling percentage %.4g is outside the safe range %.4g-%.4g", pct, minPercentage, maxPercentage), "Set sampling_percentage within the server-side safe range."))
			}
		}
		if base == "tail_sampling" {
			for i, p := range toAnySlice(comp["policies"]) {
				pol, _ := p.(map[string]any)
				if pct, ok := number(pol["sampling_percentage"]); ok && (pct < minPercentage || pct > maxPercentage) {
					out = append(out, finding("community.sampling.percentage.safe_range", "Sampling percentage safe range", models.PolicySeverityWarning, DecisionBlock, target, fmt.Sprintf("processors.%s.policies[%d].sampling_percentage", id, i), fmt.Sprintf("sampling percentage %.4g is outside the safe range %.4g-%.4g", pct, minPercentage, maxPercentage), "Set sampling_percentage within the server-side safe range."))
				}
			}
		}
	}
	return out
}

func criticalExporterRemovalFindings(current, candidate configDoc, target PolicyTarget, critical []string) []Finding {
	criticalSet := map[string]bool{}
	for _, id := range critical {
		criticalSet[strings.TrimSpace(id)] = true
	}
	if len(criticalSet) == 0 {
		for id := range pipelineExporters(current) {
			criticalSet[id] = true
		}
	}
	candidateExporters := pipelineExporters(candidate)
	for id := range criticalSet {
		if !candidateExporters[id] {
			return []Finding{finding("community.exporters.critical_removal", "Critical exporter removal", models.PolicySeverityCritical, DecisionBlock, target, "service.pipelines.traces.exporters", "candidate config removes an exporter considered critical from the current config", "Keep critical exporters in service pipelines or explicitly change server-side policy settings.")}
		}
	}
	return nil
}

func pipelineExporters(root configDoc) map[string]bool {
	out := map[string]bool{}
	for _, name := range sortedKeys(pipelines(root)) {
		pipeline, _ := pipelines(root)[name].(map[string]any)
		_ = name
		for _, exporter := range toStringSlice(pipeline["exporters"]) {
			out[exporter] = true
		}
	}
	return out
}

func toStringSlice(v any) []string {
	var out []string
	for _, item := range toAnySlice(v) {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func toAnySlice(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case []string:
		out := make([]any, len(x))
		for i := range x {
			out[i] = x[i]
		}
		return out
	default:
		return nil
	}
}

func containsComponentType(ids []string, componentType string) bool {
	for _, id := range ids {
		if componentBase(id) == componentType {
			return true
		}
	}
	return false
}

func componentBase(id string) string {
	return strings.Split(strings.TrimSpace(id), "/")[0]
}

func number(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].RuleCode == findings[j].RuleCode {
			return findings[i].Path < findings[j].Path
		}
		return findings[i].RuleCode < findings[j].RuleCode
	})
}
