-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS digest_ratings (
    id SERIAL PRIMARY KEY,
    digest_id UUID NOT NULL REFERENCES digests(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL,
    rating SMALLINT NOT NULL, -- 1 for 👍, -1 for 👎
    feedback TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(digest_id, user_id)
);

CREATE TABLE IF NOT EXISTS setting_history (
    id SERIAL PRIMARY KEY,
    key TEXT NOT NULL,
    old_value TEXT,
    new_value TEXT,
    changed_by BIGINT NOT NULL,
    changed_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS setting_history;
DROP TABLE IF EXISTS digest_ratings;
-- +goose StatementEnd
