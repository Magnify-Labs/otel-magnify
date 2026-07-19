package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/magnify-labs/otel-magnify/pkg/models"
	"github.com/pressly/goose/v3"
)

var migration00026 = goose.NewGoMigration(
	26,
	&goose.GoFunc{RunTx: sanitizeLegacyRemoteConfigStatusesTx},
	nil,
)

// sanitizeLegacyRemoteConfigStatusesTx rewrites already-persisted workload
// remote_config_status JSON through the canonical model sanitizer. It preserves
// sibling status metadata and never logs or returns raw pre-sanitized payloads.
func sanitizeLegacyRemoteConfigStatusesTx(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, remote_config_status FROM workloads WHERE remote_config_status IS NOT NULL AND remote_config_status <> ''`)
	if err != nil {
		return fmt.Errorf("query legacy remote_config_status rows: %w", err)
	}
	//nolint:errcheck // deferred cleanup; rows fully iterated below
	defer rows.Close()

	type statusRow struct {
		id     string
		stored string
	}
	var updates []statusRow
	for rows.Next() {
		var id string
		var stored string
		if err := rows.Scan(&id, &stored); err != nil {
			return fmt.Errorf("scan legacy remote_config_status row: %w", err)
		}

		var status models.RemoteConfigStatus
		if err := status.Scan(stored); err != nil {
			return fmt.Errorf("sanitize legacy remote_config_status for workload %s: %w", id, err)
		}
		sanitized, err := status.Value()
		if err != nil {
			return fmt.Errorf("marshal sanitized legacy remote_config_status for workload %s: %w", id, err)
		}
		if sanitized != stored {
			updates = append(updates, statusRow{id: id, stored: sanitized})
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate legacy remote_config_status rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close legacy remote_config_status rows: %w", err)
	}
	if len(updates) == 0 {
		return nil
	}

	for _, update := range updates {
		if _, err := tx.ExecContext(ctx, `UPDATE workloads SET remote_config_status = $1 WHERE id = $2`, update.stored, update.id); err != nil {
			return fmt.Errorf("update sanitized legacy remote_config_status for workload %s: %w", update.id, err)
		}
	}
	return nil
}
