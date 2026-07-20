package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/auth"
	"github.com/magnify-labs/otel-magnify/internal/store"
	"github.com/magnify-labs/otel-magnify/internal/testdb"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

type storeWithoutReadinessChecker struct {
	ext.Store
}

type readinessTestStore struct {
	ext.Store
	pingContext func(context.Context) error
}

func (s *readinessTestStore) PingContext(ctx context.Context) error {
	return s.pingContext(ctx)
}

func TestHealthzRemainsLiveWhenDatabaseIsUnavailable(t *testing.T) {
	db := openHealthTestDatabase(t)
	router := newHealthTestRouter(db)
	if err := db.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	rec := serveHealthRequest(router, "/healthz")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q; want 200", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "ok" {
		t.Fatalf("body = %q, want %q", got, "ok")
	}
}

func TestReadyzReflectsDatabaseConnectivity(t *testing.T) {
	db := openHealthTestDatabase(t)
	router := newHealthTestRouter(db)

	rec := serveHealthRequest(router, "/readyz")
	if rec.Code != http.StatusOK {
		t.Fatalf("connected status = %d, body = %q; want 200", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "ready" {
		t.Fatalf("connected body = %q, want %q", got, "ready")
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	rec = serveHealthRequest(router, "/readyz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("disconnected status = %d, body = %q; want 503", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "not ready" {
		t.Fatalf("disconnected body = %q, want generic %q", got, "not ready")
	}
}

func TestReadyzFailsClosedWithoutDatabaseChecker(t *testing.T) {
	db := openHealthTestDatabase(t)
	router := newHealthTestRouter(storeWithoutReadinessChecker{Store: db})

	rec := serveHealthRequest(router, "/readyz")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %q; want 503", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "not ready" {
		t.Fatalf("body = %q, want generic %q", got, "not ready")
	}
}

func TestReadyzFailsClosedWithTypedNilDatabase(t *testing.T) {
	var db *store.DB
	var database ext.Store = db
	router := newHealthTestRouter(database)

	rec := serveHealthRequest(router, "/readyz")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %q; want 503", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "not ready" {
		t.Fatalf("body = %q, want generic %q", got, "not ready")
	}
}

func TestReadyzDoesNotExposeDatabaseErrors(t *testing.T) {
	const databaseError = "postgres: password authentication failed for user secret-user at db.internal"
	db := &readinessTestStore{
		pingContext: func(context.Context) error {
			return errors.New(databaseError)
		},
	}
	router := newHealthTestRouter(db)

	rec := serveHealthRequest(router, "/readyz")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %q; want 503", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "not ready" {
		t.Fatalf("body = %q, want generic %q", got, "not ready")
	}
	if strings.Contains(rec.Body.String(), databaseError) {
		t.Fatalf("response exposed database error: %q", rec.Body.String())
	}
}

func TestReadyzUsesOneSecondRequestContext(t *testing.T) {
	type requestContextKey struct{}
	const requestContextValue = "request-context"

	var (
		deadline     time.Time
		hasDeadline  bool
		contextValue any
	)
	db := &readinessTestStore{
		pingContext: func(ctx context.Context) error {
			deadline, hasDeadline = ctx.Deadline()
			contextValue = ctx.Value(requestContextKey{})
			return nil
		},
	}
	router := newHealthTestRouter(db)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	started := time.Now()
	req = req.WithContext(context.WithValue(req.Context(), requestContextKey{}, requestContextValue))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q; want 200", rec.Code, rec.Body.String())
	}
	if !hasDeadline {
		t.Fatal("database readiness context has no deadline")
	}
	if timeout := deadline.Sub(started); timeout < 900*time.Millisecond || timeout > 1100*time.Millisecond {
		t.Fatalf("database readiness timeout = %s, want one second", timeout)
	}
	if contextValue != requestContextValue {
		t.Fatalf("database readiness context value = %v, want request context value", contextValue)
	}
}

func openHealthTestDatabase(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(testdb.New(t).DSN, store.PoolConfig{
		MaxOpenConns:    2,
		MaxIdleConns:    1,
		ConnMaxIdleTime: time.Minute,
		ConnMaxLifetime: time.Minute,
	})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newHealthTestRouter(db ext.Store) http.Handler {
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

func serveHealthRequest(router http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
