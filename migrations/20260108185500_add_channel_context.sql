-- +goose Up
-- +goose StatementBegin
ALTER TABLE channels ADD COLUMN context TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE channels DROP COLUMN context;
-- +goose StatementEnd
