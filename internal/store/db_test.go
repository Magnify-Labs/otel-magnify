package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/testdb"
	"github.com/pressly/goose/v3/lock"
)

func TestOpenRejectsEmptyDSN(t *testing.T) {
	_, err := Open("", PoolConfig{MaxOpenConns: 1, MaxIdleConns: 1})
	if err == nil || !strings.Contains(err.Error(), "DB_DSN") {
		t.Fatalf("Open() error = %v", err)
	}
}

func TestNewCreatesIsolatedSchema(t *testing.T) {
	database := testdb.New(t)
	db, err := sql.Open("pgx", database.DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec("CREATE TABLE isolation_probe (id integer)"); err != nil {
		t.Fatal(err)
	}
}

func TestOpen(t *testing.T) {
	db, err := Open(testdb.New(t).DSN, PoolConfig{
		MaxOpenConns:    2,
		MaxIdleConns:    1,
		ConnMaxIdleTime: time.Minute,
		ConnMaxLifetime: time.Minute,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
}

func TestOpenRejectsInvalidPoolBounds(t *testing.T) {
	dsn := testdb.New(t).DSN
	tests := []struct {
		name      string
		pool      PoolConfig
		wantError string
	}{
		{
			name: "non_positive_max_open_connections",
			pool: PoolConfig{
				MaxOpenConns: 0,
				MaxIdleConns: 0,
			},
			wantError: "MaxOpenConns must be greater than 0",
		},
		{
			name: "negative_max_idle_connections",
			pool: PoolConfig{
				MaxOpenConns: 1,
				MaxIdleConns: -1,
			},
			wantError: "MaxIdleConns must be non-negative",
		},
		{
			name: "max_idle_connections_exceed_max_open_connections",
			pool: PoolConfig{
				MaxOpenConns: 1,
				MaxIdleConns: 2,
			},
			wantError: "MaxIdleConns must not exceed MaxOpenConns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := Open(dsn, tt.pool)
			if db != nil {
				_ = db.Close()
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Open() error = %v, want substring %q", err, tt.wantError)
			}
		})
	}
}

func TestOpenContextHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	db, err := OpenContext(ctx, testdb.New(t).DSN, PoolConfig{
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	})
	if db != nil {
		_ = db.Close()
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("OpenContext() error = %v, want context.Canceled", err)
	}
}

func TestOpenConfiguresIdleTime(t *testing.T) {
	database := testdb.New(t)
	db, err := Open(database.DSN, PoolConfig{
		MaxOpenConns:    1,
		MaxIdleConns:    1,
		ConnMaxIdleTime: 10 * time.Millisecond,
		ConnMaxLifetime: time.Minute,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var initialPID int
	if err := db.QueryRow("SELECT pg_backend_pid()").Scan(&initialPID); err != nil {
		t.Fatalf("read initial backend PID: %v", err)
	}

	observer, err := sql.Open("pgx", database.DSN)
	if err != nil {
		t.Fatalf("open observer connection: %v", err)
	}
	t.Cleanup(func() { _ = observer.Close() })

	deadline := time.Now().Add(3 * time.Second)
	for {
		var active bool
		if err := observer.QueryRow(
			"SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE pid = $1)",
			initialPID,
		).Scan(&active); err != nil {
			t.Fatalf("observe initial backend PID: %v", err)
		}
		if !active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("backend PID %d remained active after the idle timeout", initialPID)
		}
		time.Sleep(20 * time.Millisecond)
	}

	var replacementPID int
	if err := db.QueryRow("SELECT pg_backend_pid()").Scan(&replacementPID); err != nil {
		t.Fatalf("read replacement backend PID: %v", err)
	}
	if replacementPID == initialPID {
		t.Fatalf("replacement backend PID = %d, want a new connection", replacementPID)
	}
}

func TestMigrate(t *testing.T) {
	db := openTestPostgres(t)

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Verify tables exist by querying them
	tables := []string{"configs", "workloads", "workload_configs", "workload_events", "alerts", "users"}
	for _, table := range tables {
		_, err := db.Exec("SELECT count(*) FROM " + table)
		if err != nil {
			t.Errorf("table %s not created: %v", table, err)
		}
	}
}

func TestMigrateSerializesAcrossDatabaseHandles(t *testing.T) {
	database := testdb.New(t)
	first := openUnmigratedTestPostgres(t, database.DSN)
	second := openUnmigratedTestPostgres(t, database.DSN)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := make(chan struct{})
	results := make(chan error, 2)
	for _, db := range []*DB{first, second} {
		go func(db *DB) {
			<-start
			results <- db.MigrateContext(ctx)
		}(db)
	}
	close(start)

	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("concurrent MigrateContext: %v", err)
		}
	}

	var applied int
	var distinct int
	var maxVersion int64
	if err := first.QueryRowPostgres(`
		SELECT COUNT(*), COUNT(DISTINCT version_id), COALESCE(MAX(version_id), 0)
		FROM goose_db_version
		WHERE is_applied AND version_id > 0
	`).Scan(&applied, &distinct, &maxVersion); err != nil {
		t.Fatalf("query goose versions: %v", err)
	}
	if applied != 25 || distinct != 25 || maxVersion != 26 {
		t.Fatalf("goose versions = applied %d, distinct %d, max %d; want 25, 25, 26", applied, distinct, maxVersion)
	}
}

