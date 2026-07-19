package store

import (
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/testdb"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// newTestDB returns a migrated PostgreSQL schema for tests.
func newTestDB(t *testing.T) *DB {
	t.Helper()
	return newTestDBWithPoolConfig(t, testPoolConfig())
}

func newTestDBWithPoolConfig(t *testing.T, poolConfig PoolConfig) *DB {
	t.Helper()
	db, err := Open(testdb.New(t).DSN, poolConfig)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func waitForPostgresTableLockWaiters(t *testing.T, db *DB, tableName string) {
	t.Helper()

	deadline := time.NewTimer(5 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()

	for {
		var blockedSessions int
		if err := db.QueryRow(`
			SELECT COUNT(*)
			FROM pg_stat_activity
			WHERE datname = current_database()
			  AND wait_event_type = 'Lock'
			  AND query LIKE ?`, "%"+tableName+"%").Scan(&blockedSessions); err != nil {
			t.Fatalf("count blocked PostgreSQL sessions: %v", err)
		}
		if blockedSessions >= 2 {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("timed out waiting for two PostgreSQL sessions blocked on %s; last count = %d", tableName, blockedSessions)
		case <-ticker.C:
		}
	}
}

func testPoolConfig() PoolConfig {
	return PoolConfig{
		MaxOpenConns:    2,
		MaxIdleConns:    1,
		ConnMaxLifetime: time.Minute,
	}
}

func seedWorkload(t *testing.T, db *DB, id string) {
	t.Helper()
	if err := db.UpsertWorkload(models.Workload{
		ID: id, Type: "collector", Status: "connected",
		LastSeenAt:      time.Now().UTC(),
		Labels:          models.Labels{},
		FingerprintKeys: models.FingerprintKeys{},
	}); err != nil {
		t.Fatal(err)
	}
}

func seedConfig(t *testing.T, db *DB, id, content string) {
	t.Helper()
	if err := db.CreateConfig(models.Config{
		ID: id, Name: "test-" + id, Content: content,
		CreatedAt: time.Now().UTC(), CreatedBy: "test",
	}); err != nil {
		t.Fatal(err)
	}
}
