-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS raw_message_drop_log (
    id SERIAL PRIMARY KEY,
    raw_message_id UUID NOT NULL REFERENCES raw_messages(id) ON DELETE CASCADE,
    reason TEXT NOT NULL,
    detail TEXT,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (raw_message_id)
);

CREATE INDEX IF NOT EXISTS raw_message_drop_log_reason_idx ON raw_message_drop_log(reason);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS raw_message_drop_log;
-- +goose StatementEnd
