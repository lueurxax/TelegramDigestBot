-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS annotation_queue (
    id SERIAL PRIMARY KEY,
    item_id UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending',
    assigned_to BIGINT,
    assigned_at TIMESTAMPTZ,
    label TEXT,
    comment TEXT,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (item_id)
);

CREATE INDEX IF NOT EXISTS annotation_queue_status_idx ON annotation_queue(status);
CREATE INDEX IF NOT EXISTS annotation_queue_assigned_idx ON annotation_queue(assigned_to) WHERE assigned_to IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS annotation_queue;
-- +goose StatementEnd
