-- +goose Up
-- +goose StatementBegin
ALTER TABLE channels ADD COLUMN access_hash BIGINT;
ALTER TABLE channels ADD COLUMN invite_link TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS channels_invite_link_uq ON channels (invite_link) WHERE invite_link IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS channels_invite_link_uq;
ALTER TABLE channels DROP COLUMN invite_link;
ALTER TABLE channels DROP COLUMN access_hash;
-- +goose StatementEnd
