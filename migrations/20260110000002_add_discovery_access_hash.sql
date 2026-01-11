-- +goose Up
-- +goose StatementBegin
ALTER TABLE discovered_channels ADD COLUMN IF NOT EXISTS access_hash BIGINT DEFAULT 0;
ALTER TABLE discovered_channels ADD COLUMN IF NOT EXISTS resolution_attempts INT DEFAULT 0;
ALTER TABLE discovered_channels ADD COLUMN IF NOT EXISTS last_resolution_attempt TIMESTAMPTZ;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE discovered_channels DROP COLUMN access_hash;
ALTER TABLE discovered_channels DROP COLUMN resolution_attempts;
ALTER TABLE discovered_channels DROP COLUMN last_resolution_attempt;
-- +goose StatementEnd
