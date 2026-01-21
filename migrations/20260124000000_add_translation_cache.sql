-- +goose Up
CREATE TABLE translation_cache (
    query TEXT NOT NULL,
    target_lang VARCHAR(10) NOT NULL,
    translated_text TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (query, target_lang)
);

CREATE INDEX idx_translation_cache_expires_at ON translation_cache (expires_at);

-- +goose Down
DROP TABLE translation_cache;
