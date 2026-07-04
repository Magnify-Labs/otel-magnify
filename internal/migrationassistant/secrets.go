package migrationassistant

import (
	"regexp"
	"strings"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

var secretKeyPattern = regexp.MustCompile(`(?i)(api[_-]?key|apikey|token|license[_-]?key|password|secret|private[_-]?key|client[_-]?secret|access[_-]?key|app[_-]?key)`)

func scanSecrets(vendor, source string, state *conversionState) {
	placeholder := map[string]string{
		models.ConfigMigrationVendorDatadogAgent:    "${DATADOG_API_KEY}",
		models.ConfigMigrationVendorSplunkForwarder: "${SPLUNK_HEC_TOKEN}",
		models.ConfigMigrationVendorNewRelicInfra:   "${NEW_RELIC_LICENSE_KEY}",
	}[vendor]
	if placeholder == "" {
		placeholder = "${REDACTED_SECRET}"
	}
	seen := map[string]bool{}
	section := ""
	for _, raw := range strings.Split(source, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.Contains(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		key := ""
		if idx := strings.IndexAny(line, ":="); idx > 0 {
			key = strings.TrimSpace(line[:idx])
		}
		if key == "" || !secretKeyPattern.MatchString(key) {
			continue
		}
		path := key
		if section != "" {
			path = section + "." + key
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		state.redactions = append(state.redactions, models.ConfigMigrationRedaction{Path: path, Placeholder: placeholderForKey(vendor, key, placeholder), Reason: "Potential secret value was not copied into the draft."})
	}
}

func placeholderForKey(vendor, key, fallback string) string {
	k := strings.ToLower(key)
	switch {
	case strings.Contains(k, "license") && vendor == models.ConfigMigrationVendorNewRelicInfra:
		return "${NEW_RELIC_LICENSE_KEY}"
	case strings.Contains(k, "token") && vendor == models.ConfigMigrationVendorSplunkForwarder:
		return "${SPLUNK_HEC_TOKEN}"
	case strings.Contains(k, "api") && vendor == models.ConfigMigrationVendorDatadogAgent:
		return "${DATADOG_API_KEY}"
	default:
		return fallback
	}
}
