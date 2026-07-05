package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/auth"
)

const defaultSessionCookieTTL = 24 * time.Hour

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time) {
	maxAge := int(defaultSessionCookieTTL.Seconds())
	if !expiresAt.IsZero() {
		if ttl := time.Until(expiresAt); ttl > 0 {
			maxAge = int(ttl.Seconds())
		} else {
			maxAge = 0
		}
	}
	//nolint:gosec // Secure is enabled for TLS/proxied HTTPS; plain HTTP dev cannot receive Secure cookies.
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	//nolint:gosec // Secure is enabled for TLS/proxied HTTPS; plain HTTP dev cannot receive Secure cookies.
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func tokenExpiresAt(a extTokenExpirationProvider, token string) time.Time {
	expiresAt, hasExpiry, err := a.TokenExpiresAt(token)
	if err != nil || !hasExpiry {
		return time.Time{}
	}
	return expiresAt
}

type extTokenExpirationProvider interface {
	TokenExpiresAt(token string) (time.Time, bool, error)
}
