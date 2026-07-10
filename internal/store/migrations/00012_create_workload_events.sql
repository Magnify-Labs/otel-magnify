-- +goose Up
-- +goose StatementBegin
CREATE TABLE workload_events (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    workload_id  TEXT     NOT NULL REFERENCES workloads(id) ON DELETE CASCADE,
    instance_uid TEXT     NOT NULL,
    pod_name     TEXT     NOT NULL DEFAULT '',
    event_type   TEXT     NOT NULL CHECK (event_type IN ('connected','disconnected','version_changed')),
    version      TEXT     NOT NULL DEFAULT '',
    prev_version TEXT     NOT NULL DEFAULT '',
    occurred_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_workload_events_wl_time   ON workload_events(workload_id, occurred_at DESC);
CREATE INDEX idx_workload_events_retention ON workload_events(occurred_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE workload_events;
-- +goose StatementEnd
