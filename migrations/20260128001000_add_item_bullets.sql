-- +goose Up
-- +goose StatementBegin

-- item_bullets stores extracted bullet points from items for bulletized digest output
CREATE TABLE IF NOT EXISTS item_bullets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    item_id UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    bullet_index INT NOT NULL,
    text TEXT NOT NULL,
    topic VARCHAR(255),
    relevance_score REAL DEFAULT 0,
    importance_score REAL DEFAULT 0,
    embedding vector(1536),
    bullet_hash VARCHAR(64),
    bullet_cluster_id UUID,
    status VARCHAR(32) DEFAULT 'pending',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Index for looking up bullets by item
CREATE INDEX IF NOT EXISTS idx_item_bullets_item_id ON item_bullets(item_id);

-- Index for status filtering during digest generation
CREATE INDEX IF NOT EXISTS idx_item_bullets_status ON item_bullets(status);

-- Index for deduplication by hash
CREATE INDEX IF NOT EXISTS idx_item_bullets_hash ON item_bullets(bullet_hash);

-- Index for clustering bullets
CREATE INDEX IF NOT EXISTS idx_item_bullets_cluster_id ON item_bullets(bullet_cluster_id) WHERE bullet_cluster_id IS NOT NULL;

-- Composite index for importance-based selection
CREATE INDEX IF NOT EXISTS idx_item_bullets_importance ON item_bullets(importance_score DESC, relevance_score DESC) WHERE status = 'ready';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_item_bullets_importance;
DROP INDEX IF EXISTS idx_item_bullets_cluster_id;
DROP INDEX IF EXISTS idx_item_bullets_hash;
DROP INDEX IF EXISTS idx_item_bullets_status;
DROP INDEX IF EXISTS idx_item_bullets_item_id;
DROP TABLE IF EXISTS item_bullets;

-- +goose StatementEnd
