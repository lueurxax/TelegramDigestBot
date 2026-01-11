-- +goose Up
-- Unified link content cache
CREATE TABLE link_cache (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    url TEXT UNIQUE NOT NULL,
    domain TEXT NOT NULL,
    link_type TEXT NOT NULL,        -- 'web', 'telegram'

    -- Common fields
    title TEXT,
    content TEXT,                   -- Article text or message text
    author TEXT,
    published_at TIMESTAMPTZ,

    -- Web-specific
    description TEXT,
    image_url TEXT,
    word_count INT,

    -- Telegram-specific
    channel_username TEXT,
    channel_title TEXT,
    channel_id BIGINT,
    message_id BIGINT,
    views INT,
    forwards INT,
    has_media BOOLEAN DEFAULT FALSE,
    media_type TEXT,

    -- Status
    status TEXT NOT NULL DEFAULT 'pending',  -- pending, success, failed, blocked, no_access
    error_message TEXT,
    language TEXT,

    -- Timestamps
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    expires_at TIMESTAMPTZ
);

CREATE INDEX idx_link_cache_url ON link_cache(url);
CREATE INDEX idx_link_cache_domain ON link_cache(domain);
CREATE INDEX idx_link_cache_status ON link_cache(status);
CREATE INDEX idx_link_cache_expires ON link_cache(expires_at);

-- Message to link association
CREATE TABLE message_links (
    raw_message_id UUID REFERENCES raw_messages(id) ON DELETE CASCADE,
    link_cache_id UUID REFERENCES link_cache(id) ON DELETE CASCADE,
    position INT,
    PRIMARY KEY (raw_message_id, link_cache_id)
);

-- +goose Down
DROP TABLE message_links;
DROP TABLE link_cache;
