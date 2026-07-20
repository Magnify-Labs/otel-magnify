package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/auth"
	"github.com/magnify-labs/otel-magnify/internal/store"
	"github.com/magnify-labs/otel-magnify/internal/testdb"
	"github.com/magnify-labs/otel-magnify/pkg/capabilities"
)

// newFeaturesTestRouter builds a minimal router for features endpoint tests.
// It mirrors the helper pattern from mehandler_test.go: real in-memory store
// and real auth, no OpAMP pusher or WebSocket hub.
func newFeaturesTestRouter(t *testing.T, registry capabilities.Registry) http.Handler {
	t.Helper()
	db, err := store.Open(testdb.New(t).DSN, testPoolConfig())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	a := auth.New("0123456789abcdef0123456789abcdef")
	return NewRouter(db, a, nil, nil, nil, "", nil, nil, 30*24*time.Hour, registry, nil, nil)
}

func TestListFeatures_Public_NoAuth(t *testing.T) {
	r := newFeaturesTestRouter(t, testCapabilities(map[string]bool{"sso.admin": true}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/features", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestListFeatures_ReturnsConfiguredMap(t *testing.T) {
	r := newFeaturesTestRouter(t, testCapabilities(map[string]bool{
		"sso.admin":    true,
		"audit.export": false,
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/features", nil))

	var body struct {
		Features map[string]bool `json:"features"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := body.Features["sso.admin"]; got != true {
		t.Fatalf("sso.admin: got %v, want true", got)
	}
	if got, ok := body.Features["audit.export"]; !ok || got != false {
		t.Fatalf("audit.export: got (%v, %v), want (false, true)", got, ok)
	}
}

func TestListFeatures_NilMap_Returns200WithEmptyFeatures(t *testing.T) {
	r := newFeaturesTestRouter(t, testCapabilities(nil))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/features", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	var body struct {
		Features map[string]bool `json:"features"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Features == nil {
		t.Fatalf("features must be non-nil even if empty (frontend distinguishes via key presence)")
	}
}

func TestListCapabilities_PublicAndDeterministic(t *testing.T) {
	registry, err := capabilities.New([]capabilities.Capability{
		{ID: "zeta", State: capabilities.StateDisabled, ReasonCode: capabilities.ReasonNotEnabled},
		{ID: "alpha", State: capabilities.StateEnabled},
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	r := newFeaturesTestRouter(t, registry)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/capabilities", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	var body capabilities.Document
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.APIVersion != "v1" || len(body.Capabilities) != 2 || body.Capabilities[0].ID != "alpha" || body.Capabilities[1].ID != "zeta" {
		t.Fatalf("body = %#v", body)
	}
}

func TestListCapabilities_EmptyRegistryUsesArray(t *testing.T) {
	r := newFeaturesTestRouter(t, capabilities.Registry{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/capabilities", nil))
	if strings.TrimSpace(rec.Body.String()) != `{"api_version":"v1","capabilities":[]}` {
		t.Fatalf("body = %s", rec.Body.String())
	}
}
