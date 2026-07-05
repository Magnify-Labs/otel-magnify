package api

import (
	"net/http"
	"net/http/httptest"
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
