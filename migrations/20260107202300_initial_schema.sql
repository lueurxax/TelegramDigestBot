-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS channels (
  id                UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  tg_peer_id        BIGINT NOT NULL,              -- MTProto peer/channel ID
  username          TEXT,                         -- optional
  title             TEXT,
  is_active         BOOLEAN NOT NULL DEFAULT TRUE,
  added_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  added_by_tg_user  BIGINT                         -- admin TG user id (optional)
);

CREATE UNIQUE INDEX IF NOT EXISTS channels_peer_id_uq ON channels (tg_peer_id);
CREATE UNIQUE INDEX IF NOT EXISTS channels_username_uq ON channels (username);
CREATE INDEX IF NOT EXISTS channels_active_idx ON channels (is_active);

CREATE TABLE IF NOT EXISTS settings (
  key   TEXT PRIMARY KEY,
  value JSONB NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS raw_messages (
  id               UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  channel_id        UUID NOT NULL REFERENCES channels(id),
  tg_message_id     BIGINT NOT NULL,
  tg_date           TIMESTAMPTZ NOT NULL,
  text              TEXT,
  entities_json     JSONB,
  media_json        JSONB,
  canonical_hash    TEXT NOT NULL,          -- sha256(canonicalized text)
  is_forward        BOOLEAN NOT NULL DEFAULT FALSE,
  inserted_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  processed_at      TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS raw_messages_uq ON raw_messages (channel_id, tg_message_id);
CREATE INDEX IF NOT EXISTS raw_messages_unprocessed_idx ON raw_messages (processed_at) WHERE processed_at IS NULL;
CREATE INDEX IF NOT EXISTS raw_messages_hash_idx ON raw_messages (canonical_hash);
CREATE INDEX IF NOT EXISTS raw_messages_date_idx ON raw_messages (tg_date);

CREATE TABLE IF NOT EXISTS items (
  id                 UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  raw_message_id      UUID NOT NULL REFERENCES raw_messages(id),
  relevance_score     REAL NOT NULL DEFAULT 0,     -- 0..1
  importance_score    REAL NOT NULL DEFAULT 0,     -- 0..1
  topic              TEXT,
  summary             TEXT,
  language           TEXT,
  status             TEXT NOT NULL DEFAULT 'ready',  -- ready|rejected|error
  error_json          JSONB,
  created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS items_raw_message_uq ON items(raw_message_id);
CREATE INDEX IF NOT EXISTS items_status_idx ON items(status);
CREATE INDEX IF NOT EXISTS items_scores_idx ON items(importance_score DESC, relevance_score DESC);

CREATE TABLE IF NOT EXISTS embeddings (
  item_id     UUID PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
  embedding   vector(1536),              -- dimension depends on model
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS embeddings_ivfflat_idx ON embeddings USING ivfflat (embedding vector_cosine_ops);

CREATE TABLE IF NOT EXISTS clusters (
  id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  window_start TIMESTAMPTZ NOT NULL,
  window_end   TIMESTAMPTZ NOT NULL,
  topic        TEXT,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS clusters_window_idx ON clusters (window_start, window_end);

CREATE TABLE IF NOT EXISTS cluster_items (
  cluster_id UUID NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
  item_id    UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  PRIMARY KEY (cluster_id, item_id)
);

CREATE TABLE IF NOT EXISTS digests (
  id             UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  window_start   TIMESTAMPTZ NOT NULL,
  window_end     TIMESTAMPTZ NOT NULL,
  posted_chat_id BIGINT,                -- target channel id
  posted_msg_id  BIGINT,                -- TG message id created by bot
  status         TEXT NOT NULL DEFAULT 'created', -- created|posted|error
  error_json     JSONB,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  posted_at      TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS digests_window_uq ON digests (window_start, window_end);
CREATE INDEX IF NOT EXISTS digests_status_idx ON digests (status);

CREATE TABLE IF NOT EXISTS digest_entries (
  id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  digest_id    UUID NOT NULL REFERENCES digests(id) ON DELETE CASCADE,
  title        TEXT,
  body         TEXT NOT NULL,
  sources_json JSONB NOT NULL,     -- [{channel, msg_id, link, ...}, ...]
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS digest_entries_digest_idx ON digest_entries(digest_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS digest_entries;
DROP TABLE IF EXISTS digests;
DROP TABLE IF EXISTS cluster_items;
DROP TABLE IF EXISTS clusters;
DROP TABLE IF EXISTS embeddings;
DROP TABLE IF EXISTS items;
DROP TABLE IF EXISTS raw_messages;
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS channels;
-- +goose StatementEnd
