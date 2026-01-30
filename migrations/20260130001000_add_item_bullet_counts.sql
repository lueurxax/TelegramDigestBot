-- +goose Up
-- +goose StatementBegin
ALTER TABLE items
ADD COLUMN IF NOT EXISTS bullet_total_count integer NOT NULL DEFAULT 0,
ADD COLUMN IF NOT EXISTS bullet_included_count integer NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE items
DROP COLUMN IF EXISTS bullet_included_count,
DROP COLUMN IF EXISTS bullet_total_count;
-- +goose StatementEnd
