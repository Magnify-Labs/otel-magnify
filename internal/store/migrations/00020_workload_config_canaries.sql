-- +goose Up
CREATE TABLE IF NOT EXISTS workload_config_canaries (
    id TEXT PRIMARY KEY,
    workload_id TEXT NOT NULL REFERENCES workloads(id) ON DELETE CASCADE,
    config_id TEXT NOT NULL REFERENCES configs(id) ON DELETE RESTRICT,
    status TEXT NOT NULL,
    selection TEXT NOT NULL DEFAULT '{}',
    targets TEXT NOT NULL DEFAULT '[]',
    stop_reasons TEXT NOT NULL DEFAULT '[]',
    actor TEXT,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    promoted_at TIMESTAMP NULL,
    aborted_at TIMESTAMP NULL,
    rolled_back_at TIMESTAMP NULL
);

CREATE INDEX IF NOT EXISTS idx_workload_config_canaries_workload_updated
    ON workload_config_canaries(workload_id, updated_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_workload_config_canaries_workload_updated;
DROP TABLE IF EXISTS workload_config_canaries;
