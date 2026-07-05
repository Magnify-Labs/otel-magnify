package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoginRejectsOversizedJSONBody(t *testing.T) {
	_, router, _, audit := newAuditTestAPI(t)

	body := `{"email":"alice@example.com","password":"` + strings.Repeat("x", int(maxJSONBodyBytes)) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "request body too large") {
		t.Fatalf("body = %q, want safe body-size error", rec.Body.String())
	}
	if findEvent(audit.snapshot(), "auth.login.failure") != nil {
		t.Fatalf("oversized validation rejection must not emit login failure audit event")
	}
}

func TestLoginRejectsOversizedTrailingJSONBody(t *testing.T) {
	_, router, _, audit := newAuditTestAPI(t)

	body := `{"email":"alice@example.com","password":"wrong-password"}` + strings.Repeat(" ", int(maxJSONBodyBytes))
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "request body too large") {
		t.Fatalf("body = %q, want safe body-size error", rec.Body.String())
	}
	if findEvent(audit.snapshot(), "auth.login.failure") != nil {
		t.Fatalf("oversized trailing body must not emit login failure audit event")
	}
}

func TestJSONPayloadEndpointsRejectOversizedBodies(t *testing.T) {
	_, router, _, _ := newAuditTestAPI(t)
	oversizedString := strings.Repeat("x", int(maxJSONBodyBytes))

	tests := []struct {
		name   string
		method string
		url    string
		body   string
	}{
		{
			name:   "create config",
			method: http.MethodPost,
			url:    "/api/configs",
			body:   `{"name":"oversized","content":"` + oversizedString + `"}`,
		},
		{
			name:   "change password",
			method: http.MethodPut,
			url:    "/api/me/password",
			body:   `{"current_password":"old-password-xx","new_password":"` + oversizedString + `"}`,
		},
		{
			name:   "preferences",
			method: http.MethodPut,
			url:    "/api/me/preferences",
			body:   `{"theme":"dark","language":"` + oversizedString + `"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := authedJSONRequest(t, tt.method, tt.url, tt.body, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "request body too large") {
				t.Fatalf("body = %q, want safe body-size error", rec.Body.String())
			}
		})
	}
}

func TestLoginRateLimitsRepeatedFailuresByIP(t *testing.T) {
	_, router, _, _ := newAuditTestAPI(t)

	for i := 1; i <= 6; i++ {
		body := fmt.Sprintf(`{"email":"ghost-%d@example.com","password":"wrong-password"}`, i)
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
		req.RemoteAddr = "203.0.113.10:12345"
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if i <= 5 && rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, body=%s; want 401 before throttle", i, rec.Code, rec.Body.String())
		}
		if i == 6 && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("attempt %d status = %d, body=%s; want 429 after repeated failures", i, rec.Code, rec.Body.String())
		}
	}
}
