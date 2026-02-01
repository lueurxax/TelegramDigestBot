-- +goose Up
CREATE TABLE IF NOT EXISTS item_link_debug (
    item_id uuid PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    link_context_used boolean NOT NULL DEFAULT false,
    link_content_len int NOT NULL DEFAULT 0,
    link_lang_queries int NOT NULL DEFAULT 0,
    canonical_source_detected boolean NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS item_link_debug;
