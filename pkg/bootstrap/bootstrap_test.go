package bootstrap_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/testdb"
	"github.com/magnify-labs/otel-magnify/pkg/bootstrap"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/server"
	"github.com/pressly/goose/v3/lock"
)

// TestRun_ReturnsOnContextCancel confirms that bootstrap.Run honours
// context cancellation and returns cleanly. It runs with a PostgreSQL
// test store and a short-lived context.
func TestRun_ReturnsOnContextCancel(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key-at-least-32-bytes!")
	t.Setenv("DB_DSN", testPostgresDSN(t))
	t.Setenv("LISTEN_ADDR", ":0")
	t.Setenv("OPAMP_ADDR", ":0")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Run must return regardless of where it is in startup.

	errCh := make(chan error, 1)
	go func() { errCh <- bootstrap.Run(ctx, bootstrap.Options{}) }()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within 5 seconds of cancel")
	}
}

func TestOpenDatabaseHonorsRunCancellation(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-key-at-least-32-bytes!")
	t.Setenv("DB_DSN", "postgres://postgres:postgres@127.0.0.1:1/postgres?sslmode=disable")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := bootstrap.Run(ctx, bootstrap.Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
}

func TestMigrateContextCancelsBootstrapWhileWaitingForSessionLock(t *testing.T) {
	database := testdb.New(t)
	holder, err := sql.Open("pgx", database.DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = holder.Close() })

	conn, err := holder.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(
		context.Background(),
		"SELECT pg_advisory_lock($1)",
		lock.DefaultLockID,
	); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = conn.ExecContext(
			context.Background(),
			"SELECT pg_advisory_unlock($1)",
			lock.DefaultLockID,
		)
	}()

	t.Setenv("JWT_SECRET", "test-secret-at-least-32-bytes-long-for-hmac")
	t.Setenv("DB_DSN", database.DSN)
	t.Setenv("LISTEN_ADDR", ":0")
	t.Setenv("OPAMP_ADDR", ":0")

	preRunCalled := false
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	err = bootstrap.Run(ctx, bootstrap.Options{
		PreRun: func(_ ext.Store, _ ext.AuthProvider) ([]server.Option, error) {
			preRunCalled = true
			return nil, errors.New("migration lock was bypassed")
		},
	})
	if preRunCalled {
		t.Fatal("PreRun was called before the migration lock became available")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v, want context.DeadlineExceeded", err)
	}
}

// TestRun_FailsWithoutJWTSecret confirms that Run surfaces a missing
// JWT_SECRET as an error rather than calling os.Exit.
func TestRun_FailsWithoutJWTSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	t.Setenv("DB_DSN", testPostgresDSN(t))

	err := bootstrap.Run(context.Background(), bootstrap.Options{})
	if err == nil {
		t.Fatal("expected error when JWT_SECRET is unset, got nil")
	}
}

func TestRun_RejectsWeakJWTSecretsBeforeOpeningDatabase(t *testing.T) {
	tests := []struct {
		name      string
		secret    string
		wantError string
	}{
		{
			name:      "missing",
			secret:    "",
			wantError: "JWT_SECRET environment variable is required",
		},
		{
			name:      "placeholder",
			secret:    "change-me-in-production",
			wantError: "JWT_SECRET must not use the placeholder value",
		},
		{
			name:      "too_short",
			secret:    "short-secret",
			wantError: "JWT_SECRET must be at least 32 characters",
		},
		{
			name:      "whitespace_only",
			secret:    "    	\n",
			wantError: "JWT_SECRET environment variable is required",
		},
		{
			name:      "padded_placeholder",
			secret:    "  change-me-in-production          ",
			wantError: "JWT_SECRET must not use the placeholder value",
		},
		{
			name:      "padded_too_short",
			secret:    "short-secret                         ",
			wantError: "JWT_SECRET must be at least 32 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("JWT_SECRET", tt.secret)
			t.Setenv("DB_DSN", testPostgresDSN(t))

			err := bootstrap.Run(context.Background(), bootstrap.Options{})
			if err == nil {
				t.Fatal("expected weak JWT_SECRET to fail bootstrap, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantError)
			}
		})
	}
}

func TestRun_AcceptsValidJWTSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-at-least-32-bytes-long-for-hmac")
	t.Setenv("DB_DSN", testPostgresDSN(t))
	t.Setenv("LISTEN_ADDR", ":0")
	t.Setenv("OPAMP_ADDR", ":0")

	preRunCalled := false
	opts := bootstrap.Options{
		PreRun: func(_ ext.Store, _ ext.AuthProvider) ([]server.Option, error) {
			preRunCalled = true
			return nil, errors.New("stop after secret validation")
		},
	}
	err := bootstrap.Run(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "stop after secret validation") {
		t.Fatalf("Run error = %v, want PreRun sentinel", err)
	}
	if !preRunCalled {
		t.Fatal("PreRun was not called for a valid JWT_SECRET")
	}
}

func TestPreRun_CalledAfterMigrations_BeforeServerStart(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-at-least-32-bytes-long-for-hmac")
	t.Setenv("DB_DSN", testPostgresDSN(t))
	t.Setenv("LISTEN_ADDR", ":0")
	t.Setenv("OPAMP_ADDR", ":0")

	type preRunObservation struct {
		gotGroups bool
		gotAuth   bool
	}
	observed := make(chan preRunObservation, 1)
	opts := bootstrap.Options{
		PreRun: func(store ext.Store, auth ext.AuthProvider) ([]server.Option, error) {
			// Migrations already applied: seeded groups must exist.
			groups, err := store.ListSystemGroups()
			if err != nil {
				return nil, fmt.Errorf("ListSystemGroups in PreRun: %w", err)
			}
			gotGroups := len(groups) >= 3
			// Auth provider must be non-nil and functional: minting should work.
			gotAuth := false
			if auth != nil {
				if _, err := auth.GenerateToken("u1", "e@x", []string{"viewer"}); err == nil {
					gotAuth = true
				}
			}
			observed <- preRunObservation{gotGroups: gotGroups, gotAuth: gotAuth}
			return nil, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- bootstrap.Run(ctx, opts) }()

	var got preRunObservation
	select {
	case got = <-observed:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("PreRun was not called")
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("bootstrap.Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bootstrap.Run did not return after cancel")
	}

	if !got.gotGroups {
		t.Fatal("PreRun did not see seeded system groups — ran before migrations?")
	}
	if !got.gotAuth {
		t.Fatal("PreRun did not receive a functional auth provider")
	}
}

// NOTE: coverage of "PreRun-returned server.Option is applied to the
// server" is provided transitively by TestPreRun_CalledAfterMigrations_BeforeServerStart
// (PreRun is called with the right args) and by TestAuthMethodProvider_*
// in pkg/server (the option, once registered, is consulted by the
// /api/auth/methods handler).

func TestPreRun_Error_PropagatesAsRunError(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-at-least-32-bytes-long-for-hmac")
	t.Setenv("DB_DSN", testPostgresDSN(t))
	t.Setenv("LISTEN_ADDR", ":0")
	t.Setenv("OPAMP_ADDR", ":0")

	want := errors.New("prerun boom")
	opts := bootstrap.Options{
		PreRun: func(_ ext.Store, _ ext.AuthProvider) ([]server.Option, error) {
			return nil, want
		},
	}
	err := bootstrap.Run(context.Background(), opts)
	if !errors.Is(err, want) {
		t.Fatalf("expected PreRun error to propagate, got %v", err)
	}
}

func testPostgresDSN(t *testing.T) string {
	t.Helper()
	return testdb.New(t).DSN
}
