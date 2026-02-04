-- +goose NO TRANSACTION
-- +goose Up
-- Add index on cluster_items(item_id) for reverse lookups from items to clusters
CREATE INDEX CONCURRENTLY IF NOT EXISTS cluster_items_item_id_idx
ON cluster_items (item_id);

-- +goose Down
DROP INDEX IF EXISTS cluster_items_item_id_idx;
