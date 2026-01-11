-- +goose Up
-- +goose StatementBegin
ALTER TABLE raw_messages ADD COLUMN discoveries_extracted BOOLEAN DEFAULT FALSE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE raw_messages DROP COLUMN discoveries_extracted;
-- +goose StatementEnd
