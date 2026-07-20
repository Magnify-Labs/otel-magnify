package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/auth"
	"github.com/magnify-labs/otel-magnify/internal/store"
	"github.com/magnify-labs/otel-magnify/internal/testdb"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

type storeWithoutStatsProvider struct {
	ext.Store
}

func TestDatabaseStatsReturnsCountsWithoutConnectionDetails(t *testing.T) {
	db, schema := openDatabaseStatsTestDatabase(t)
	router := newDatabaseStatsTestRouter(db)
	want := db.Stats()
	req := authedRequestForGroups(t, http.MethodGet, "/api/system/database", "", []string{"administrator"})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s; want 200", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	wantFields := map[string]int64{
		"max_open_connections": int64(want.MaxOpenConnections),
		"open_connections":     int64(want.OpenConnections),
		"in_use":               int64(want.InUse),
		"idle":                 int64(want.Idle),
		"wait_count":           want.WaitCount,
		"wait_duration_ms":     want.WaitDuration.Milliseconds(),
		"max_idle_closed":      want.MaxIdleClosed,
		"max_idle_time_closed": want.MaxIdleTimeClosed,
		"max_lifetime_closed":  want.MaxLifetimeClosed,
	}
	if len(body) != len(wantFields) {
		t.Fatalf("response fields = %v, want only %v", sortedMapKeys(body), sortedMapKeys(wantFields))
	}
	for field, wantValue := range wantFields {
		value, ok := body[field]
		if !ok {
			t.Errorf("response is missing numeric field %q", field)
			continue
		}
		number, ok := value.(float64)
		if !ok {
			t.Errorf("field %q type = %T, want JSON number", field, value)
			continue
		}
		if got := int64(number); got != wantValue {
			t.Errorf("field %q = %d, want %d", field, got, wantValue)
		}
	}

	response := strings.ToLower(rec.Body.String())
	for _, connectionDetail := range []string{"dsn", "host", "database", "user", "error", strings.ToLower(schema)} {
		if strings.Contains(response, connectionDetail) {
			t.Errorf("response exposed connection detail %q: %s", connectionDetail, rec.Body.String())
		}
	}
}

func TestDatabaseStatsRequiresManageSettings(t *testing.T) {
	db, _ := openDatabaseStatsTestDatabase(t)
	router := newDatabaseStatsTestRouter(db)

	tests := []struct {
		name       string
		groups     []string
		wantStatus int
	}{
		{name: "viewer", groups: []string{"viewer"}, wantStatus: http.StatusForbidden},
		{name: "administrator", groups: []string{"administrator"}, wantStatus: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := authedRequestForGroups(t, http.MethodGet, "/api/system/database", "", tt.groups)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, body = %s; want %d", rec.Code, rec.Body.String(), tt.wantStatus)
			}
		})
	}
}

func TestDatabaseStatsFailsClosedWithoutStatsProvider(t *testing.T) {
	db, schema := openDatabaseStatsTestDatabase(t)
	router := newDatabaseStatsTestRouter(storeWithoutStatsProvider{Store: db})
	req := authedRequestForGroups(t, http.MethodGet, "/api/system/database", "", []string{"administrator"})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s; want 503", rec.Code, rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), strings.ToLower(schema)) {
		t.Fatalf("response exposed database schema: %s", rec.Body.String())
	}
}

func TestDatabaseStatsFailsClosedWithTypedNilProvider(t *testing.T) {
	var db *store.DB
	var database ext.Store = db
	router := newDatabaseStatsTestRouter(database)
	req := authedRequestForGroups(t, http.MethodGet, "/api/system/database", "", []string{"administrator"})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s; want 503", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) != 1 || body["error"] != "database statistics unavailable" {
		t.Fatalf("body = %v, want generic database statistics error", body)
	}
}

func openDatabaseStatsTestDatabase(t *testing.T) (*store.DB, string) {
	t.Helper()
	database := testdb.New(t)
	db, err := store.Open(database.DSN, store.PoolConfig{
		MaxOpenConns:    4,
		MaxIdleConns:    1,
		ConnMaxIdleTime: time.Minute,
		ConnMaxLifetime: time.Minute,
	})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, database.Schema
}

func newDatabaseStatsTestRouter(db ext.Store) http.Handler {
	return NewRouter(
		db,
		auth.New("test-secret-key-at-least-32-bytes!"),
		nil,
		nil,
		nil,
		"",
		nil,
		nil,
		30*24*time.Hour,
		testCapabilities(nil),
		nil,
		nil,
	)
}

func sortedMapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
