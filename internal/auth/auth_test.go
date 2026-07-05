package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

// Compile-time check: *Auth satisfies ext.AuthProvider.
var _ ext.AuthProvider = (*Auth)(nil)

func TestGenerateAndValidateToken(t *testing.T) {
	a := New("test-secret-key-at-least-32-bytes!")

	token, err := a.GenerateToken("user-001", "admin@test.com", []string{"administrator"})
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	info, err := a.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if info.UserID != "user-001" {
		t.Errorf("UserID = %q, want user-001", info.UserID)
	}
	if info.Email != "admin@test.com" {
		t.Errorf("Email = %q, want admin@test.com", info.Email)
	}
	if len(info.Groups) != 1 || info.Groups[0] != "administrator" {
		t.Errorf("Groups = %v, want [administrator]", info.Groups)
	}
}

func TestTokenExpiresAtReturnsJWTExpiration(t *testing.T) {
	a := New("test-secret-key-at-least-32-bytes!")

	token, err := a.GenerateToken("user-001", "admin@test.com", []string{"administrator"})
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	expiresAt, ok, err := a.TokenExpiresAt(token)
	if err != nil {
		t.Fatalf("TokenExpiresAt: %v", err)
	}
	if !ok {
		t.Fatal("expected token to carry an expiration")
	}
	remaining := time.Until(expiresAt)
	if remaining < 23*time.Hour || remaining > 25*time.Hour {
		t.Fatalf("expiration = %s from now, want roughly 24h", remaining)
	}
}

func TestValidateToken_Invalid(t *testing.T) {
	a := New("test-secret-key-at-least-32-bytes!")
	_, err := a.ValidateToken("garbage.token.here")
	if err == nil {
		t.Error("expected error for invalid token")
	}
}

func TestMiddleware_NoToken(t *testing.T) {
	a := New("test-secret-key-at-least-32-bytes!")

	handler := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/agents", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestMiddleware_ValidToken(t *testing.T) {
	a := New("test-secret-key-at-least-32-bytes!")

	token, _ := a.GenerateToken("user-001", "admin@test.com", []string{"administrator"})

	handler := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := ext.UserInfoFromContext(r.Context())
		if info == nil {
			t.Error("expected UserInfo in context")
		}
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/agents", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestValidateToken_LegacyRoleClaim(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef"
	c := jwt.MapClaims{
		"user_id": "u1",
		"email":   "ops@example.com",
		"role":    "admin",
		"exp":     time.Now().Add(time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	signed, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	a := New(secret)
	info, err := a.ValidateToken(signed)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if len(info.Groups) != 1 || info.Groups[0] != "administrator" {
		t.Errorf("expected groups=[administrator], got %v", info.Groups)
	}
}
