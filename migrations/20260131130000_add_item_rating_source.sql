-- +goose Up
-- +goose StatementBegin
ALTER TABLE item_ratings
ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'web-list';

UPDATE item_ratings
SET source = 'web-list'
WHERE source IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE item_ratings
DROP COLUMN IF EXISTS source;
-- +goose StatementEnd
