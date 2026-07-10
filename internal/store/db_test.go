package store

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/testdb"
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
		ConnMaxLifetime: time.Minute,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
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

func openTestPostgres(t *testing.T) *DB {
	t.Helper()
	db, err := Open(testdb.New(t).DSN, PoolConfig{
		MaxOpenConns:    2,
		MaxIdleConns:    1,
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

func assertColumns(t *testing.T, db *DB, query, msg string) {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("%s: %v", msg, err)
	}
	defer rows.Close()
}
