package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/magnify-labs/otel-magnify/internal/auth"
	"github.com/magnify-labs/otel-magnify/internal/store"
	"github.com/magnify-labs/otel-magnify/internal/testdb"
	"github.com/magnify-labs/otel-magnify/pkg/capabilities"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
	"github.com/magnify-labs/otel-magnify/pkg/server"
)

func TestNew_DefaultsCompile(t *testing.T) {
	db, err := store.Open(testdb.New(t).DSN, testPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}

	a := auth.New("test-secret-key-at-least-32-bytes!")

	srv := server.New(server.Config{
		ListenAddr: ":0",
		OpAMPAddr:  ":0",
	}, db, a)

	if srv == nil {
		t.Fatal("New returned nil")
	}
}

func TestServer_StartsAndStops(t *testing.T) {
	db, err := store.Open(testdb.New(t).DSN, testPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}

	a := auth.New("test-secret-key-at-least-32-bytes!")

	srv := server.New(server.Config{
		ListenAddr: ":0",
		OpAMPAddr:  ":0",
	}, db, a)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run(ctx) }()

	// Cancellation should stop Run without relying on a fixed startup sleep.
	cancel()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Server did not stop within 5 seconds")
	}
}

func TestAuthMethods_DefaultsToPassword(t *testing.T) {
	db, err := store.Open(testdb.New(t).DSN, testPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}

	a := auth.New("test-secret-key-at-least-32-bytes!")
	srv := server.New(server.Config{ListenAddr: ":0", OpAMPAddr: ":0"}, db, a)

	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/auth/methods", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Methods []ext.AuthMethod `json:"methods"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(body.Methods) != 1 {
		t.Fatalf("len(methods) = %d, want 1", len(body.Methods))
	}
	got := body.Methods[0]
	if got.ID != "password" || got.Type != "password" {
		t.Errorf("method = %+v, want id=password type=password", got)
	}
	if got.LoginURL != "/api/auth/login" {
		t.Errorf("LoginURL = %q, want /api/auth/login", got.LoginURL)
	}
}

func TestAuthMethods_AppendsViaOption(t *testing.T) {
	db, err := store.Open(testdb.New(t).DSN, testPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}

	a := auth.New("test-secret-key-at-least-32-bytes!")
	okta := ext.AuthMethod{
		ID:          "okta-main",
		Type:        "sso",
		DisplayName: "Okta",
		LoginURL:    "/api/auth/sso/okta-main/login",
	}

	srv := server.New(server.Config{ListenAddr: ":0", OpAMPAddr: ":0"}, db, a,
		server.WithAuthMethod(okta),
	)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/methods", nil))

	var body struct {
		Methods []ext.AuthMethod `json:"methods"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(body.Methods) != 2 {
		t.Fatalf("len(methods) = %d, want 2", len(body.Methods))
	}
	if body.Methods[0].ID != "password" {
		t.Errorf("methods[0].ID = %q, want password (default must come first)", body.Methods[0].ID)
	}
	if body.Methods[1] != okta {
		t.Errorf("methods[1] = %+v, want %+v", body.Methods[1], okta)
	}
}

func TestAuthMethods_DuplicateIDKeepsFirst(t *testing.T) {
	db, err := store.Open(testdb.New(t).DSN, testPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}

	a := auth.New("test-secret-key-at-least-32-bytes!")

	srv := server.New(server.Config{ListenAddr: ":0", OpAMPAddr: ":0"}, db, a,
		server.WithAuthMethod(ext.AuthMethod{
			ID: "password", Type: "sso", DisplayName: "Override", LoginURL: "/nope",
		}),
	)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/methods", nil))

	var body struct {
		Methods []ext.AuthMethod `json:"methods"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(body.Methods) != 1 {
		t.Fatalf("len(methods) = %d, want 1 (duplicate must be dropped)", len(body.Methods))
	}
	if body.Methods[0].DisplayName != "Email + password" {
		t.Errorf("default method was overridden: got DisplayName=%q", body.Methods[0].DisplayName)
	}
}

func TestAuthMethodProvider_DynamicOverridesStatic(t *testing.T) {
	db, err := store.Open(testdb.New(t).DSN, testPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}

	a := auth.New("test-secret-key-at-least-32-bytes!")
	dynamic := []ext.AuthMethod{
		{ID: "password", Type: "password", DisplayName: "Email + password", LoginURL: "/api/auth/login"},
		{ID: "okta-main", Type: "sso", DisplayName: "Okta Corp", LoginURL: "/api/auth/sso/okta-main/login"},
	}
	srv := server.New(server.Config{ListenAddr: ":0", OpAMPAddr: ":0"}, db, a,
		server.WithAuthMethod(ext.AuthMethod{ID: "static-leftover", Type: "sso", DisplayName: "Ignored"}),
		server.WithAuthMethodProvider(func() []ext.AuthMethod { return dynamic }),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/methods", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rec.Code)
	}
	var body struct {
		Methods []ext.AuthMethod `json:"methods"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Methods) != 2 || body.Methods[1].ID != "okta-main" {
		ids := make([]string, len(body.Methods))
		for i, m := range body.Methods {
			ids[i] = m.ID
		}
		t.Fatalf("expected [password, okta-main], got %v", ids)
	}
}

func TestAuthMethodProvider_NilProvider_FallbacksToStatic(t *testing.T) {
	db, err := store.Open(testdb.New(t).DSN, testPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}

	a := auth.New("test-secret-key-at-least-32-bytes!")
	srv := server.New(server.Config{ListenAddr: ":0", OpAMPAddr: ":0"}, db, a)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/methods", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rec.Code)
	}
	var body struct {
		Methods []ext.AuthMethod `json:"methods"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Methods) != 1 || body.Methods[0].ID != "password" {
		t.Fatalf("expected [password] only, got %+v", body.Methods)
	}
}

func TestWithFeatures_PopulatesServerField(t *testing.T) {
	db, err := store.Open(testdb.New(t).DSN, testPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}

	a := auth.New("test-secret-key-at-least-32-bytes!")
	srv := server.New(server.Config{ListenAddr: ":0", OpAMPAddr: ":0"}, db, a,
		server.WithFeatures(map[string]bool{"sso.admin": true}))

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/features", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Features map[string]bool `json:"features"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body unmarshal: %v", err)
	}
	if got := body.Features["sso.admin"]; got != true {
		t.Fatalf("sso.admin: got %v, want true", got)
	}
}

