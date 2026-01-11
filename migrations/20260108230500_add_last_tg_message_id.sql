-- +goose Up
-- +goose StatementBegin
ALTER TABLE channels ADD COLUMN last_tg_message_id BIGINT NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE channels DROP COLUMN last_tg_message_id;
-- +goose StatementEnd
