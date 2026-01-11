-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS discovered_channels (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    -- Channel identification (at least one should be set)
    username TEXT,
    tg_peer_id BIGINT DEFAULT 0,
    invite_link TEXT,

    -- Metadata
    title TEXT,

    -- Discovery tracking
    source_type TEXT NOT NULL,                 -- 'forward', 'link', 'mention'
    discovery_count INT NOT NULL DEFAULT 1,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Source tracking
    discovered_from_channel_id UUID REFERENCES channels(id) ON DELETE SET NULL,

    -- Status workflow
    status TEXT NOT NULL DEFAULT 'pending',    -- 'pending', 'approved', 'rejected', 'added'
    status_changed_at TIMESTAMPTZ,
    status_changed_by BIGINT
);

-- Unique indexes for upsert (conditional uniqueness)
CREATE UNIQUE INDEX IF NOT EXISTS discovered_channels_username_uq
    ON discovered_channels (username) WHERE username IS NOT NULL AND username != '';
CREATE UNIQUE INDEX IF NOT EXISTS discovered_channels_peer_id_uq
    ON discovered_channels (tg_peer_id) WHERE tg_peer_id != 0;
CREATE UNIQUE INDEX IF NOT EXISTS discovered_channels_invite_uq
    ON discovered_channels (invite_link) WHERE invite_link IS NOT NULL AND invite_link != '';

-- Query indexes
CREATE INDEX IF NOT EXISTS discovered_channels_status_idx ON discovered_channels (status);
CREATE INDEX IF NOT EXISTS discovered_channels_count_idx ON discovered_channels (discovery_count DESC);
CREATE INDEX IF NOT EXISTS discovered_channels_last_seen_idx ON discovered_channels (last_seen_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS discovered_channels;
-- +goose StatementEnd
