package oteldiff

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

func changedFields(prefix string, b, t any) []FieldChange {
	var out []FieldChange
	walkDiff(prefix, b, t, &out)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
func walkDiff(path string, b, t any, out *[]FieldChange) {
	bm, bok := asMap(b)
	tm, tok := asMap(t)
	if bok && tok {
		keys := unionStringKeysFromMaps(bm, tm)
		for _, k := range keys {
			walkDiff(path+"."+k, bm[k], tm[k], out)
		}
		return
	}
	if !reflect.DeepEqual(b, t) {
		*out = append(*out, FieldChange{Path: path, Before: redactValue(b, path), After: redactValue(t, path), Risk: fieldRisk(path, b, t)})
	}
}
func fieldRisk(path string, b, t any) Risk {
	l := strings.ToLower(path)
	if isAuthPath(l) || strings.Contains(l, "headers") || isInsecurePath(l) && boolBecameTrue(redactValue(b, path), redactValue(t, path)) {
		return RiskHigh
	}
	return RiskLow
}

func redactValue(v any, path string) any {
	if v == nil {
		return nil
	}
	if isSensitivePath(path) {
		return MaskedValue
	}
	switch x := v.(type) {
	case map[string]any:
		m := map[string]any{}
		for _, k := range sortedKeys(x) {
			child := path + "." + k
			if strings.Contains(strings.ToLower(path), "headers") {
				if isSecretKey(k) {
					m[k] = MaskedValue
				} else {
					m[k] = redactValue(x[k], child)
				}
			} else {
				m[k] = redactValue(x[k], child)
			}
		}
		return m
	case []any:
		a := make([]any, len(x))
		for i := range x {
			a[i] = redactValue(x[i], fmt.Sprintf("%s[%d]", path, i))
		}
		return a
	case string:
		if isSensitivePath(path) || looksSecret(x) {
			return MaskedValue
		}
		return sanitizeEndpointIfURL(x)
	default:
		return x
	}
}
func isSensitivePath(path string) bool {
	parts := strings.FieldsFunc(strings.ToLower(path), func(r rune) bool { return r == '.' || r == '/' || r == '[' || r == ']' })
	for _, p := range parts {
		if isSecretKey(p) {
			return true
		}
	}
	return false
}
func isAuthPath(path string) bool {
	return strings.Contains(path, "auth") || strings.Contains(path, "authorization") || strings.Contains(path, "bearer_token") || strings.Contains(path, "api_key") || strings.Contains(path, "client_secret") || strings.Contains(path, "password") || strings.Contains(path, "token")
}
func isSecretKey(k string) bool {
	k = strings.ToLower(strings.ReplaceAll(k, "-", "_"))
	secrets := []string{"authorization", "proxy_authorization", "password", "passwd", "token", "access_token", "refresh_token", "bearer_token", "secret", "client_secret", "api_key", "apikey", "private_key", "key_pem", "cert_pem", "credentials", "cookie", "set_cookie", "x_api_key", "dd_api_key", "signalfx_access_token", "x_sf_token", "x_honeycomb_team", "x_otlp_api_key"}
	for _, s := range secrets {
		if k == s || strings.Contains(k, s) {
			return true
		}
	}
	return false
}
func looksSecret(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "bearer ") || strings.Contains(l, "secret") || strings.Contains(l, "token") || strings.Contains(l, "api-key") || strings.Contains(l, "apikey")
}
func redactSecretLikeString(s string) string {
	if looksSecret(s) {
		return MaskedValue
	}
	return s
}
func sanitizeEndpointIfURL(s string) string {
	if strings.Contains(s, "://") && (strings.Contains(s, "@") || strings.Contains(s, "?")) {
		return sanitizeEndpoint(s)
	}
	return s
}
func isInsecurePath(path string) bool {
	return strings.HasSuffix(path, ".insecure") || strings.HasSuffix(path, ".insecure_skip_verify") || strings.Contains(path, "tls.insecure")
}
func boolBecameTrue(b, t any) bool {
	bv, bok := b.(bool)
	tv, tok := t.(bool)
	return tok && tv && (!bok || !bv)
}

func strongSamplingChange(b, t any) bool {
	bf, bok := number(b)
	tf, tok := number(t)
	if !bok || !tok {
		return false
	}
	if tf == 0 && bf != 0 {
		return true
	}
	if bf == 0 {
		return false
	}
	ratio := tf / bf
	return ratio <= 0.5 || ratio >= 2 || (bf == 100 && tf < 100)
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
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
func isSamplingID(id string) bool {
	l := strings.ToLower(id)
	return strings.Contains(l, "sampling") || strings.Contains(l, "probabilistic_sampler") || strings.Contains(l, "tail_sampling")
}
