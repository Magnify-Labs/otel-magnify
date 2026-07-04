package migrationassistant

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return fmt.Sprintf("%v", t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func asBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(strings.TrimSpace(t), "true") || strings.TrimSpace(t) == "1"
	default:
		return false
	}
}

func asStringSlice(v any) []string {
	switch t := v.(type) {
	case []any:
		out := []string{}
		for _, item := range t {
			if s := asString(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return t
	case string:
		parts := strings.Split(t, ",")
		out := []string{}
		for _, p := range parts {
			if s := strings.TrimSpace(p); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

var attrCleaner = regexp.MustCompile(`[^A-Za-z0-9_.\-/]+`)

func sanitizeAttrKey(s string) string {
	s = strings.Trim(attrCleaner.ReplaceAllString(strings.TrimSpace(s), "_"), "_")
	if s == "" {
		return "unknown"
	}
	return s
}

func migrationEvidence(source, target, rule, explanation string) models.ConfigMigrationEvidence {
	return models.ConfigMigrationEvidence{SourcePath: source, TargetPath: target, RuleID: rule, Explanation: explanation}
}
