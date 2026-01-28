-- +goose Up
-- +goose StatementBegin
CREATE TABLE channel_weight_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    importance_weight REAL NOT NULL,
    auto_weight_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    weight_override BOOLEAN NOT NULL DEFAULT FALSE,
    reason TEXT,
    updated_by BIGINT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX channel_weight_history_channel_idx ON channel_weight_history (channel_id, updated_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS channel_weight_history_channel_idx;
DROP TABLE IF EXISTS channel_weight_history;
-- +goose StatementEnd
