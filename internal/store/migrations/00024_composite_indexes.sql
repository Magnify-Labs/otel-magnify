-- +goose Up
-- +goose StatementBegin
CREATE INDEX idx_configs_created_at_id
    ON configs(created_at DESC, id);
CREATE INDEX idx_workloads_active_display
    ON workloads(archived_at, display_name, id);
CREATE INDEX idx_workload_configs_workload_status_time
    ON workload_configs(workload_id, status, applied_at DESC);
CREATE INDEX idx_alerts_unresolved_fired_at
    ON alerts(resolved_at, fired_at DESC, id);
CREATE INDEX idx_alerts_workload_rule_resolved
    ON alerts(workload_id, rule, resolved_at);
DROP INDEX IF EXISTS idx_workload_events_wl_time;
CREATE INDEX idx_workload_events_workload_time_id
    ON workload_events(workload_id, occurred_at DESC, id DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_workload_events_workload_time_id;
CREATE INDEX idx_workload_events_wl_time
    ON workload_events(workload_id, occurred_at DESC);
DROP INDEX IF EXISTS idx_alerts_workload_rule_resolved;
DROP INDEX IF EXISTS idx_alerts_unresolved_fired_at;
DROP INDEX IF EXISTS idx_workload_configs_workload_status_time;
DROP INDEX IF EXISTS idx_workloads_active_display;
DROP INDEX IF EXISTS idx_configs_created_at_id;
-- +goose StatementEnd