func TestMigrateContextCancelsWhileWaitingForSessionLock(t *testing.T) {
	database := testdb.New(t)
	holder := openUnmigratedTestPostgres(t, database.DSN)
	waiter := openUnmigratedTestPostgres(t, database.DSN)

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

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := waiter.MigrateContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("MigrateContext() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestMigrateLockWaitIsBounded(t *testing.T) {
	database := testdb.New(t)
	holder := openUnmigratedTestPostgres(t, database.DSN)
	waiter := openUnmigratedTestPostgres(t, database.DSN)

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

	locker, err := newMigrationLocker(1)
	if err != nil {
		t.Fatalf("newMigrationLocker: %v", err)
	}
	startedAt := time.Now()
	err = waiter.migrate(context.Background(), locker)
	elapsed := time.Since(startedAt)
	if err == nil || !strings.Contains(err.Error(), "failed to acquire lock") {
		t.Fatalf("migrate() error = %v, want lock acquisition error", err)
	}
	if elapsed >= 3*time.Second {
		t.Fatalf("migrate() lock wait = %s, want less than 3s", elapsed)
	}
}

func TestMigrate_WorkloadConfigPushFields(t *testing.T) {
	db := openTestPostgres(t)
	assertColumns(t, db, "SELECT error_message, pushed_by FROM workload_configs LIMIT 0",
		"workload_configs missing push fields")
	assertColumns(t, db, "SELECT remote_config_status FROM workloads LIMIT 0",
		"workloads missing remote_config_status")
}

func TestRebind(t *testing.T) {
	query := "SELECT ?, '?', \"?\", -- ?\n ? /* ? */"
	if got, want := rebind(query), "SELECT $1, '?', \"?\", -- ?\n $2 /* ? */"; got != want {
		t.Fatalf("rebind() = %q, want %q", got, want)
	}
}

func TestQueryRowPostgresPreservesJSONBQuestionMarkOperator(t *testing.T) {
	db := openTestPostgres(t)
	var found bool
	err := db.QueryRowPostgres(
		`SELECT '{"enabled": true}'::jsonb ? $1`,
		"enabled",
	).Scan(&found)
	if err != nil || !found {
		t.Fatalf("native JSONB query: found=%v err=%v", found, err)
	}
}

func TestTxQueryRowPostgresPreservesNativePlaceholders(t *testing.T) {
	db := openTestPostgres(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })

	var found bool
	err = tx.QueryRowPostgres(
		`SELECT '{"enabled": true}'::jsonb ? $1`,
		"enabled",
	).Scan(&found)
	if err != nil || !found {
		t.Fatalf("native transaction query: found=%v err=%v", found, err)
	}
}

func TestQueryRowPostgresPreservesDollarQuotedStrings(t *testing.T) {
	db := openTestPostgres(t)
	var literal string
	var argument string
	err := db.QueryRowPostgres(
		`SELECT $body$?$body$, $1::text`,
		"native argument",
	).Scan(&literal, &argument)
	if err != nil {
		t.Fatalf("native dollar-quoted query: %v", err)
	}
	if literal != "?" || argument != "native argument" {
		t.Fatalf("native dollar-quoted query = (%q, %q), want (%q, %q)", literal, argument, "?", "native argument")
	}
}

func TestQueryRowPostgresPreservesEscapeStrings(t *testing.T) {
	db := openTestPostgres(t)
	var literal string
	var argument string
	err := db.QueryRowPostgres(
		`SELECT E'contains ?', $1::text`,
		"native argument",
	).Scan(&literal, &argument)
	if err != nil {
		t.Fatalf("native escape-string query: %v", err)
	}
	if literal != "contains ?" || argument != "native argument" {
		t.Fatalf("native escape-string query = (%q, %q), want (%q, %q)", literal, argument, "contains ?", "native argument")
	}
}

func openTestPostgres(t *testing.T) *DB {
	t.Helper()
	db, err := Open(testdb.New(t).DSN, PoolConfig{
		MaxOpenConns:    2,
		MaxIdleConns:    1,
		ConnMaxIdleTime: time.Minute,
		ConnMaxLifetime: time.Minute,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

func openUnmigratedTestPostgres(t *testing.T, dsn string) *DB {
	t.Helper()
	db, err := Open(dsn, testPoolConfig())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func assertColumns(t *testing.T, db *DB, query, msg string) {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("%s: %v", msg, err)
	}
	defer rows.Close()
}
