-- +goose Up
-- +goose StatementBegin
ALTER TABLE raw_messages ADD COLUMN media_data BYTEA;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE raw_messages DROP COLUMN media_data;
-- +goose StatementEnd
