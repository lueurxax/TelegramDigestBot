-- +goose NO TRANSACTION
-- +goose Up
-- Add index on items.language for cluster_language_links query performance
CREATE INDEX CONCURRENTLY IF NOT EXISTS items_language_idx
ON items (language) WHERE language IS NOT NULL AND language <> '';

-- Add standalone index on clusters.source for filtering research clusters
CREATE INDEX CONCURRENTLY IF NOT EXISTS clusters_source_standalone_idx
ON clusters (source);

-- +goose Down
DROP INDEX IF EXISTS items_language_idx;
DROP INDEX IF EXISTS clusters_source_standalone_idx;
