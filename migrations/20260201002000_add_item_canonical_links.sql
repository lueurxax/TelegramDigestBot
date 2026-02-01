-- +goose Up
CREATE TABLE IF NOT EXISTS item_canonical_links (
    item_id uuid PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    canonical_item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    canonical_url text NOT NULL,
    similarity real,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_item_canonical_links_canonical_item_id
    ON item_canonical_links (canonical_item_id);

CREATE INDEX IF NOT EXISTS idx_item_canonical_links_canonical_url
    ON item_canonical_links (canonical_url);

-- +goose Down
DROP TABLE IF EXISTS item_canonical_links;
