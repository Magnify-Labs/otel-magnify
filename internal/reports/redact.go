// Package reports builds deterministic evidence packs and renders report exports.
package reports

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

var redactionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)authorization\s*[:=]\s*bearer\s+[^\s,;]+`),
	regexp.MustCompile(`(?i)(SECRET_[A-Z0-9_]*|[A-Z0-9_]*(?:TOKEN|PASSWORD)|api_key|client_secret)\s*[:=]\s*[^\s,;]+`),
	regexp.MustCompile(`https?://[^\s/@:]+:[^\s/@]+@[^\s]+`),
	regexp.MustCompile(`https?://[^\s]*(?:internal|tenant)[^\s]*`),
}

// RedactString removes sensitive values from report strings before rendering or hashing.
func RedactString(s string) string {
	out := models.SanitizeRemoteConfigErrorMessage(s)
	for _, re := range redactionPatterns {
		out = re.ReplaceAllString(out, "[REDACTED]")
	}
	return out
}

func redactValue(s string) string {
	if s == "" {
		return ""
	}
	if strings.Contains(s, "@") {
		return "[REDACTED]"
	}
	return RedactString(s)
}

func redactFacts(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "secret") || strings.Contains(lk, "token") || strings.Contains(lk, "password") || strings.Contains(lk, "api_key") || strings.Contains(lk, "client_secret") {
			out[k] = "[REDACTED]"
			continue
		}
		switch x := v.(type) {
		case string:
			out[k] = RedactString(x)
		case map[string]string:
			m := map[string]string{}
			for mk, mv := range x {
				m[mk] = RedactString(mv)
			}
			out[k] = m
		default:
			out[k] = v
		}
	}
	return out
}

func canonicalScalar(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int:
		return strconvItoa(x)
	case int64:
		return strconvFormatInt(x)
	case float64:
		b, _ := json.Marshal(x)
		return string(b)
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}

func strconvItoa(i int) string { return strconvFormatInt(int64(i)) }
func strconvFormatInt(i int64) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	buf := [20]byte{}
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
