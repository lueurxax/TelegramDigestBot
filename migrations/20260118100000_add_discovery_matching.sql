-- +goose Up
-- +goose StatementBegin
ALTER TABLE discovered_channels
    ADD COLUMN IF NOT EXISTS matched_channel_id UUID REFERENCES channels(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS description TEXT;

CREATE INDEX IF NOT EXISTS discovered_channels_matched_idx
    ON discovered_channels (matched_channel_id);

CREATE INDEX IF NOT EXISTS discovered_channels_pending_idx
    ON discovered_channels (status, engagement_score DESC)
    WHERE status = 'pending';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS discovered_channels_pending_idx;
DROP INDEX IF EXISTS discovered_channels_matched_idx;

ALTER TABLE discovered_channels
    DROP COLUMN IF EXISTS description,
    DROP COLUMN IF EXISTS matched_channel_id;
-- +goose StatementEnd
