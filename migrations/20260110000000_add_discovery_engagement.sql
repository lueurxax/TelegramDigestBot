-- +goose Up
-- +goose StatementBegin
ALTER TABLE discovered_channels
    ADD COLUMN max_views INT DEFAULT 0,
    ADD COLUMN max_forwards INT DEFAULT 0,
    ADD COLUMN engagement_score REAL DEFAULT 0;

CREATE INDEX IF NOT EXISTS discovered_channels_engagement_idx
    ON discovered_channels (engagement_score DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS discovered_channels_engagement_idx;
ALTER TABLE discovered_channels
    DROP COLUMN max_views,
    DROP COLUMN max_forwards,
    DROP COLUMN engagement_score;
-- +goose StatementEnd
