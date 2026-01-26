-- +goose Up
ALTER TABLE items ADD COLUMN first_seen_at TIMESTAMPTZ;

UPDATE items i
SET first_seen_at = rm.tg_date
FROM raw_messages rm
WHERE i.raw_message_id = rm.id AND i.first_seen_at IS NULL;

CREATE TABLE channel_quality_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    period_start DATE NOT NULL,
    period_end DATE NOT NULL,
    inclusion_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
    noise_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
    avg_importance DOUBLE PRECISION NOT NULL DEFAULT 0,
    avg_relevance DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (channel_id, period_start, period_end)
);

CREATE INDEX channel_quality_history_period_idx ON channel_quality_history (period_start, period_end);
CREATE INDEX channel_quality_history_channel_period_idx ON channel_quality_history (channel_id, period_start);

CREATE TABLE threshold_tuning_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tuned_at TIMESTAMPTZ NOT NULL,
    net_score DOUBLE PRECISION NOT NULL,
    delta REAL NOT NULL,
    relevance_threshold REAL NOT NULL,
    importance_threshold REAL NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX threshold_tuning_log_tuned_at_idx ON threshold_tuning_log (tuned_at);
CREATE INDEX items_first_seen_at_idx ON items (first_seen_at);

-- +goose Down
DROP INDEX IF EXISTS items_first_seen_at_idx;
DROP INDEX IF EXISTS threshold_tuning_log_tuned_at_idx;
DROP INDEX IF EXISTS channel_quality_history_channel_period_idx;
DROP INDEX IF EXISTS channel_quality_history_period_idx;
DROP TABLE threshold_tuning_log;
DROP TABLE channel_quality_history;
ALTER TABLE items DROP COLUMN first_seen_at;
