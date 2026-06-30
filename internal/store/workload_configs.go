package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// RecordWorkloadConfig appends an entry to the per-workload config history, defaulting AppliedAt/SubmittedAt to now.
func (d *DB) RecordWorkloadConfig(wc models.WorkloadConfig) error {
	t := wc.AppliedAt
	if t.IsZero() {
		t = time.Now().UTC()
	}
	submittedAt := wc.SubmittedAt
	if submittedAt.IsZero() {
		submittedAt = t
	}
	if wc.Status == "" || wc.Status == "pending" {
		wc.Status = models.PushStatusSubmitted
	}
	if wc.OpAMPStatusTimeoutAt == nil {
		timeoutAt := submittedAt.Add(30 * time.Second)
		wc.OpAMPStatusTimeoutAt = &timeoutAt
	}
	instancesJSON, err := json.Marshal(wc.InstanceStatuses)
	if err != nil {
		return err
	}
	_, err = d.Exec(`
		INSERT INTO workload_configs (workload_id, config_id, applied_at, status, error_message, pushed_by,
		                              push_id, submitted_at, sent_at, opamp_status_timeout_at, rollback_of_push_id, instance_statuses)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		wc.WorkloadID, wc.ConfigID, t, wc.Status, nullIfEmpty(wc.ErrorMessage), nullIfEmpty(wc.PushedBy),
		wc.PushID, submittedAt, wc.SentAt, wc.OpAMPStatusTimeoutAt, wc.RollbackOfPushID, string(instancesJSON),
	)
	return err
}

// MarkWorkloadConfigSent records that the latest workload/config push was sent through OpAMP.
func (d *DB) MarkWorkloadConfigSent(workloadID, configID string, sentAt time.Time) error {
	_, err := d.Exec(`
		UPDATE workload_configs SET status = ?, sent_at = ?
		WHERE workload_id = ? AND config_id = ?
		  AND applied_at = (
		    SELECT MAX(applied_at) FROM workload_configs WHERE workload_id = ? AND config_id = ?
		  )`,
		models.PushStatusSent, sentAt.UTC(), workloadID, configID, workloadID, configID,
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

// UpdateWorkloadConfigInstanceStatus merges a single instance's remote status into the latest push row.
func (d *DB) UpdateWorkloadConfigInstanceStatus(workloadID, configID, instanceUID, status, errorMessage string, updatedAt time.Time) error {
	wc, err := d.GetLatestWorkloadConfigByHash(workloadID, configID)
	if err != nil || wc == nil {
		return err
	}
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	found := false
	for i := range wc.InstanceStatuses {
		if wc.InstanceStatuses[i].InstanceUID == instanceUID {
			wc.InstanceStatuses[i].Status = status
			wc.InstanceStatuses[i].ConfigHash = configID
			wc.InstanceStatuses[i].UpdatedAt = updatedAt.UTC()
			wc.InstanceStatuses[i].ErrorMessage = errorMessage
			wc.InstanceStatuses[i].ErrorCause = ""
			found = true
			break
		}
	}
	if !found {
		wc.InstanceStatuses = append(wc.InstanceStatuses, models.WorkloadConfigInstanceStatus{
			InstanceUID:  instanceUID,
			Required:     true,
			Status:       status,
			ConfigHash:   configID,
			UpdatedAt:    updatedAt.UTC(),
			ErrorMessage: errorMessage,
		})
	}
	wc.HydratePushStatus(updatedAt.UTC())
	instancesJSON, err := json.Marshal(wc.InstanceStatuses)
	if err != nil {
		return err
	}
	_, err = d.Exec(`
		UPDATE workload_configs SET status = ?, error_message = ?, instance_statuses = ?
		WHERE workload_id = ? AND config_id = ?
		  AND applied_at = (
		    SELECT MAX(applied_at) FROM workload_configs WHERE workload_id = ? AND config_id = ?
		  )`,
		wc.Status, nullIfEmpty(errorMessage), string(instancesJSON), workloadID, configID, workloadID, configID,
	)
	return err
}

// GetLatestPendingWorkloadConfig returns the most recent still-in-flight push for the workload, or (nil, nil) if there is none.
func (d *DB) GetLatestPendingWorkloadConfig(workloadID string) (*models.WorkloadConfig, error) {
	wc, err := d.getOneWorkloadConfig(`
		SELECT wc.workload_id, wc.config_id, wc.applied_at, wc.status,
		       COALESCE(wc.error_message, ''), COALESCE(wc.pushed_by, ''), COALESCE(c.content, ''), wc.label,
		       COALESCE(wc.push_id, ''), COALESCE(wc.submitted_at, wc.applied_at), wc.sent_at, wc.opamp_status_timeout_at,
		       COALESCE(wc.rollback_of_push_id, ''), COALESCE(wc.instance_statuses, '[]')
		FROM workload_configs wc
		LEFT JOIN configs c ON c.id = wc.config_id
		WHERE wc.workload_id = ? AND wc.status IN ('pending','submitted','sent','applying','rollback_started')
		ORDER BY wc.applied_at DESC LIMIT 1`, workloadID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return wc, err
}

// GetLatestWorkloadConfig returns the most recent push row for a workload.
func (d *DB) GetLatestWorkloadConfig(workloadID string) (*models.WorkloadConfig, error) {
	wc, err := d.getOneWorkloadConfig(`
		SELECT wc.workload_id, wc.config_id, wc.applied_at, wc.status,
		       COALESCE(wc.error_message, ''), COALESCE(wc.pushed_by, ''), COALESCE(c.content, ''), wc.label,
		       COALESCE(wc.push_id, ''), COALESCE(wc.submitted_at, wc.applied_at), wc.sent_at, wc.opamp_status_timeout_at,
		       COALESCE(wc.rollback_of_push_id, ''), COALESCE(wc.instance_statuses, '[]')
		FROM workload_configs wc
		LEFT JOIN configs c ON c.id = wc.config_id
		WHERE wc.workload_id = ?
		ORDER BY wc.applied_at DESC LIMIT 1`, workloadID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return wc, err
}

// GetLatestWorkloadConfigByHash returns the most recent push of configID for a workload.
func (d *DB) GetLatestWorkloadConfigByHash(workloadID, configID string) (*models.WorkloadConfig, error) {
	wc, err := d.getOneWorkloadConfig(`
		SELECT wc.workload_id, wc.config_id, wc.applied_at, wc.status,
		       COALESCE(wc.error_message, ''), COALESCE(wc.pushed_by, ''), COALESCE(c.content, ''), wc.label,
		       COALESCE(wc.push_id, ''), COALESCE(wc.submitted_at, wc.applied_at), wc.sent_at, wc.opamp_status_timeout_at,
		       COALESCE(wc.rollback_of_push_id, ''), COALESCE(wc.instance_statuses, '[]')
		FROM workload_configs wc
		LEFT JOIN configs c ON c.id = wc.config_id
		WHERE wc.workload_id = ? AND wc.config_id = ?
		ORDER BY wc.applied_at DESC LIMIT 1`, workloadID, configID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return wc, err
}

// GetWorkloadConfigHistory returns the full push history for a workload, joined with the config content, ordered newest first.
func (d *DB) GetWorkloadConfigHistory(workloadID string) ([]models.WorkloadConfig, error) {
	rows, err := d.Query(`
		SELECT wc.workload_id, wc.config_id, wc.applied_at, wc.status,
		       COALESCE(wc.error_message, ''), COALESCE(wc.pushed_by, ''),
		       COALESCE(c.content, ''), wc.label,
		       COALESCE(wc.push_id, ''), COALESCE(wc.submitted_at, wc.applied_at), wc.sent_at, wc.opamp_status_timeout_at,
		       COALESCE(wc.rollback_of_push_id, ''), COALESCE(wc.instance_statuses, '[]')
		FROM workload_configs wc
		LEFT JOIN configs c ON c.id = wc.config_id
		WHERE wc.workload_id = ?
		ORDER BY wc.applied_at DESC`, workloadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var history []models.WorkloadConfig
	for rows.Next() {
		wc, err := scanWorkloadConfig(rows)
		if err != nil {
			return nil, err
		}
		history = append(history, wc)
	}
	return history, rows.Err()
}

func (d *DB) getOneWorkloadConfig(query string, args ...any) (*models.WorkloadConfig, error) {
	wc, err := scanWorkloadConfig(d.QueryRow(query, args...))
	if err != nil {
		return nil, err
	}
	return &wc, nil
}

type workloadConfigScanner interface{ Scan(dest ...any) error }

func scanWorkloadConfig(row workloadConfigScanner) (models.WorkloadConfig, error) {
	var (
		wc                               models.WorkloadConfig
		label                            sql.NullString
		pushID, rollbackOf, instancesRaw string
		submittedAt, sentAt, timeoutAt   nullableTime
	)
	if err := row.Scan(&wc.WorkloadID, &wc.ConfigID, &wc.AppliedAt, &wc.Status,
		&wc.ErrorMessage, &wc.PushedBy, &wc.Content, &label,
		&pushID, &submittedAt, &sentAt, &timeoutAt, &rollbackOf, &instancesRaw); err != nil {
		return wc, err
	}
	if label.Valid {
		v := label.String
		wc.Label = &v
	}
	wc.PushID = pushID
	if submittedAt.Valid {
		wc.SubmittedAt = submittedAt.Time
	}
	if sentAt.Valid {
		v := sentAt.Time
		wc.SentAt = &v
	}
	if timeoutAt.Valid {
		v := timeoutAt.Time
		wc.OpAMPStatusTimeoutAt = &v
	}
	wc.RollbackOfPushID = rollbackOf
	if instancesRaw != "" {
		_ = json.Unmarshal([]byte(instancesRaw), &wc.InstanceStatuses)
	}
	wc.UpdatedAt = wc.AppliedAt
	for _, inst := range wc.InstanceStatuses {
		if inst.UpdatedAt.After(wc.UpdatedAt) {
			wc.UpdatedAt = inst.UpdatedAt
		}
	}
	wc.HydratePushStatus(time.Now().UTC())
	return wc, nil
}

type nullableTime struct {
	Time  time.Time
	Valid bool
}

func (nt *nullableTime) Scan(src any) error {
	if src == nil {
		nt.Valid = false
		return nil
	}
	switch v := src.(type) {
	case time.Time:
		nt.Time, nt.Valid = v, true
		return nil
	case string:
		return nt.scanString(v)
	case []byte:
		return nt.scanString(string(v))
	default:
		return fmt.Errorf("unsupported time scan type %T", src)
	}
}

func (nt *nullableTime) scanString(v string) error {
	if v == "" {
		nt.Valid = false
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05 -0700 MST", "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, v); err == nil {
			nt.Time, nt.Valid = t, true
			return nil
		}
	}
	return fmt.Errorf("unsupported time format %q", v)
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
	defer rows.Close() //nolint:errcheck

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
	wc, err := d.getOneWorkloadConfig(`
		SELECT wc.workload_id, wc.config_id, wc.applied_at, wc.status,
		       COALESCE(wc.error_message, ''), COALESCE(wc.pushed_by, ''),
		       COALESCE(c.content, ''), wc.label,
		       COALESCE(wc.push_id, ''), COALESCE(wc.submitted_at, wc.applied_at), wc.sent_at, wc.opamp_status_timeout_at,
		       COALESCE(wc.rollback_of_push_id, ''), COALESCE(wc.instance_statuses, '[]')
		FROM workload_configs wc
		LEFT JOIN configs c ON c.id = wc.config_id
		WHERE wc.workload_id = ? AND wc.status = 'applied'
		ORDER BY wc.applied_at DESC LIMIT 1`, workloadID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return wc, err
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
	return d.GetLatestWorkloadConfigByHash(workloadID, hash)
}
