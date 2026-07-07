package oteldiff

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func asMap(v any) (map[string]any, bool) { m, ok := v.(map[string]any); return m, ok }
func normalizeValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := map[string]any{}
		for k, vv := range x {
			m[k] = normalizeValue(vv)
		}
		return m
	case []any:
		a := make([]any, len(x))
		for i, vv := range x {
			a[i] = normalizeValue(vv)
		}
		return a
	default:
		return x
	}
}
func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return []string{}
	}
	out := []string{}
	for _, el := range arr {
		if s, ok := el.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
func sortedCompKeys(m map[string]component) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
func unionComponentIDs(a, b map[string]component) []string {
	set := map[string]bool{}
	for k := range a {
		set[k] = true
	}
	for k := range b {
		set[k] = true
	}
	return sortedBoolKeys(set)
}
func unionPipelineKeys(a, b map[string]PipelineShape) []string {
	set := map[string]bool{}
	for k := range a {
		set[k] = true
	}
	for k := range b {
		set[k] = true
	}
	return sortedBoolKeys(set)
}
func unionEndpointKeys(a, b map[string]struct {
	component ComponentRef
	value     EndpointValue
}) []string {
	set := map[string]bool{}
	for k := range a {
		set[k] = true
	}
	for k := range b {
		set[k] = true
	}
	return sortedBoolKeys(set)
}
func sortedBoolKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
func indexMap(s []string) map[string]int {
	m := map[string]int{}
	for i, v := range s {
		m[v] = i
	}
	return m
}
func unionStringKeys(a, b map[string]int) []string {
	set := map[string]bool{}
	for k := range a {
		set[k] = true
	}
	for k := range b {
		set[k] = true
	}
	return sortedBoolKeys(set)
}
func unionStringKeysFromMaps(a, b map[string]any) []string {
	set := map[string]bool{}
	for k := range a {
		set[k] = true
	}
	for k := range b {
		set[k] = true
	}
	return sortedBoolKeys(set)
}
func appendRule(rules []string, rule string) []string {
	for _, r := range rules {
		if r == rule {
			return rules
		}
	}
	return append(rules, rule)
}
func dedupeSorted(in []string) []string {
	set := map[string]bool{}
	for _, v := range in {
		if v != "" {
			set[v] = true
		}
	}
	return sortedBoolKeys(set)
}
func canonical(v any) string { b, _ := json.Marshal(v); return string(b) }
func maxRisk(a, b Risk) Risk {
	if riskRank(b) > riskRank(a) {
		return b
	}
	return a
}
func riskRank(r Risk) int {
	switch r {
	case RiskHigh:
		return 3
	case RiskMedium:
		return 2
	case RiskLow:
		return 1
	case RiskNone:
		return 0
	}
	return 0
}
func riskCategory(rule string) string {
	if strings.Contains(rule, "auth") || strings.Contains(rule, "security") || strings.Contains(rule, "tls") || strings.Contains(rule, "insecure") {
		return "security"
	}
	if strings.Contains(rule, "pipeline") || strings.Contains(rule, "exporter") {
		return "data_loss"
	}
	if strings.Contains(rule, "sampling") {
		return "cost"
	}
	return "routing"
}
func titleForRule(rule string) string { return strings.ReplaceAll(rule, "_", " ") }
func countComponents(m map[string]map[string]component) int {
	n := 0
	for _, mm := range m {
		n += len(mm)
	}
	return n
}
func impactedPipelines(base, target map[string]PipelineShape, cat, id string) []string {
	set := map[string]bool{}
	for name, p := range base {
		if pipelineUses(p, cat, id) {
			set[name] = true
		}
	}
	for name, p := range target {
		if pipelineUses(p, cat, id) {
			set[name] = true
		}
	}
	return sortedBoolKeys(set)
}
func pipelineUses(p PipelineShape, cat, id string) bool {
	var list []string
	switch cat {
	case "receivers":
		list = p.Receivers
	case "processors":
		list = p.Processors
	case "exporters":
		list = p.Exporters
	default:
		return false
	}
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}
func walk(v any, path string, fn func(string, any)) {
	fn(path, v)
	if m, ok := asMap(v); ok {
		for _, k := range sortedKeys(m) {
			walk(m[k], path+"."+k, fn)
		}
	}
	if arr, ok := v.([]any); ok {
		for i, x := range arr {
			walk(x, fmt.Sprintf("%s[%d]", path, i), fn)
		}
	}
}
func lastPathPart(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[i+1:]
	}
	return path
}
func scalarString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case int:
		return strconv.Itoa(x), true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	default:
		return "", false
	}
}
