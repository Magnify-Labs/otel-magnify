-- +goose Up
-- +goose StatementBegin
CREATE TABLE workload_configs_new (
    workload_id              TEXT NOT NULL REFERENCES workloads(id) ON DELETE CASCADE,
    config_id                TEXT NOT NULL REFERENCES configs(id),
    applied_at               TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status                   TEXT NOT NULL DEFAULT 'submitted' CHECK (status IN ('pending','submitted','sent','applying','applied','failed','rollback_started','rollback_applied','rollback_failed')),
    error_message            TEXT,
    pushed_by                TEXT,
    label                    TEXT,
    push_id                  TEXT NOT NULL DEFAULT '',
    submitted_at             TIMESTAMP,
    sent_at                  TIMESTAMP,
    opamp_status_timeout_at  TIMESTAMP,
    rollback_of_push_id      TEXT NOT NULL DEFAULT '',
    instance_statuses        TEXT NOT NULL DEFAULT '[]',
    PRIMARY KEY (workload_id, config_id, applied_at)
);
INSERT INTO workload_configs_new (workload_id, config_id, applied_at, status, error_message, pushed_by, label,
                                  submitted_at)
SELECT workload_id, config_id, applied_at,
       CASE WHEN status = 'pending' THEN 'submitted' ELSE status END,
       error_message, pushed_by, label, applied_at
FROM workload_configs;
DROP TABLE workload_configs;
ALTER TABLE workload_configs_new RENAME TO workload_configs;
CREATE INDEX idx_workload_configs_workload_time
    ON workload_configs(workload_id, applied_at DESC);
CREATE INDEX idx_workload_configs_push_id
    ON workload_configs(push_id)
    WHERE push_id <> '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_workload_configs_push_id;
CREATE TABLE workload_configs_old (
    workload_id   TEXT NOT NULL REFERENCES workloads(id) ON DELETE CASCADE,
    config_id     TEXT NOT NULL REFERENCES configs(id),
    applied_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','applying','applied','failed')),
    error_message TEXT,
    pushed_by     TEXT,
    label         TEXT,
    PRIMARY KEY (workload_id, config_id, applied_at)
);
INSERT INTO workload_configs_old (workload_id, config_id, applied_at, status, error_message, pushed_by, label)
SELECT workload_id, config_id, applied_at,
       CASE WHEN status IN ('submitted','sent','rollback_started') THEN 'pending'
            WHEN status = 'rollback_applied' THEN 'applied'
            WHEN status = 'rollback_failed' THEN 'failed'
            ELSE status END,
       error_message, pushed_by, label
FROM workload_configs;
DROP TABLE workload_configs;
ALTER TABLE workload_configs_old RENAME TO workload_configs;
CREATE INDEX idx_workload_configs_workload_time
    ON workload_configs(workload_id, applied_at DESC);
-- +goose StatementEnd
