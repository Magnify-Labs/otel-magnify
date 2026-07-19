package store

import (
	"context"
	"encoding/json"
	"io/fs"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/testdb"
	"github.com/magnify-labs/otel-magnify/pkg/models"
	"github.com/pressly/goose/v3"
)

func TestMigration00026SanitizesLegacyRemoteConfigStatusesOnce(t *testing.T) {
	ctx := context.Background()
	db := openUnmigratedTestPostgres(t, testdb.New(t).DSN)

	sqlMigrations, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("migrations fs: %v", err)
	}
	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		db.DB,
		sqlMigrations,
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		t.Fatalf("goose provider: %v", err)
	}
	if _, err := provider.UpTo(ctx, 25); err != nil {
		t.Fatalf("migrate SQL schema to version 25: %v", err)
	}

	updatedAt := time.Unix(42, 0).UTC()
	legacyJSON, err := json.Marshal(map[string]any{
		"status":        "failed",
		"config_hash":   "hash-a",
		"error_message": "collector failed: SECRET_TOKEN=abc123",
		"updated_at":    updatedAt.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("marshal legacy status: %v", err)
	}
	if _, err := db.ExecPostgres(`
		INSERT INTO workloads (id, type, status, remote_config_status)
		VALUES ($1, $2, $3, $4)
	`, "wl-legacy-migration-26", "collector", "connected", string(legacyJSON)); err != nil {
		t.Fatalf("seed legacy remote_config_status: %v", err)
	}

	if err := db.MigrateContext(ctx); err != nil {
		t.Fatalf("MigrateContext: %v", err)
	}
	assertGooseVersion(t, db, 26)

	var stored string
	if err := db.QueryRowPostgres(
		"SELECT remote_config_status FROM workloads WHERE id = $1",
		"wl-legacy-migration-26",
	).Scan(&stored); err != nil {
		t.Fatalf("query sanitized remote_config_status: %v", err)
	}
	assertNoSensitiveWorkloadStatusText(t, stored)

	var got models.RemoteConfigStatus
	if err := json.Unmarshal([]byte(stored), &got); err != nil {
		t.Fatalf("unmarshal sanitized remote_config_status: %v", err)
	}
	if got.Status != "failed" || got.ConfigHash != "hash-a" || !got.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("status metadata was corrupted: %+v", got)
	}
	if got.ErrorMessage != "Remote config error details redacted" {
		t.Fatalf("error_message = %q, want redacted summary", got.ErrorMessage)
	}

	if err := db.MigrateContext(ctx); err != nil {
		t.Fatalf("second MigrateContext: %v", err)
	}
	assertGooseVersion(t, db, 26)

	var storedAfterSecondMigration string
	if err := db.QueryRowPostgres(
		"SELECT remote_config_status FROM workloads WHERE id = $1",
		"wl-legacy-migration-26",
	).Scan(&storedAfterSecondMigration); err != nil {
		t.Fatalf("query remote_config_status after second migration: %v", err)
	}
	if storedAfterSecondMigration != stored {
		t.Fatalf("second migration changed remote_config_status: got %q, want %q", storedAfterSecondMigration, stored)
	}
}

func assertGooseVersion(t *testing.T, db *DB, want int64) {
	t.Helper()
	var got int64
	if err := db.QueryRowPostgres(`
		SELECT COALESCE(MAX(version_id), 0)
		FROM goose_db_version
		WHERE is_applied
	`).Scan(&got); err != nil {
		t.Fatalf("query goose version: %v", err)
	}
	if got != want {
		t.Fatalf("goose version = %d, want %d", got, want)
	}
}
