-- +goose Up
-- +goose StatementBegin
CREATE TABLE workload_known_good_configs (
    workload_id TEXT PRIMARY KEY REFERENCES workloads(id) ON DELETE CASCADE,
    config_id TEXT NOT NULL REFERENCES configs(id) ON DELETE RESTRICT,
    marked_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    marked_by TEXT,
    source_applied_at TIMESTAMP,
    replaced_config_id TEXT,
    replace_reason TEXT,
    CHECK (config_id <> '')
);

CREATE INDEX idx_workload_known_good_config_id
    ON workload_known_good_configs(config_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE workload_known_good_configs;
-- +goose StatementEnd
