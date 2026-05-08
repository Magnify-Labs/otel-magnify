package store

import (
	"database/sql"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// RecordWorkloadConfig appends an entry to the per-workload config history, defaulting AppliedAt to now.
func (d *DB) RecordWorkloadConfig(wc models.WorkloadConfig) error {
	t := wc.AppliedAt
	if t.IsZero() {
		t = time.Now().UTC()
	}
	_, err := d.Exec(`
		INSERT INTO workload_configs (workload_id, config_id, applied_at, status, error_message, pushed_by)
		VALUES (?, ?, ?, ?, ?, ?)`,
		wc.WorkloadID, wc.ConfigID, t, wc.Status, nullIfEmpty(wc.ErrorMessage), nullIfEmpty(wc.PushedBy),
	)
	return err
}

// UpdateWorkloadConfigStatus updates status and error_message on the latest workload_configs row for the given (workload, config) pair.
func (d *DB) UpdateWorkloadConfigStatus(workloadID, configID, status, errorMessage string) error {
	_, err := d.Exec(`
		UPDATE workload_configs SET status = ?, error_message = ?
		WHERE workload_id = ? AND config_id = ?
		  AND applied_at = (
		    SELECT MAX(applied_at) FROM workload_configs WHERE workload_id = ? AND config_id = ?
		  )`,
		status, nullIfEmpty(errorMessage), workloadID, configID, workloadID, configID,
	)
	return err
}

// GetLatestPendingWorkloadConfig returns the most recent still-pending push for the workload, or (nil, nil) if there is none.
func (d *DB) GetLatestPendingWorkloadConfig(workloadID string) (*models.WorkloadConfig, error) {
	var (
		wc    models.WorkloadConfig
		label sql.NullString
	)
	err := d.QueryRow(`
		SELECT workload_id, config_id, applied_at, status,
		       COALESCE(error_message, ''), COALESCE(pushed_by, ''), label
		FROM workload_configs WHERE workload_id = ? AND status = 'pending'
		ORDER BY applied_at DESC LIMIT 1`, workloadID,
	).Scan(&wc.WorkloadID, &wc.ConfigID, &wc.AppliedAt, &wc.Status, &wc.ErrorMessage, &wc.PushedBy, &label)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if label.Valid {
		v := label.String
		wc.Label = &v
	}
	return &wc, nil
}

// GetWorkloadConfigHistory returns the full push history for a workload, joined with the config content, ordered newest first.
func (d *DB) GetWorkloadConfigHistory(workloadID string) ([]models.WorkloadConfig, error) {
	rows, err := d.Query(`
		SELECT wc.workload_id, wc.config_id, wc.applied_at, wc.status,
		       COALESCE(wc.error_message, ''), COALESCE(wc.pushed_by, ''),
		       COALESCE(c.content, ''), wc.label
		FROM workload_configs wc
		LEFT JOIN configs c ON c.id = wc.config_id
		WHERE wc.workload_id = ?
		ORDER BY wc.applied_at DESC`, workloadID)
	if err != nil {
		return nil, err
	}
	//nolint:errcheck // deferred cleanup; rows fully iterated below
	defer rows.Close()

	var history []models.WorkloadConfig
	for rows.Next() {
		var (
			wc    models.WorkloadConfig
			label sql.NullString
		)
		if err := rows.Scan(&wc.WorkloadID, &wc.ConfigID, &wc.AppliedAt, &wc.Status,
			&wc.ErrorMessage, &wc.PushedBy, &wc.Content, &label); err != nil {
			return nil, err
		}
		if label.Valid {
			v := label.String
			wc.Label = &v
		}
		history = append(history, wc)
	}
	return history, rows.Err()
}

// GetPushActivity returns a time series of push counts per calendar day (UTC)
// covering the last `days` days, oldest first. Missing days are filled with
// zero. The bucketing is done in Go so the SQL stays portable across SQLite
// and Postgres.
func (d *DB) GetPushActivity(days int) ([]models.PushActivityPoint, error) {
	if days <= 0 {
		return []models.PushActivityPoint{}, nil
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days+1)
	startDay := time.Date(cutoff.Year(), cutoff.Month(), cutoff.Day(), 0, 0, 0, 0, time.UTC)

	rows, err := d.Query(`SELECT applied_at FROM workload_configs WHERE applied_at >= ?`, startDay)
	if err != nil {
		return nil, err
	}
	//nolint:errcheck // deferred cleanup; rows fully iterated below
	defer rows.Close()

	counts := make(map[string]int, days)
	for rows.Next() {
		var t time.Time
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		counts[t.UTC().Format("2006-01-02")]++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]models.PushActivityPoint, days)
	for i := 0; i < days; i++ {
		day := startDay.AddDate(0, 0, i).Format("2006-01-02")
		out[i] = models.PushActivityPoint{Day: day, Count: counts[day]}
	}
	return out, nil
}

// GetLastAppliedWorkloadConfig returns the most recent successfully-applied config for a workload, or (nil, nil) if none has applied yet.
func (d *DB) GetLastAppliedWorkloadConfig(workloadID string) (*models.WorkloadConfig, error) {
	var (
		wc    models.WorkloadConfig
		label sql.NullString
	)
	err := d.QueryRow(`
		SELECT wc.workload_id, wc.config_id, wc.applied_at, wc.status,
		       COALESCE(wc.error_message, ''), COALESCE(wc.pushed_by, ''),
		       COALESCE(c.content, ''), wc.label
		FROM workload_configs wc
		LEFT JOIN configs c ON c.id = wc.config_id
		WHERE wc.workload_id = ? AND wc.status = 'applied'
		ORDER BY wc.applied_at DESC LIMIT 1`, workloadID,
	).Scan(&wc.WorkloadID, &wc.ConfigID, &wc.AppliedAt, &wc.Status,
		&wc.ErrorMessage, &wc.PushedBy, &wc.Content, &label)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if label.Valid {
		v := label.String
		wc.Label = &v
	}
	return &wc, nil
}

// SetWorkloadConfigLabel attaches a user-facing label to every history row
// matching (workloadID, hash). Passing an empty label clears any existing
// value (stored as SQL NULL). Returns sql.ErrNoRows when no row matches —
// the caller can surface a 404 to the user.
func (d *DB) SetWorkloadConfigLabel(workloadID, hash, label string) error {
	res, err := d.Exec(
		`UPDATE workload_configs SET label = ? WHERE workload_id = ? AND config_id = ?`,
		nullIfEmpty(label), workloadID, hash,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetWorkloadConfigByHash returns the most recent push of the given hash to
// the workload, joined with the config content. Returns (nil, nil) when no
// row matches so the handler can surface a clean 404.
func (d *DB) GetWorkloadConfigByHash(workloadID, hash string) (*models.WorkloadConfig, error) {
	var (
		wc    models.WorkloadConfig
		label sql.NullString
	)
	err := d.QueryRow(`
		SELECT wc.workload_id, wc.config_id, wc.applied_at, wc.status,
		       COALESCE(wc.error_message, ''), COALESCE(wc.pushed_by, ''),
		       COALESCE(c.content, ''), wc.label
		FROM workload_configs wc
		LEFT JOIN configs c ON c.id = wc.config_id
		WHERE wc.workload_id = ? AND wc.config_id = ?
		ORDER BY wc.applied_at DESC LIMIT 1`, workloadID, hash,
	).Scan(&wc.WorkloadID, &wc.ConfigID, &wc.AppliedAt, &wc.Status,
		&wc.ErrorMessage, &wc.PushedBy, &wc.Content, &label)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if label.Valid {
		v := label.String
		wc.Label = &v
	}
	return &wc, nil
}
