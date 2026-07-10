package store

import (
	"strings"
	"testing"
)

func TestCompositeIndexesCoverFrequentStoreQueries(t *testing.T) {
	db := newTestDB(t)

	expected := map[string]string{
		"idx_configs_created_at_id":                 "(created_at DESC, id)",
		"idx_workloads_active_display":              "(archived_at, display_name, id)",
		"idx_workload_configs_workload_status_time": "(workload_id, status, applied_at DESC)",
		"idx_alerts_unresolved_fired_at":            "(resolved_at, fired_at DESC, id)",
		"idx_alerts_workload_rule_resolved":         "(workload_id, rule, resolved_at)",
		"idx_workload_events_workload_time_id":      "(workload_id, occurred_at DESC, id DESC)",
	}
	for indexName, columns := range expected {
		var definition string
		if err := db.QueryRow(`
			SELECT indexdef FROM pg_indexes
			WHERE schemaname = current_schema() AND indexname = ?`, indexName,
		).Scan(&definition); err != nil {
			t.Fatalf("query definition for %s: %v", indexName, err)
		}
		if !strings.Contains(definition, columns) {
			t.Fatalf("%s definition = %q, want columns %q", indexName, definition, columns)
		}
	}
}
