-- +goose Up
-- +goose StatementBegin
ALTER TABLE configs ADD COLUMN source_type TEXT NOT NULL DEFAULT 'manual';
ALTER TABLE configs ADD COLUMN git_url TEXT;
ALTER TABLE configs ADD COLUMN git_provider TEXT;
ALTER TABLE configs ADD COLUMN git_ref TEXT;
ALTER TABLE configs ADD COLUMN git_path TEXT;
ALTER TABLE configs ADD COLUMN commit_sha TEXT;
ALTER TABLE configs ADD COLUMN imported_at TIMESTAMP;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE configs DROP COLUMN imported_at;
ALTER TABLE configs DROP COLUMN commit_sha;
ALTER TABLE configs DROP COLUMN git_path;
ALTER TABLE configs DROP COLUMN git_ref;
ALTER TABLE configs DROP COLUMN git_provider;
ALTER TABLE configs DROP COLUMN git_url;
ALTER TABLE configs DROP COLUMN source_type;
-- +goose StatementEnd
