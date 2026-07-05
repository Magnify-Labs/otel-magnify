// Package auth provides JWT-based authentication and HTTP middleware.
package auth

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

// claims holds the JWT payload. `Role` is kept for tolerant parsing of
// legacy v0.1.x tokens still in circulation (24h TTL). New tokens emit
// `Groups` exclusively.
type claims struct {
	UserID string   `json:"user_id"`
	Email  string   `json:"email"`
	Groups []string `json:"groups,omitempty"`
	Role   string   `json:"role,omitempty"` // legacy, read-only
	jwt.RegisteredClaims
}

// Auth handles token generation and validation for a given HMAC secret.
type Auth struct {
	secret []byte
}

// New creates an Auth instance. The secret must be at least 32 bytes for
// adequate HMAC-SHA256 security.
func New(secret string) *Auth { return &Auth{secret: []byte(secret)} }

// GenerateToken mints a signed JWT. Tokens expire after 24h.
func (a *Auth) GenerateToken(userID, email string, groups []string) (string, error) {
	c := claims{
		UserID: userID,
		Email:  email,
		Groups: groups,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	return token.SignedString(a.secret)
}

// ValidateToken parses and verifies the token. Legacy tokens carrying
// `role` instead of `groups` are converted transparently (admin →
// administrator, viewer → viewer).
func (a *Auth) ValidateToken(tokenStr string) (*ext.UserInfo, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &claims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return a.secret, nil
	})
	if err != nil {
		return nil, err
	}
	c, ok := token.Claims.(*claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	groups := c.Groups
	if len(groups) == 0 && c.Role != "" {
		groups = []string{legacyRoleToGroupName(c.Role)}
	}
	return &ext.UserInfo{UserID: c.UserID, Email: c.Email, Groups: groups}, nil
}

// TokenExpiresAt returns the JWT exp claim after verifying the token signature.
// The second return value is false only for legacy tokens that omit exp.
func (a *Auth) TokenExpiresAt(tokenStr string) (time.Time, bool, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &claims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return a.secret, nil
	})
	if err != nil {
		return time.Time{}, false, err
	}
	c, ok := token.Claims.(*claims)
	if !ok || !token.Valid {
		return time.Time{}, false, fmt.Errorf("invalid token claims")
	}
	if c.ExpiresAt == nil {
		return time.Time{}, false, nil
	}
	return c.ExpiresAt.Time, true, nil
}

func legacyRoleToGroupName(role string) string {
	if role == "admin" {
		return "administrator"
	}
	return role
}

// Middleware returns an HTTP handler that enforces Bearer token authentication.
// On success it stores the validated UserInfo in the request context so downstream
// handlers can retrieve it via ext.UserInfoFromContext.
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		tokenStr := strings.TrimPrefix(header, "Bearer ")
		info, err := a.ValidateToken(tokenStr)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := ext.ContextWithUserInfo(r.Context(), info)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
