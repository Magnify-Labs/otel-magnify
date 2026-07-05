package api

import (
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

var sensitiveQueryKeys = map[string]struct{}{
	"access_token":  {},
	"api_key":       {},
	"apikey":        {},
	"auth":          {},
	"authorization": {},
	"bearer":        {},
	"code":          {},
	"credential":    {},
	"jwt":           {},
	"key":           {},
	"password":      {},
	"secret":        {},
	"session":       {},
	"token":         {},
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, r)
		//nolint:gosec // request-derived fields are redacted and stripped of control chars before logging.
		log.Printf("%s %s %s from %s - %d %dB in %s", sanitizeLogField(r.Method), sanitizeLogField(sanitizedRequestURI(r)), sanitizeLogField(r.Proto), sanitizeLogField(r.RemoteAddr), ww.Status(), ww.BytesWritten(), time.Since(start))
	})
}

func sanitizedRequestURI(r *http.Request) string {
	u := *r.URL
	if u.RawQuery != "" {
		u.RawQuery = redactSensitiveQuery(u.Query()).Encode()
	}
	return u.RequestURI()
}

func redactSensitiveQuery(values url.Values) url.Values {
	redacted := make(url.Values, len(values))
	for key, vals := range values {
		copied := append([]string(nil), vals...)
		if isSensitiveQueryKey(key) {
			for i := range copied {
				copied[i] = "REDACTED"
			}
		}
		redacted[key] = copied
	}
	return redacted
}

func isSensitiveQueryKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if _, ok := sensitiveQueryKeys[key]; ok {
		return true
	}
	return strings.Contains(key, "token") || strings.Contains(key, "secret") || strings.Contains(key, "password")
}

func sanitizeLogField(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '	' || r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, value)
}