func newServerTestInstance(t *testing.T, opts ...server.Option) *server.Server {
	t.Helper()
	db, err := store.Open(testdb.New(t).DSN, testPoolConfig())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	a := auth.New("test-secret-key-at-least-32-bytes!")
	return server.New(server.Config{ListenAddr: ":0", OpAMPAddr: ":0"}, db, a, opts...)
}

func getFeatures(t *testing.T, handler http.Handler) map[string]bool {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/features", nil))
	var body struct {
		Features map[string]bool `json:"features"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal features: %v", err)
	}
	return body.Features
}

func getCapabilities(t *testing.T, handler http.Handler) capabilities.Document {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/capabilities", nil))
	var body capabilities.Document
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal capabilities: %v", err)
	}
	return body
}

func TestWithFeaturesPreservesFalseValuesAndCopiesInput(t *testing.T) {
	features := map[string]bool{"enabled": true, "disabled": false}
	srv := newServerTestInstance(t, server.WithFeatures(features))
	features["enabled"] = false

	legacy := getFeatures(t, srv.Handler())
	if !legacy["enabled"] || legacy["disabled"] {
		t.Fatalf("features = %#v", legacy)
	}
	document := getCapabilities(t, srv.Handler())
	if len(document.Capabilities) != 2 || document.Capabilities[0].ID != "disabled" || document.Capabilities[0].ReasonCode != capabilities.ReasonNotEnabled {
		t.Fatalf("capabilities = %#v", document)
	}
}

func TestCapabilityOptionsUseLastSnapshot(t *testing.T) {
	first, err := capabilities.New([]capabilities.Capability{{ID: "first", State: capabilities.StateEnabled}})
	if err != nil {
		t.Fatal(err)
	}
	srv := newServerTestInstance(t,
		server.WithCapabilities(first),
		server.WithFeatures(map[string]bool{"second": true}),
	)
	if got := getFeatures(t, srv.Handler()); len(got) != 1 || !got["second"] {
		t.Fatalf("features = %#v", got)
	}
}

// TestWithProtectedRouterHook_AppliesAuthMiddleware locks in the contract
// that routes registered via WithProtectedRouterHook reject anonymous
// requests with 401 and only invoke the handler once the Bearer token has
// been validated and ext.UserInfoFromContext returns a non-nil value.
//
// This is the regression test for the SSO v1 admin auth gap: enterprise
// hooks mounted via WithRouterHook bypassed the auth middleware, so the
// inner perm.RequireGroup check rejected even valid admin tokens.
func TestWithProtectedRouterHook_AppliesAuthMiddleware(t *testing.T) {
	db, err := store.Open(testdb.New(t).DSN, testPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateUser(models.User{ID: "u1", Email: "admin@x.com", PasswordHash: "x"}); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	a := auth.New("test-secret-key-at-least-32-bytes!")

	var capturedUser *ext.UserInfo
	hook := func(r chi.Router) {
		r.Get("/api/protected/ping", func(w http.ResponseWriter, req *http.Request) {
			capturedUser = ext.UserInfoFromContext(req.Context())
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("pong"))
		})
	}

	srv := server.New(server.Config{ListenAddr: ":0", OpAMPAddr: ":0"}, db, a,
		server.WithProtectedRouterHook(hook),
	)
	handler := srv.Handler()

	t.Run("rejects anonymous request with 401", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/protected/ping", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous: status = %d, want 401; body=%s", rec.Code, rec.Body.String())
		}
		if capturedUser != nil {
			t.Fatalf("handler should not run when auth fails; got user=%+v", capturedUser)
		}
	})

	t.Run("rejects invalid Bearer token with 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/protected/ping", nil)
		req.Header.Set("Authorization", "Bearer not-a-valid-token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("invalid token: status = %d, want 401", rec.Code)
		}
	})

	t.Run("accepts valid Bearer token and exposes UserInfo to handler", func(t *testing.T) {
		capturedUser = nil
		token, err := a.GenerateToken("u1", "admin@x.com", []string{"administrator"})
		if err != nil {
			t.Fatalf("GenerateToken: %v", err)
		}
		req := httptest.NewRequest(http.MethodGet, "/api/protected/ping", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("authed: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if capturedUser == nil {
			t.Fatal("UserInfoFromContext returned nil; auth middleware did not run")
		}
		if capturedUser.UserID != "u1" || capturedUser.Email != "admin@x.com" {
			t.Errorf("UserInfo = %+v, want UserID=u1 Email=admin@x.com", capturedUser)
		}
		if len(capturedUser.Groups) != 1 || capturedUser.Groups[0] != "administrator" {
			t.Errorf("UserInfo.Groups = %v, want [administrator]", capturedUser.Groups)
		}
	})
}

func TestWithFeatures_NotSet_ReturnsEmptyMap(t *testing.T) {
	db, err := store.Open(testdb.New(t).DSN, testPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}

	a := auth.New("test-secret-key-at-least-32-bytes!")
	srv := server.New(server.Config{ListenAddr: ":0", OpAMPAddr: ":0"}, db, a)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/features", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	var body struct {
		Features map[string]bool `json:"features"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body unmarshal: %v", err)
	}
	if body.Features == nil {
		t.Fatalf("features should be empty map (non-nil), got nil")
	}
	if len(body.Features) != 0 {
		t.Fatalf("features: got %v, want empty map", body.Features)
	}
}

