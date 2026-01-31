-- +goose Up
DROP INDEX IF EXISTS claims_normalized_hash_idx;
CREATE UNIQUE INDEX IF NOT EXISTS claims_normalized_hash_idx ON claims (normalized_hash);

-- +goose Down
DROP INDEX IF EXISTS claims_normalized_hash_idx;
CREATE UNIQUE INDEX IF NOT EXISTS claims_normalized_hash_idx ON claims (normalized_hash) WHERE normalized_hash IS NOT NULL;
