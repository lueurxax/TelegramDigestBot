-- +goose Up
ALTER TABLE raw_messages ADD COLUMN preview_text TEXT;

-- +goose Down
ALTER TABLE raw_messages DROP COLUMN preview_text;
