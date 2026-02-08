-- +goose Up
-- +goose StatementBegin
ALTER TABLE raw_messages
  ADD COLUMN IF NOT EXISTS has_comments_thread BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS raw_messages_channel_comments_date_idx
  ON raw_messages (channel_id, tg_date DESC)
  WHERE has_comments_thread = TRUE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS raw_messages_channel_comments_date_idx;

ALTER TABLE raw_messages
  DROP COLUMN IF EXISTS has_comments_thread;
-- +goose StatementEnd
