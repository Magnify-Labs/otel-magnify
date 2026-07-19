package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// CreateCanaryStatus persists a new workload config canary workflow.
func (d *DB) CreateCanaryStatus(status models.CanaryStatus) error {
	if status.CreatedAt.IsZero() {
		status.CreatedAt = time.Now().UTC()
	}
	if status.UpdatedAt.IsZero() {
		status.UpdatedAt = status.CreatedAt
	}
	status.Recount()
	selectionJSON, err := json.Marshal(status.Selection)
	if err != nil {
		return err
	}
	targetsJSON, err := json.Marshal(status.Targets)
	if err != nil {
		return err
	}
	reasonsJSON, err := json.Marshal(status.StopReasons)
	if err != nil {
		return err
	}
	_, err = d.Exec(`INSERT INTO workload_config_canaries
		(id, workload_id, config_id, status, selection, targets, stop_reasons, actor, created_at, updated_at, promoted_at, aborted_at, rolled_back_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		status.ID, status.WorkloadID, status.ConfigHash, status.Status, string(selectionJSON), string(targetsJSON), string(reasonsJSON),
		nullIfEmpty(status.Actor), status.CreatedAt.UTC(), status.UpdatedAt.UTC(), status.PromotedAt, status.AbortedAt, status.RolledBackAt)
	return err
}

// UpdateCanaryStatus replaces the mutable status fields for a canary workflow.
func (d *DB) UpdateCanaryStatus(status models.CanaryStatus) error {
	return updateCanaryStatus(d, status)
}

type canaryStatusExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func updateCanaryStatus(execer canaryStatusExecer, status models.CanaryStatus) error {
	if status.UpdatedAt.IsZero() {
		status.UpdatedAt = time.Now().UTC()
	}
	status.Recount()
	selectionJSON, err := json.Marshal(status.Selection)
	if err != nil {
		return err
	}
	targetsJSON, err := json.Marshal(status.Targets)
	if err != nil {
		return err
	}
	reasonsJSON, err := json.Marshal(status.StopReasons)
	if err != nil {
		return err
	}
	_, err = execer.Exec(`UPDATE workload_config_canaries
		SET status = ?, selection = ?, targets = ?, stop_reasons = ?, actor = ?, updated_at = ?, promoted_at = ?, aborted_at = ?, rolled_back_at = ?
		WHERE id = ? AND workload_id = ?`,
		status.Status, string(selectionJSON), string(targetsJSON), string(reasonsJSON), nullIfEmpty(status.Actor), status.UpdatedAt.UTC(),
		status.PromotedAt, status.AbortedAt, status.RolledBackAt, status.ID, status.WorkloadID)
	return err
}

// GetCanaryStatus returns one canary workflow for a workload, or nil when missing.
func (d *DB) GetCanaryStatus(workloadID, canaryID string) (*models.CanaryStatus, error) {
	status, err := d.scanCanary(`SELECT id, workload_id, config_id, status, selection, targets, stop_reasons, COALESCE(actor,''), created_at, updated_at, promoted_at, aborted_at, rolled_back_at
		FROM workload_config_canaries WHERE workload_id = ? AND id = ?`, workloadID, canaryID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return status, err
}

// UpdateCanaryTargetStatus reconciles OpAMP remote-config status into active canaries.
func (d *DB) UpdateCanaryTargetStatus(workloadID, configID, instanceUID, status string, updatedAt time.Time) error {
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	canaries, err := activeCanariesForConfig(tx, workloadID, configID)
	if err != nil {
		return err
	}
	for _, canary := range canaries {
		changed := false
		for i := range canary.Targets {
			if canary.Targets[i].InstanceUID != instanceUID {
				continue
			}
			canary.Targets[i].Status = status
			canary.Targets[i].UpdatedAt = updatedAt.UTC()
			if status == models.PushStatusFailed {
				canary.Targets[i].StopReason = models.CanaryStopRemoteConfigFailed
			} else {
				canary.Targets[i].StopReason = ""
			}
			changed = true
			break
		}
		if !changed {
			continue
		}
		canary.UpdatedAt = updatedAt.UTC()
		canary.Recount()
		switch {
		case canary.Counts.Failed > 0:
			canary.Status = models.CanaryStatusStopped
		case len(canary.Targets) > 0 && canary.Counts.Applied == len(canary.Targets):
			canary.Status = models.CanaryStatusSucceeded
		default:
			canary.Status = models.CanaryStatusRunning
		}
		if err := updateCanaryStatus(tx, *canary); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func activeCanariesForConfig(tx *Tx, workloadID, configID string) ([]*models.CanaryStatus, error) {
	rows, err := tx.Query(`SELECT id, workload_id, config_id, status, selection, targets, stop_reasons, COALESCE(actor,''), created_at, updated_at, promoted_at, aborted_at, rolled_back_at
		FROM workload_config_canaries
		WHERE workload_id = ? AND config_id = ? AND status IN (?, ?)
		ORDER BY id
		FOR UPDATE`, workloadID, configID, models.CanaryStatusRunning, models.CanaryStatusSucceeded)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var canaries []*models.CanaryStatus
	for rows.Next() {
		canary, err := scanCanaryRows(rows)
		if err != nil {
			return nil, err
		}
		canaries = append(canaries, canary)
	}
	return canaries, rows.Err()
}

func (d *DB) scanCanary(query string, args ...any) (*models.CanaryStatus, error) {
	return scanCanaryRows(d.QueryRow(query, args...))
}

type canaryScanner interface {
	Scan(dest ...any) error
}

func scanCanaryRows(scanner canaryScanner) (*models.CanaryStatus, error) {
	var status models.CanaryStatus
	var selectionJSON, targetsJSON, reasonsJSON string
	var promotedAt, abortedAt, rolledBackAt sql.NullTime
	err := scanner.Scan(&status.ID, &status.WorkloadID, &status.ConfigHash, &status.Status, &selectionJSON, &targetsJSON, &reasonsJSON, &status.Actor, &status.CreatedAt, &status.UpdatedAt, &promotedAt, &abortedAt, &rolledBackAt)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(selectionJSON), &status.Selection); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(targetsJSON), &status.Targets); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(reasonsJSON), &status.StopReasons)
	if promotedAt.Valid {
		t := promotedAt.Time.UTC()
		status.PromotedAt = &t
	}
	if abortedAt.Valid {
		t := abortedAt.Time.UTC()
		status.AbortedAt = &t
	}
	if rolledBackAt.Valid {
		t := rolledBackAt.Time.UTC()
		status.RolledBackAt = &t
	}
	status.Recount()
	return &status, nil
}
