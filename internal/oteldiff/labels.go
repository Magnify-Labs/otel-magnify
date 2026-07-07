package oteldiff

import "strings"

func componentRef(cat, id string) ComponentRef {
	typ, name := componentTypeName(id)
	return ComponentRef{Category: cat, ID: id, Type: typ, Name: name, Path: cat + "." + id}
}
func componentTypeName(id string) (string, string) {
	parts := strings.SplitN(id, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return id, ""
}
func singular(cat string) string { return strings.TrimSuffix(cat, "s") }
func humanComponentName(id string) string {
	if humanSafeLabel(id) == MaskedValue {
		return MaskedValue
	}
	typ, name := componentTypeName(id)
	label := typ
	if name != "" {
		label = name
	}
	switch strings.ToLower(label) {
	case "loki":
		return "Loki"
	case "otlp":
		return "OTLP"
	case "otlphttp":
		return "OTLP HTTP"
	case "prometheus":
		return "Prometheus"
	default:
		return humanSafeLabel(label)
	}
}
func humanFieldName(path string) string {
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return humanSafeLabel(path)
	}
	field := parts[len(parts)-1]
	if humanSafeLabel(field) == MaskedValue {
		return "sensitive setting"
	}
	return strings.ReplaceAll(field, "_", " ")
}
func humanSafeLabel(label string) string {
	if label == "" {
		return ""
	}
	if isSensitivePath(label) || looksSecret(label) {
		return MaskedValue
	}
	return sanitizeEndpointIfURL(label)
}
func humanSafePath(path string) string {
	parts := strings.Split(path, ".")
	for i, part := range parts {
		parts[i] = humanSafeLabel(part)
	}
	return strings.Join(parts, ".")
}
func signalOf(key string) string {
	sig := strings.SplitN(key, "/", 2)[0]
	switch sig {
	case "traces", "metrics", "logs", "profiles":
		return sig
	default:
		return "unknown"
	}
}
func removedComponentRisk(cat, id string) Risk {
	typ, _ := componentTypeName(id)
	if cat == "exporters" || (cat == "processors" && (typ == "batch" || typ == "memory_limiter" || isSamplingID(id))) {
		return RiskHigh
	}
	return RiskMedium
}
func removedComponentRules(cat, id string, _ []string) []string {
	typ, _ := componentTypeName(id)
	var rules []string
	if cat == "exporters" {
		rules = append(rules, "exporter_removed")
	}
	if cat == "processors" && typ == "batch" {
		rules = append(rules, "batch_processor_removed_from_pipeline")
	}
	if cat == "processors" && typ == "memory_limiter" {
		rules = append(rules, "memory_limiter_removed_from_pipeline")
	}
	if cat == "processors" && isSamplingID(id) {
		rules = append(rules, "sampling_guard_removed")
	}
	return rules
}
func componentModifiedRisk(_ string, id string, fields []FieldChange) Risk {
	r := RiskLow
	for _, f := range fields {
		l := strings.ToLower(f.Path)
		if isAuthPath(l) || strings.Contains(l, "headers") || isInsecurePath(l) && boolBecameTrue(f.Before, f.After) {
			r = maxRisk(r, RiskHigh)
		}
		if isSamplingID(id) && strings.Contains(l, "sampling_percentage") {
			if strongSamplingChange(f.Before, f.After) {
				r = maxRisk(r, RiskHigh)
			} else {
				r = maxRisk(r, RiskMedium)
			}
		}
	}
	return r
}
func componentModifiedRules(_ string, id string, fields []FieldChange) []string {
	var rules []string
	for _, f := range fields {
		l := strings.ToLower(f.Path)
		if isAuthPath(l) || strings.Contains(l, "headers") {
			rules = appendRule(rules, "auth_header_modified")
		}
		if isInsecurePath(l) && boolBecameTrue(f.Before, f.After) {
			rules = appendRule(rules, "transport_security_weakened")
		}
		if isSamplingID(id) && strings.Contains(l, "sampling_percentage") {
			if strongSamplingChange(f.Before, f.After) {
				rules = appendRule(rules, "sampling_rate_strongly_changed")
			} else {
				rules = appendRule(rules, "sampling_policy_modified")
			}
		}
	}
	return rules
}
