-- +goose Up
-- +goose StatementBegin
ALTER TABLE items ADD COLUMN retry_count INT NOT NULL DEFAULT 0;
ALTER TABLE items ADD COLUMN next_retry_at TIMESTAMPTZ;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE items DROP COLUMN retry_count;
ALTER TABLE items DROP COLUMN next_retry_at;
-- +goose StatementEnd
