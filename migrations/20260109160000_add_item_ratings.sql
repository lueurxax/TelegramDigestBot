-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS item_ratings (
    id SERIAL PRIMARY KEY,
    item_id UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL,
    rating TEXT NOT NULL, -- 'good', 'bad', 'irrelevant'
    feedback TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(item_id, user_id)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS item_ratings;
-- +goose StatementEnd
