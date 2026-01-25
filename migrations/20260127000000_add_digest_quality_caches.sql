-- +goose Up
-- +goose StatementBegin
ALTER TABLE items ADD COLUMN IF NOT EXISTS language_source TEXT;

CREATE TABLE IF NOT EXISTS summary_cache (
    canonical_hash TEXT NOT NULL,
    digest_language TEXT NOT NULL,
    summary TEXT NOT NULL,
    topic TEXT,
    language TEXT,
    relevance_score REAL NOT NULL DEFAULT 0,
    importance_score REAL NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (canonical_hash, digest_language)
);

CREATE TABLE IF NOT EXISTS cluster_summary_cache (
    digest_language TEXT NOT NULL,
    cluster_fingerprint TEXT NOT NULL,
    item_ids JSONB NOT NULL,
    summary TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (digest_language, cluster_fingerprint)
);

CREATE INDEX IF NOT EXISTS idx_cluster_summary_cache_updated_at
    ON cluster_summary_cache (updated_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS cluster_summary_cache;
DROP TABLE IF EXISTS summary_cache;
ALTER TABLE items DROP COLUMN IF EXISTS language_source;
-- +goose StatementEnd
