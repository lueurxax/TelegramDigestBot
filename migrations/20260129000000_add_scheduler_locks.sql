-- +goose Up
-- +goose StatementBegin

-- Row-based scheduler locks (works with connection pooling unlike advisory locks)
CREATE TABLE IF NOT EXISTS scheduler_locks (
    lock_name TEXT PRIMARY KEY,
    holder_id TEXT NOT NULL,
    acquired_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL
);

-- Index for finding expired locks
CREATE INDEX IF NOT EXISTS scheduler_locks_expires_idx ON scheduler_locks (expires_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS scheduler_locks_expires_idx;
DROP TABLE IF EXISTS scheduler_locks;

-- +goose StatementEnd
