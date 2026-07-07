package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/auth"
	"github.com/magnify-labs/otel-magnify/internal/store"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestListAuthMethods_ReturnsConfiguredMethods(t *testing.T) {
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}

	router := NewRouter(
		db,
		auth.New("test-secret-key-at-least-32-bytes!"),
		nil,
		nil,
		nil,
		"",
		nil,
		func() []ext.AuthMethod {
			return []ext.AuthMethod{{ID: "password", Type: "password", DisplayName: "Email + password", LoginURL: "/api/auth/login"}}
		},
		30*24*time.Hour,
		nil,
		nil,
		nil,
	)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/methods", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Methods []ext.AuthMethod `json:"methods"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(body.Methods) != 1 || body.Methods[0].ID != "password" || body.Methods[0].LoginURL != "/api/auth/login" {
		t.Fatalf("methods = %+v", body.Methods)
	}
}

func TestGetConfig_Handler(t *testing.T) {
	db, router, _ := newTestAPI(t)
	cfg := models.Config{ID: "cfg-1", Name: "collector", Content: "receivers:\n  otlp: {}", CreatedAt: time.Now().UTC()}
	if err := db.CreateConfig(cfg); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authedRequest(t, http.MethodGet, "/api/configs/cfg-1"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got models.Config
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != cfg.ID || got.Content != cfg.Content {
		t.Fatalf("config = %+v", got)
	}
}

func TestResolveAlert_Handler(t *testing.T) {
	db, router, _ := newTestAPI(t)
	if err := db.UpsertWorkload(models.Workload{
		ID: "w1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateAlert(models.Alert{ID: "alert-1", WorkloadID: "w1", Rule: "workload_down", Severity: "critical", Message: "down", FiredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authedPost(t, "/api/alerts/alert-1/resolve", "{}"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	alerts, err := db.ListAlerts(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 || alerts[0].ResolvedAt == nil {
		t.Fatalf("alert not resolved: %+v", alerts)
	}
}

func TestServeStatic_FallsBackToIndexForSPARoute(t *testing.T) {
	staticFS := fstest.MapFS{
		"index.html":    {Data: []byte(`<!doctype html><title>otel-magnify</title><div id="root"></div>`)},
		"assets/app.js": {Data: []byte(`console.log("ok")`)},
	}

	rec := httptest.NewRecorder()
	ServeStatic(staticFS).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/inventory", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "otel-magnify") {
		t.Fatalf("expected SPA index fallback, body=%s", rec.Body.String())
	}
}

func TestFullStackSmoke_LoginRepresentativeAPIAndFrontendFallback(t *testing.T) {
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	hash, err := hashPassword("smoke-pass-123")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateUser(models.User{ID: "smoke-user", Email: "smoke@example.com", PasswordHash: hash}); err != nil {
		t.Fatal(err)
	}
	if err := db.AttachUserToGroupByName("smoke-user", "administrator"); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertWorkload(models.Workload{
		ID: "w-smoke", DisplayName: "smoke collector", Type: "collector", Version: "0.100.0", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{"group": "smoke"},
	}); err != nil {
		t.Fatal(err)
	}

	hub := NewHub()
	go hub.Run()
	t.Cleanup(hub.Stop)
	staticFS := fstest.MapFS{
		"index.html":    {Data: []byte(`<!doctype html><title>otel-magnify</title><script type="module" src="/assets/app.js"></script>`)},
		"assets/app.js": {Data: []byte(`fetch('/api/features')`)},
	}
	router := NewRouter(
		db,
		auth.New("test-secret-key-at-least-32-bytes!"),
		hub,
		nil,
		nil,
		"",
		staticFS,
		func() []ext.AuthMethod {
			return []ext.AuthMethod{{ID: "password", Type: "password", DisplayName: "Email + password", LoginURL: "/api/auth/login"}}
		},
		30*24*time.Hour,
		map[string]bool{FeatureConfigSafetyVersionIntelligence: true},
		nil,
		nil,
	)

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	client := server.Client()

	loginBody := bytes.NewBufferString(`{"email":"smoke@example.com","password":"smoke-pass-123"}`)
	loginResp, err := client.Post(server.URL+"/api/auth/login", "application/json", loginBody)
	if err != nil {
		t.Fatal(err)
	}
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", loginResp.StatusCode)
	}
	var login struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(loginResp.Body).Decode(&login); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if login.Token == "" {
		t.Fatal("expected non-empty token")
	}

	apiReq, err := http.NewRequest(http.MethodGet, server.URL+"/api/workloads/version-intelligence?recommended_version=0.100.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	apiReq.Header.Set("Authorization", "Bearer "+login.Token)
	apiResp, err := client.Do(apiReq)
	if err != nil {
		t.Fatal(err)
	}
	defer apiResp.Body.Close()
	if apiResp.StatusCode != http.StatusOK {
		t.Fatalf("representative API status = %d", apiResp.StatusCode)
	}
	var intelligence models.FleetVersionIntelligence
	if err := json.NewDecoder(apiResp.Body).Decode(&intelligence); err != nil {
		t.Fatalf("decode representative API: %v", err)
	}
	if intelligence.SchemaVersion != "fleet-version-intelligence.v1" || len(intelligence.VersionMatrix) != 1 {
		t.Fatalf("unexpected representative API response: %+v", intelligence)
	}

	frontendResp, err := client.Get(server.URL + "/inventory")
	if err != nil {
		t.Fatal(err)
	}
	defer frontendResp.Body.Close()
	if frontendResp.StatusCode != http.StatusOK {
		t.Fatalf("frontend status = %d", frontendResp.StatusCode)
	}
	body, err := io.ReadAll(frontendResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("/assets/app.js")) {
		t.Fatalf("frontend fallback did not serve index with app script: %s", string(body))
	}

	assetResp, err := client.Get(server.URL + "/assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	defer assetResp.Body.Close()
	if assetResp.StatusCode != http.StatusOK {
		t.Fatalf("frontend asset status = %d", assetResp.StatusCode)
	}
	assetBody, err := io.ReadAll(assetResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(assetBody, []byte("/api/features")) {
		t.Fatalf("frontend asset did not include API integration call: %s", string(assetBody))
	}
}
