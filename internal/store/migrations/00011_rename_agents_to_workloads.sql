-- +goose Up
-- +goose StatementBegin
CREATE TABLE workloads (
    id                    TEXT PRIMARY KEY,
    fingerprint_source    TEXT NOT NULL DEFAULT 'uid' CHECK (fingerprint_source IN ('k8s','host','uid')),
    fingerprint_keys      TEXT NOT NULL DEFAULT '{}',
    display_name          TEXT NOT NULL DEFAULT '',
    type                  TEXT NOT NULL CHECK (type IN ('collector','sdk')),
    version               TEXT NOT NULL DEFAULT '',
    status                TEXT NOT NULL DEFAULT 'disconnected' CHECK (status IN ('connected','disconnected','degraded')),
    last_seen_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    labels                TEXT NOT NULL DEFAULT '{}',
    active_config_id      TEXT REFERENCES configs(id),
    active_config_hash    TEXT NOT NULL DEFAULT '',
    remote_config_status  TEXT,
    available_components  TEXT,
    accepts_remote_config BOOLEAN NOT NULL DEFAULT FALSE,
    retention_until       TIMESTAMP,
    archived_at           TIMESTAMP
);

INSERT INTO workloads (id, fingerprint_source, fingerprint_keys, display_name, type, version, status,
                       last_seen_at, labels, active_config_id, remote_config_status,
                       available_components, accepts_remote_config)
SELECT id, 'uid', '{}', display_name, type, version, status, last_seen_at, labels,
       active_config_id, remote_config_status, available_components, accepts_remote_config <> 0
FROM agents;

CREATE TABLE agent_configs_new (
    workload_id   TEXT NOT NULL REFERENCES workloads(id) ON DELETE CASCADE,
    config_id     TEXT NOT NULL REFERENCES configs(id),
    applied_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','applying','applied','failed')),
    error_message TEXT,
    pushed_by     TEXT,
    PRIMARY KEY (workload_id, config_id, applied_at)
);
INSERT INTO agent_configs_new (workload_id, config_id, applied_at, status, error_message, pushed_by)
SELECT agent_id, config_id, applied_at, status, error_message, pushed_by FROM agent_configs;
DROP TABLE agent_configs;
ALTER TABLE agent_configs_new RENAME TO workload_configs;

CREATE TABLE alerts_new (
    id            TEXT PRIMARY KEY,
    workload_id   TEXT NOT NULL REFERENCES workloads(id) ON DELETE CASCADE,
    rule          TEXT NOT NULL,
    severity      TEXT NOT NULL CHECK (severity IN ('warning', 'critical')),
    message       TEXT NOT NULL,
    fired_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    resolved_at   TIMESTAMP
);
INSERT INTO alerts_new (id, workload_id, rule, severity, message, fired_at, resolved_at)
SELECT id, agent_id, rule, severity, message, fired_at, resolved_at FROM alerts;
DROP TABLE alerts;
ALTER TABLE alerts_new RENAME TO alerts;

DROP TABLE agents;

CREATE INDEX idx_workloads_retention
    ON workloads(retention_until)
    WHERE archived_at IS NULL;
CREATE INDEX idx_workload_configs_workload_time
    ON workload_configs(workload_id, applied_at DESC);
CREATE INDEX idx_alerts_workload
    ON alerts(workload_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TABLE agents (
    id               TEXT PRIMARY KEY,
    display_name     TEXT NOT NULL DEFAULT '',
    type             TEXT NOT NULL CHECK (type IN ('collector','sdk')),
    version          TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'disconnected' CHECK (status IN ('connected','disconnected','degraded')),
    last_seen_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    labels           TEXT NOT NULL DEFAULT '{}',
    active_config_id TEXT REFERENCES configs(id),
    remote_config_status  TEXT,
    available_components  TEXT,
    accepts_remote_config INTEGER NOT NULL DEFAULT 0
);
INSERT INTO agents (id, display_name, type, version, status, last_seen_at, labels,
                    active_config_id, remote_config_status, available_components, accepts_remote_config)
SELECT id, display_name, type, version, status, last_seen_at, labels,
                    active_config_id, remote_config_status, available_components,
                    CASE WHEN accepts_remote_config THEN 1 ELSE 0 END
FROM workloads;

CREATE TABLE agent_configs_restore (
    agent_id      TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    config_id     TEXT NOT NULL REFERENCES configs(id),
    applied_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','applying','applied','failed')),
    error_message TEXT,
    pushed_by     TEXT,
    PRIMARY KEY (agent_id, config_id, applied_at)
);
INSERT INTO agent_configs_restore (agent_id, config_id, applied_at, status, error_message, pushed_by)
SELECT workload_id, config_id, applied_at, status, error_message, pushed_by FROM workload_configs;
DROP TABLE workload_configs;
ALTER TABLE agent_configs_restore RENAME TO agent_configs;

CREATE TABLE alerts_restore (
    id          TEXT PRIMARY KEY,
    agent_id    TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    rule        TEXT NOT NULL,
    severity    TEXT NOT NULL CHECK (severity IN ('warning', 'critical')),
    message     TEXT NOT NULL,
    fired_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    resolved_at TIMESTAMP
);
INSERT INTO alerts_restore (id, agent_id, rule, severity, message, fired_at, resolved_at)
SELECT id, workload_id, rule, severity, message, fired_at, resolved_at FROM alerts;
DROP TABLE alerts;
ALTER TABLE alerts_restore RENAME TO alerts;

DROP TABLE workloads;
-- +goose StatementEnd
