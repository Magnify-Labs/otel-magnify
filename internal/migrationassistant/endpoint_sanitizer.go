package migrationassistant

import (
	"net/url"
	"strings"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

const redactedEndpointPlaceholder = "${REDACTED_SECRET}"

func sanitizeEndpoint(endpoint, path string, state *conversionState) string {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return ""
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return trimmed
	}

	if parsed.User != nil {
		parsed.User = nil
		addEndpointRedaction(state, path+".userinfo")
	}

	if parsed.RawQuery != "" {
		parsed.RawQuery = sanitizeEndpointRawQuery(parsed.RawQuery, path, state)
	}

	return parsed.String()
}

func sanitizeEndpointRawQuery(rawQuery, path string, state *conversionState) string {
	parts := strings.Split(rawQuery, "&")
	for i, part := range parts {
		if part == "" {
			continue
		}
		key := part
		if idx := strings.IndexByte(part, '='); idx >= 0 {
			key = part[:idx]
		}
		decodedKey, err := url.QueryUnescape(key)
		if err != nil {
			decodedKey = key
		}
		if !secretKeyPattern.MatchString(decodedKey) {
			continue
		}
		parts[i] = key + "=" + redactedEndpointPlaceholder
		addEndpointRedaction(state, path+".query."+decodedKey)
	}
	return strings.Join(parts, "&")
}

func addEndpointRedaction(state *conversionState, path string) {
	state.redactions = append(state.redactions, models.ConfigMigrationRedaction{
		Path:        path,
		Placeholder: redactedEndpointPlaceholder,
		Reason:      "Sensitive endpoint material was redacted before rendering the draft.",
	})
}
