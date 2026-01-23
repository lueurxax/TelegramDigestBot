-- +goose Up
-- +goose StatementBegin

-- Add processing_started_at column to support FOR UPDATE SKIP LOCKED pattern
-- This prevents multiple workers from processing the same messages
ALTER TABLE raw_messages ADD COLUMN IF NOT EXISTS processing_started_at TIMESTAMPTZ;

-- Index for finding stuck messages (claimed but not processed)
CREATE INDEX IF NOT EXISTS raw_messages_stuck_idx
ON raw_messages (processing_started_at)
WHERE processing_started_at IS NOT NULL AND processed_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS raw_messages_stuck_idx;
ALTER TABLE raw_messages DROP COLUMN IF EXISTS processing_started_at;

-- +goose StatementEnd
