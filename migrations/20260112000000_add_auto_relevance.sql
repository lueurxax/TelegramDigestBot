-- +goose Up
-- +goose StatementBegin
ALTER TABLE channels ADD COLUMN auto_relevance_enabled BOOLEAN DEFAULT TRUE;
ALTER TABLE channels ADD COLUMN relevance_threshold_delta FLOAT4 DEFAULT 0.0;

CREATE TABLE IF NOT EXISTS relevance_gate_log (
    id SERIAL PRIMARY KEY,
    raw_message_id UUID NOT NULL REFERENCES raw_messages(id) ON DELETE CASCADE,
    decision TEXT NOT NULL,
    confidence FLOAT4,
    reason TEXT,
    model TEXT,
    gate_version TEXT,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS relevance_gate_log;
ALTER TABLE channels DROP COLUMN IF EXISTS relevance_threshold_delta;
ALTER TABLE channels DROP COLUMN IF EXISTS auto_relevance_enabled;
-- +goose StatementEnd
