-- +goose Up
-- +goose StatementBegin
ALTER TABLE items ADD COLUMN digested_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS items_digested_at_idx ON items(digested_at) WHERE digested_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS items_digested_at_idx;
ALTER TABLE items DROP COLUMN digested_at;
-- +goose StatementEnd
