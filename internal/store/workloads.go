package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// UpsertWorkload inserts or updates a workload row, preserving previous remote_config_status / available_components when the new value is null.
func (d *DB) UpsertWorkload(w models.Workload) error {
	labelsJSON, err := w.Labels.Value()
	if err != nil {
		return fmt.Errorf("marshal labels: %w", err)
	}
	keysJSON, err := w.FingerprintKeys.Value()
	if err != nil {
		return fmt.Errorf("marshal fingerprint_keys: %w", err)
	}
	var statusJSON any
	if w.RemoteConfigStatus != nil {
		status := w.RemoteConfigStatus.Sanitized()
		s, err := status.Value()
		if err != nil {
			return fmt.Errorf("marshal remote_config_status: %w", err)
		}
		statusJSON = s
	}
	var componentsJSON any
	if w.AvailableComponents != nil {
		c, err := w.AvailableComponents.Value()
		if err != nil {
			return fmt.Errorf("marshal available_components: %w", err)
		}
		componentsJSON = c
	}
	fingerprintSource := w.FingerprintSource
	if fingerprintSource == "" {
		fingerprintSource = "uid"
	}
	_, err = d.Exec(`
		INSERT INTO workloads (
			id, fingerprint_source, fingerprint_keys, display_name, type, version, status,
			last_seen_at, labels, active_config_id, active_config_hash,
			remote_config_status, available_components, accepts_remote_config,
			retention_until, archived_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			fingerprint_source     = excluded.fingerprint_source,
			fingerprint_keys       = excluded.fingerprint_keys,
			display_name           = excluded.display_name,
			type                   = excluded.type,
			version                = excluded.version,
			status                 = excluded.status,
			last_seen_at           = excluded.last_seen_at,
			labels                 = excluded.labels,
			active_config_id       = excluded.active_config_id,
			active_config_hash     = excluded.active_config_hash,
			remote_config_status   = COALESCE(excluded.remote_config_status, workloads.remote_config_status),
			available_components   = COALESCE(excluded.available_components, workloads.available_components),
			accepts_remote_config  = excluded.accepts_remote_config,
			retention_until        = excluded.retention_until,
			archived_at            = excluded.archived_at
	`,
		w.ID, fingerprintSource, keysJSON, w.DisplayName, w.Type, w.Version, w.Status,
		w.LastSeenAt.UTC(), labelsJSON, w.ActiveConfigID, w.ActiveConfigHash,
		statusJSON, componentsJSON, w.AcceptsRemoteConfig,
		w.RetentionUntil, w.ArchivedAt,
	)
	return err
}

// GetWorkload fetches a workload by id, decoding the JSON-stored labels, fingerprint keys, remote config status, and available components.
func (d *DB) GetWorkload(id string) (models.Workload, error) {
	var w models.Workload
	var labelsJSON, keysJSON string
	var statusJSON, componentsJSON sql.NullString
	var retention, archived sql.NullTime
	err := d.QueryRow(`
		SELECT id, fingerprint_source, fingerprint_keys, display_name, type, version, status,
		       last_seen_at, labels, active_config_id, active_config_hash,
		       remote_config_status, available_components, accepts_remote_config,
		       retention_until, archived_at
		FROM workloads WHERE id = ?`, id,
	).Scan(
		&w.ID, &w.FingerprintSource, &keysJSON, &w.DisplayName, &w.Type, &w.Version, &w.Status,
		&w.LastSeenAt, &labelsJSON, &w.ActiveConfigID, &w.ActiveConfigHash,
		&statusJSON, &componentsJSON, &w.AcceptsRemoteConfig,
		&retention, &archived,
	)
	if err != nil {
		return w, fmt.Errorf("get workload %s: %w", id, err)
	}
	if err := w.Labels.Scan(labelsJSON); err != nil {
		return w, fmt.Errorf("scan labels: %w", err)
	}
	if err := w.FingerprintKeys.Scan(keysJSON); err != nil {
		return w, fmt.Errorf("scan fingerprint_keys: %w", err)
	}
	if statusJSON.Valid && statusJSON.String != "" {
		w.RemoteConfigStatus = &models.RemoteConfigStatus{}
		if err := w.RemoteConfigStatus.Scan(statusJSON.String); err != nil {
			return w, err
		}
	}
	if componentsJSON.Valid && componentsJSON.String != "" {
		w.AvailableComponents = &models.AvailableComponents{}
		if err := w.AvailableComponents.Scan(componentsJSON.String); err != nil {
			return w, err
		}
	}
	if retention.Valid {
		t := retention.Time.UTC()
		w.RetentionUntil = &t
	}
	if archived.Valid {
		t := archived.Time.UTC()
		w.ArchivedAt = &t
	}
	return w, nil
}

