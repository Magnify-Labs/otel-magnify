-- +goose Up
CREATE TABLE IF NOT EXISTS workload_config_approvals (
    id TEXT PRIMARY KEY,
    workload_id TEXT NOT NULL REFERENCES workloads(id) ON DELETE CASCADE,
    draft_yaml TEXT NOT NULL,
    target_group TEXT NOT NULL,
    target_env TEXT NOT NULL DEFAULT '',
    requester TEXT NOT NULL DEFAULT '',
    request_comment TEXT NOT NULL DEFAULT '',
    approver TEXT NOT NULL DEFAULT '',
    approval_comment TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    approved_by TEXT NULL,
    approved_at TIMESTAMP NULL,
    push_comment TEXT NULL,
    prod_target BOOLEAN NOT NULL DEFAULT FALSE,
    prod_confirmation BOOLEAN NOT NULL DEFAULT FALSE,
    prod_double_confirmed BOOLEAN NOT NULL DEFAULT FALSE,
    break_glass BOOLEAN NOT NULL DEFAULT FALSE,
    break_glass_reason TEXT NULL,
    config_hash TEXT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    pushed_at TIMESTAMP NULL
);

CREATE INDEX IF NOT EXISTS idx_workload_config_approvals_workload_updated
    ON workload_config_approvals(workload_id, updated_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_workload_config_approvals_one_pending_target
    ON workload_config_approvals(workload_id, target_group, status)
    WHERE status = 'pending';

-- +goose Down
DROP INDEX IF EXISTS idx_workload_config_approvals_one_pending_target;
DROP INDEX IF EXISTS idx_workload_config_approvals_workload_updated;
DROP TABLE IF EXISTS workload_config_approvals;
