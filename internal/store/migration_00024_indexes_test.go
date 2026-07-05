package store

import (
	"strings"
	"testing"
	"time"
)

func TestCompositeIndexesCoverFrequentStoreQueries(t *testing.T) {
	db := newTestDB(t)

	expected := map[string][]string{
		"idx_configs_created_at_id":                 {"created_at", "id"},
		"idx_workloads_active_display":              {"archived_at", "display_name", "id"},
		"idx_workload_configs_workload_status_time": {"workload_id", "status", "applied_at"},
		"idx_alerts_unresolved_fired_at":            {"resolved_at", "fired_at", "id"},
		"idx_alerts_workload_rule_resolved":         {"workload_id", "rule", "resolved_at"},
		"idx_workload_events_workload_time_id":      {"workload_id", "occurred_at", "id"},
	}
	for indexName, wantColumns := range expected {
		gotColumns := sqliteIndexColumns(t, db, indexName)
		if strings.Join(gotColumns, ",") != strings.Join(wantColumns, ",") {
			t.Fatalf("%s columns = %v, want %v", indexName, gotColumns, wantColumns)
		}
	}

	cases := []struct {
		name      string
		indexName string
		query     string
		args      []any
	}{
		{
			name:      "config list newest first",
			indexName: "idx_configs_created_at_id",
			query:     `SELECT id, name, content, created_at, created_by FROM configs ORDER BY created_at DESC`,
		},
		{
			name:      "active workload inventory",
			indexName: "idx_workloads_active_display",
			query: `SELECT id, fingerprint_source, fingerprint_keys, display_name, type, version, status,
				last_seen_at, labels, active_config_id, active_config_hash,
				remote_config_status, available_components, accepts_remote_config,
				retention_until, archived_at
				FROM workloads WHERE archived_at IS NULL ORDER BY display_name`,
		},
		{
			name:      "unresolved alert list",
			indexName: "idx_alerts_unresolved_fired_at",
			query:     `SELECT id, workload_id, rule, severity, message, fired_at, resolved_at FROM alerts WHERE resolved_at IS NULL ORDER BY fired_at DESC`,
		},
		{
			name:      "unresolved alert lookup by workload and rule",
			indexName: "idx_alerts_workload_rule_resolved",
			query:     `SELECT id, workload_id, rule, severity, message, fired_at, resolved_at FROM alerts WHERE workload_id = ? AND rule = ? AND resolved_at IS NULL LIMIT 1`,
			args:      []any{"wl-1", "workload_down"},
		},
		{
			name:      "latest pending config push",
			indexName: "idx_workload_configs_workload_status_time",
			query: `SELECT workload_id, config_id, applied_at, status,
				COALESCE(error_message, ''), COALESCE(pushed_by, ''), label
				FROM workload_configs WHERE workload_id = ? AND status = 'pending'
				ORDER BY applied_at DESC LIMIT 1`,
			args: []any{"wl-1"},
		},
		{
			name:      "latest applied config version",
			indexName: "idx_workload_configs_workload_status_time",
			query: `SELECT wc.workload_id, wc.config_id, wc.applied_at, wc.status,
				COALESCE(wc.error_message, ''), COALESCE(wc.pushed_by, ''),
				COALESCE(c.content, ''), wc.label
				FROM workload_configs wc
				LEFT JOIN configs c ON c.id = wc.config_id
				WHERE wc.workload_id = ? AND wc.status = 'applied'
				ORDER BY wc.applied_at DESC LIMIT 1`,
			args: []any{"wl-1"},
		},
		{
			name:      "workload version event history",
			indexName: "idx_workload_events_workload_time_id",
			query: `SELECT id, workload_id, instance_uid, pod_name, event_type, version, prev_version, occurred_at
				FROM workload_events
				WHERE workload_id = ? AND occurred_at > ?
				ORDER BY occurred_at DESC, id DESC LIMIT ?`,
			args: []any{"wl-1", time.Now().UTC().Add(-time.Hour), 50},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := sqliteQueryPlan(t, db, tc.query, tc.args...)
			if !strings.Contains(plan, tc.indexName) {
				t.Fatalf("query plan does not use %s:\n%s", tc.indexName, plan)
			}
			if strings.Contains(plan, "USE TEMP B-TREE FOR ORDER BY") {
				t.Fatalf("query plan sorts via temp b-tree despite %s:\n%s", tc.indexName, plan)
			}
		})
	}
}

func sqliteIndexColumns(t *testing.T, db *DB, indexName string) []string {
	t.Helper()

	rows, err := db.Query(`PRAGMA index_info(` + indexName + `)`)
	if err != nil {
		t.Fatalf("PRAGMA index_info(%s): %v", indexName, err)
	}
	//nolint:errcheck // test cleanup
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var seqno, cid int
		var name string
		if err := rows.Scan(&seqno, &cid, &name); err != nil {
			t.Fatalf("scan index_info(%s): %v", indexName, err)
		}
		cols = append(cols, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("index_info(%s): %v", indexName, err)
	}
	if len(cols) == 0 {
		t.Fatalf("index %s not found", indexName)
	}
	return cols
}

func sqliteQueryPlan(t *testing.T, db *DB, query string, args ...any) string {
	t.Helper()

	rows, err := db.Query(`EXPLAIN QUERY PLAN `+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	//nolint:errcheck // test cleanup
	defer rows.Close()

	var details []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan query plan: %v", err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("query plan rows: %v", err)
	}
	return strings.Join(details, "\n")
}
