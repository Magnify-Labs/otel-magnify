-- +goose Up
-- +goose StatementBegin
CREATE TABLE gitops_validation_statuses (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider TEXT NOT NULL,
    event TEXT NOT NULL,
    action TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    source_path TEXT NOT NULL DEFAULT '',
    source_ref TEXT NOT NULL DEFAULT '',
    commit_sha TEXT NOT NULL DEFAULT '',
    observed_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_gitops_validation_statuses_lookup
    ON gitops_validation_statuses(provider, source_path, source_ref, commit_sha, observed_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE gitops_validation_statuses;
-- +goose StatementEnd
