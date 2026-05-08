-- +goose Up
-- +goose StatementBegin
ALTER TABLE workload_configs ADD COLUMN label TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- SQLite supports DROP COLUMN since 3.35 (modernc.org/sqlite ships a newer
-- version) and Postgres has supported it forever.
ALTER TABLE workload_configs DROP COLUMN label;
-- +goose StatementEnd
