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
	db, err := Open(testdb.New(t).DSN, testPoolConfig())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
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
