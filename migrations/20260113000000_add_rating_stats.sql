-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS channel_rating_stats (
    channel_id UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    period_start DATE NOT NULL,
    period_end DATE NOT NULL,
    weighted_good DOUBLE PRECISION NOT NULL DEFAULT 0,
    weighted_bad DOUBLE PRECISION NOT NULL DEFAULT 0,
    weighted_irrelevant DOUBLE PRECISION NOT NULL DEFAULT 0,
    weighted_total DOUBLE PRECISION NOT NULL DEFAULT 0,
    rating_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (channel_id, period_start, period_end)
);

CREATE TABLE IF NOT EXISTS global_rating_stats (
    period_start DATE NOT NULL,
    period_end DATE NOT NULL,
    weighted_good DOUBLE PRECISION NOT NULL DEFAULT 0,
    weighted_bad DOUBLE PRECISION NOT NULL DEFAULT 0,
    weighted_irrelevant DOUBLE PRECISION NOT NULL DEFAULT 0,
    weighted_total DOUBLE PRECISION NOT NULL DEFAULT 0,
    rating_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (period_start, period_end)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS global_rating_stats;
DROP TABLE IF EXISTS channel_rating_stats;
-- +goose StatementEnd
