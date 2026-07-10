package store

import (
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// InsertWorkloadEvent appends a workload event row, defaulting OccurredAt to now, and returns the assigned auto-increment id.
func (d *DB) InsertWorkloadEvent(e models.WorkloadEvent) (int64, error) {
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	var id int64
	err := d.QueryRow(`
		INSERT INTO workload_events (workload_id, instance_uid, pod_name, event_type, version, prev_version, occurred_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		RETURNING id`,
		e.WorkloadID, e.InstanceUID, e.PodName, e.EventType, e.Version, e.PrevVersion, e.OccurredAt.UTC(),
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// ListWorkloadEvents returns the latest events for a workload (capped by limit), optionally filtered to events after the since timestamp.
func (d *DB) ListWorkloadEvents(workloadID string, limit int, since time.Time) ([]models.WorkloadEvent, error) {
	q := `SELECT id, workload_id, instance_uid, pod_name, event_type, version, prev_version, occurred_at
	      FROM workload_events
	      WHERE workload_id = ?`
	args := []any{workloadID}
	if !since.IsZero() {
		q += ` AND occurred_at > ?`
		args = append(args, since.UTC())
	}
	q += ` ORDER BY occurred_at DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	//nolint:errcheck // deferred cleanup; rows fully iterated below
	defer rows.Close()

	var out []models.WorkloadEvent
	for rows.Next() {
		var e models.WorkloadEvent
		if err := rows.Scan(&e.ID, &e.WorkloadID, &e.InstanceUID, &e.PodName, &e.EventType, &e.Version, &e.PrevVersion, &e.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PurgeOldWorkloadEvents deletes all events older than cutoff and returns the number of rows removed.
func (d *DB) PurgeOldWorkloadEvents(cutoff time.Time) (int64, error) {
	res, err := d.Exec(`DELETE FROM workload_events WHERE occurred_at < ?`, cutoff.UTC())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
