-- +goose Up
-- Add support for heuristic claim extraction

-- Add normalized_hash column for deduplication
ALTER TABLE claims ADD COLUMN IF NOT EXISTS normalized_hash TEXT;

-- Create unique index on normalized_hash for upsert support
CREATE UNIQUE INDEX IF NOT EXISTS claims_normalized_hash_idx ON claims (normalized_hash) WHERE normalized_hash IS NOT NULL;

-- Add index for querying by source type (evidence vs heuristic)
CREATE INDEX IF NOT EXISTS claims_source_type_idx ON claims ((normalized_hash IS NULL));

-- Add embedding column for semantic deduplication (pgvector)
ALTER TABLE claims ADD COLUMN IF NOT EXISTS embedding vector(1536);

-- Create index for embedding similarity search
CREATE INDEX IF NOT EXISTS claims_embedding_idx ON claims USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);

-- +goose Down
DROP INDEX IF EXISTS claims_embedding_idx;
DROP INDEX IF EXISTS claims_source_type_idx;
DROP INDEX IF EXISTS claims_normalized_hash_idx;
ALTER TABLE claims DROP COLUMN IF EXISTS embedding;
ALTER TABLE claims DROP COLUMN IF EXISTS normalized_hash;
