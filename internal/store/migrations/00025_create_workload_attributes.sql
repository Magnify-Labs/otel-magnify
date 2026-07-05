-- +goose Up
-- +goose StatementBegin
-- DB-neutral projection table for workload JSON maps that need indexed lookup.
-- labels and fingerprint_keys remain the source of truth returned by the API;
-- new/upserted workload rows refresh this projection transactionally.
CREATE TABLE workload_attributes (
    workload_id TEXT NOT NULL REFERENCES workloads(id) ON DELETE CASCADE,
    source      TEXT NOT NULL CHECK (source IN ('fingerprint','label')),
    key         TEXT NOT NULL,
    value       TEXT NOT NULL,
    PRIMARY KEY (workload_id, source, key)
);

CREATE INDEX idx_workload_attributes_lookup
    ON workload_attributes(source, key, value, workload_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_workload_attributes_lookup;
DROP TABLE IF EXISTS workload_attributes;
-- +goose StatementEnd
