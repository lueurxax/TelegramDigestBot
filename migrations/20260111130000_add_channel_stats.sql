-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS channel_stats (
    id SERIAL PRIMARY KEY,
    channel_id UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    period_start DATE NOT NULL,
    period_end DATE NOT NULL,

    -- Volume metrics
    messages_received INT DEFAULT 0,
    items_created INT DEFAULT 0,
    items_digested INT DEFAULT 0,

    -- Quality metrics
    avg_importance FLOAT,
    avg_relevance FLOAT,

    -- Timestamps
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),

    UNIQUE(channel_id, period_start, period_end)
);

CREATE INDEX idx_channel_stats_channel_id ON channel_stats(channel_id);
CREATE INDEX idx_channel_stats_period ON channel_stats(period_start, period_end);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS channel_stats;
-- +goose StatementEnd