// ListWorkloads returns all workloads ordered by display_name; archived rows are excluded unless includeArchived is true.
func (d *DB) ListWorkloads(includeArchived bool) ([]models.Workload, error) {
	q := `SELECT id, fingerprint_source, fingerprint_keys, display_name, type, version, status,
	             last_seen_at, labels, active_config_id, active_config_hash,
	             remote_config_status, available_components, accepts_remote_config,
	             retention_until, archived_at
	      FROM workloads`
	if !includeArchived {
		q += ` WHERE archived_at IS NULL`
	}
	q += ` ORDER BY display_name`

	rows, err := d.Query(q)
	if err != nil {
		return nil, err
	}
	//nolint:errcheck // deferred cleanup; rows fully iterated below
	defer rows.Close()

	var out []models.Workload
	for rows.Next() {
		var w models.Workload
		var labelsJSON, keysJSON string
		var statusJSON, componentsJSON sql.NullString
		var retention, archived sql.NullTime
		if err := rows.Scan(
			&w.ID, &w.FingerprintSource, &keysJSON, &w.DisplayName, &w.Type, &w.Version, &w.Status,
			&w.LastSeenAt, &labelsJSON, &w.ActiveConfigID, &w.ActiveConfigHash,
			&statusJSON, &componentsJSON, &w.AcceptsRemoteConfig,
			&retention, &archived,
		); err != nil {
			return nil, err
		}
		if err := w.Labels.Scan(labelsJSON); err != nil {
			return nil, err
		}
		if err := w.FingerprintKeys.Scan(keysJSON); err != nil {
			return nil, err
		}
		if statusJSON.Valid && statusJSON.String != "" {
			w.RemoteConfigStatus = &models.RemoteConfigStatus{}
			if err := w.RemoteConfigStatus.Scan(statusJSON.String); err != nil {
				return nil, err
			}
		}
		if componentsJSON.Valid && componentsJSON.String != "" {
			w.AvailableComponents = &models.AvailableComponents{}
			if err := w.AvailableComponents.Scan(componentsJSON.String); err != nil {
				return nil, err
			}
		}
		if retention.Valid {
			t := retention.Time.UTC()
			w.RetentionUntil = &t
		}
		if archived.Valid {
			t := archived.Time.UTC()
			w.ArchivedAt = &t
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// MarkWorkloadDisconnected flips a workload to "disconnected" and stamps its retention deadline; returns sql.ErrNoRows when no row matches.
func (d *DB) MarkWorkloadDisconnected(id string, retentionUntil time.Time) error {
	res, err := d.Exec(`UPDATE workloads SET status = 'disconnected', retention_until = ? WHERE id = ?`,
		retentionUntil.UTC(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ClearWorkloadRetention nulls retention_until for the given workload, called when the workload reconnects.
func (d *DB) ClearWorkloadRetention(id string) error {
	_, err := d.Exec(`UPDATE workloads SET retention_until = NULL WHERE id = ?`, id)
	return err
}

// ArchiveExpiredWorkloads stamps archived_at on workloads whose retention deadline has passed, returning the row count.
func (d *DB) ArchiveExpiredWorkloads(now time.Time) (int64, error) {
	res, err := d.Exec(`UPDATE workloads
	                    SET archived_at = ?
	                    WHERE archived_at IS NULL
	                      AND retention_until IS NOT NULL
	                      AND retention_until < ?`,
		now.UTC(), now.UTC())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteWorkload permanently removes a workload row.
func (d *DB) DeleteWorkload(id string) error {
	_, err := d.Exec(`DELETE FROM workloads WHERE id = ?`, id)
	return err
}
