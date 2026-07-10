-- +goose Up
-- +goose StatementBegin
ALTER TABLE workload_configs ADD COLUMN label TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE workload_configs DROP COLUMN label;
-- +goose StatementEnd