type serverTestLicenseChecker map[string]bool

func (s serverTestLicenseChecker) FeatureEnabled(feature string) bool { return s[feature] }

func testPoolConfig() store.PoolConfig {
	return store.PoolConfig{MaxOpenConns: 2, MaxIdleConns: 1, ConnMaxLifetime: time.Minute}
}

func TestWithLicenseChecker_AllowsGatedEndpointWithoutAdvertisingFeature(t *testing.T) {
	db, err := store.Open(testdb.New(t).DSN, testPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}

	a := auth.New("test-secret-key-at-least-32-bytes!")
	srv := server.New(server.Config{ListenAddr: ":0", OpAMPAddr: ":0"}, db, a,
		server.WithLicenseChecker(serverTestLicenseChecker{"config_safety.version_intelligence": true}))
	handler := srv.Handler()

	featuresRec := httptest.NewRecorder()
	handler.ServeHTTP(featuresRec, httptest.NewRequest(http.MethodGet, "/api/features", nil))
	var featuresBody struct {
		Features map[string]bool `json:"features"`
	}
	if err := json.Unmarshal(featuresRec.Body.Bytes(), &featuresBody); err != nil {
		t.Fatalf("features body unmarshal: %v", err)
	}
	if len(featuresBody.Features) != 0 {
		t.Fatalf("license checker should not mutate advertised features, got %v", featuresBody.Features)
	}
	capabilitiesBody := getCapabilities(t, handler)
	if len(capabilitiesBody.Capabilities) != 0 {
		t.Fatalf("license checker should not advertise capabilities, got %v", capabilitiesBody.Capabilities)
	}

	token, err := a.GenerateToken("u1", "admin@x.com", []string{"administrator"})
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/workloads/version-intelligence?recommended_version=0.100.0", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}
