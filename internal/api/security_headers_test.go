package api

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRouterSetsSecurityHeaders(t *testing.T) {
	router := NewRouter(nil, wsTestAuth{}, nil, nil, nil, "", nil, nil, 30*24*time.Hour, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	for name, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	} {
		if got := rec.Header().Get(name); got != want {
			t.Fatalf("%s=%q, want %q", name, got, want)
		}
	}
	if got := rec.Header().Get("Content-Security-Policy"); got == "" {
		t.Fatal("Content-Security-Policy header is missing")
	}
}

// Guards against regressions where authenticated failures bypass middleware and
// return without the hardening headers browsers rely on.
func TestRouterSetsStrictSecurityHeaderValuesOnSecureRequests(t *testing.T) {
	router := NewRouter(nil, wsTestAuth{}, nil, nil, nil, "", nil, nil, 30*24*time.Hour, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s, want protected route to reject unauthenticated request", rec.Code, rec.Body.String())
	}
	for name, want := range map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
		"Content-Security-Policy":   "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self' ws: wss:; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'",
		"Permissions-Policy":        "camera=(), microphone=(), geolocation=()",
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
	} {
		if got := rec.Header().Get(name); got != want {
			t.Fatalf("%s=%q, want %q", name, got, want)
		}
	}
}

// Guards against accidentally advertising HSTS on plain HTTP development
// requests, where browsers would pin an unreachable HTTPS origin.
func TestRouterOmitsHSTSOnPlainHTTPRequests(t *testing.T) {
	router := NewRouter(nil, wsTestAuth{}, nil, nil, nil, "", nil, nil, 30*24*time.Hour, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("Strict-Transport-Security=%q, want empty for non-secure requests", got)
	}
}

func TestRequestLoggerRedactsSensitiveQueryValues(t *testing.T) {
	var logs bytes.Buffer
	originalOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(originalOutput) })

	router := NewRouter(nil, wsTestAuth{}, nil, nil, nil, "", nil, nil, 30*24*time.Hour, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/ws?token=super-secret-token&api_key=super-secret-key&safe=value", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	logText := logs.String()
	for _, secret := range []string{"super-secret-token", "super-secret-key"} {
		if strings.Contains(logText, secret) {
			t.Fatalf("request logs leaked sensitive query value %q: %s", secret, logText)
		}
	}
	if !strings.Contains(logText, "token=REDACTED") || !strings.Contains(logText, "api_key=REDACTED") {
		t.Fatalf("request logs did not redact sensitive query values: %s", logText)
	}
	if !strings.Contains(logText, "safe=value") {
		t.Fatalf("request logs should keep non-sensitive query values: %s", logText)
	}
}
