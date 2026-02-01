-- +goose Up
ALTER TABLE link_cache ADD COLUMN canonical_url TEXT;
ALTER TABLE link_cache ADD COLUMN canonical_domain TEXT;

CREATE INDEX IF NOT EXISTS idx_link_cache_canonical_url ON link_cache(canonical_url);

-- +goose Down
DROP INDEX IF EXISTS idx_link_cache_canonical_url;
ALTER TABLE link_cache DROP COLUMN canonical_domain;
ALTER TABLE link_cache DROP COLUMN canonical_url;
