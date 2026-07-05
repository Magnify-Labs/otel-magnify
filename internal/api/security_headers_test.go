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
