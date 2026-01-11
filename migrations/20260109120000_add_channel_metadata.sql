-- +goose Up
-- +goose StatementBegin
ALTER TABLE channels ADD COLUMN category TEXT;
ALTER TABLE channels ADD COLUMN tone TEXT;
ALTER TABLE channels ADD COLUMN update_freq TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE channels DROP COLUMN update_freq;
ALTER TABLE channels DROP COLUMN tone;
ALTER TABLE channels DROP COLUMN category;
-- +goose StatementEnd
