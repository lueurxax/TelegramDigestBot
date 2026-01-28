-- +goose Up
ALTER TABLE items ADD COLUMN IF NOT EXISTS search_vector tsvector;
ALTER TABLE evidence_sources ADD COLUMN IF NOT EXISTS search_vector tsvector;

UPDATE items i
SET search_vector = to_tsvector('simple', coalesce(i.summary, '') || ' ' || coalesce(i.topic, '') || ' ' || coalesce(rm.text, ''))
FROM raw_messages rm
WHERE i.raw_message_id = rm.id;

UPDATE evidence_sources
SET search_vector = to_tsvector('simple', coalesce(title, '') || ' ' || coalesce(description, ''));

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_items_search_vector() RETURNS trigger AS $$
DECLARE
    rm_text text;
BEGIN
    SELECT text INTO rm_text FROM raw_messages WHERE id = NEW.raw_message_id;
    NEW.search_vector := to_tsvector('simple', coalesce(NEW.summary, '') || ' ' || coalesce(NEW.topic, '') || ' ' || coalesce(rm_text, ''));
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER items_search_vector_trigger
BEFORE INSERT OR UPDATE OF summary, topic, raw_message_id ON items
FOR EACH ROW EXECUTE FUNCTION update_items_search_vector();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_items_search_vector_from_raw_message() RETURNS trigger AS $$
BEGIN
    UPDATE items i
    SET search_vector = to_tsvector('simple', coalesce(i.summary, '') || ' ' || coalesce(i.topic, '') || ' ' || coalesce(NEW.text, ''))
    WHERE i.raw_message_id = NEW.id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER raw_messages_search_vector_trigger
AFTER UPDATE OF text ON raw_messages
FOR EACH ROW EXECUTE FUNCTION update_items_search_vector_from_raw_message();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_evidence_sources_search_vector() RETURNS trigger AS $$
BEGIN
    NEW.search_vector := to_tsvector('simple', coalesce(NEW.title, '') || ' ' || coalesce(NEW.description, ''));
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER evidence_sources_search_vector_trigger
BEFORE INSERT OR UPDATE OF title, description ON evidence_sources
FOR EACH ROW EXECUTE FUNCTION update_evidence_sources_search_vector();

CREATE INDEX IF NOT EXISTS items_search_vector_idx ON items USING GIN (search_vector);
CREATE INDEX IF NOT EXISTS evidence_sources_search_vector_idx ON evidence_sources USING GIN (search_vector);

CREATE TABLE research_sessions (
    token TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX research_sessions_expires_idx ON research_sessions (expires_at);

CREATE TABLE claims (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    claim_text TEXT NOT NULL,
    first_seen_at TIMESTAMPTZ NOT NULL,
    origin_cluster_id UUID REFERENCES clusters(id) ON DELETE SET NULL,
    cluster_ids UUID[] NOT NULL DEFAULT '{}',
    contradicted_by UUID[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX claims_first_seen_idx ON claims (first_seen_at);

CREATE TABLE cluster_first_appearance (
    cluster_id UUID PRIMARY KEY REFERENCES clusters(id) ON DELETE CASCADE,
    channel_id UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    first_item_id UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    first_seen_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX cluster_first_appearance_channel_idx ON cluster_first_appearance (channel_id);

CREATE TABLE cluster_topic_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    cluster_id UUID NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    topic TEXT NOT NULL,
    window_start TIMESTAMPTZ NOT NULL,
    window_end TIMESTAMPTZ NOT NULL
);

CREATE INDEX cluster_topic_history_cluster_idx ON cluster_topic_history (cluster_id);
CREATE INDEX cluster_topic_history_window_idx ON cluster_topic_history (window_start, window_end);

CREATE TABLE cluster_language_links (
    cluster_id UUID NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    language TEXT NOT NULL,
    linked_cluster_id UUID NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    confidence REAL NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (cluster_id, linked_cluster_id, language)
);

CREATE INDEX cluster_language_links_language_idx ON cluster_language_links (language);

CREATE MATERIALIZED VIEW mv_topic_timeline AS
SELECT date_trunc('week', rm.tg_date)::date AS bucket_date,
       i.topic,
       COUNT(*) AS item_count,
       AVG(i.importance_score) AS avg_importance,
       AVG(i.relevance_score) AS avg_relevance
FROM items i
JOIN raw_messages rm ON i.raw_message_id = rm.id
GROUP BY bucket_date, i.topic;

CREATE MATERIALIZED VIEW mv_channel_overlap AS
WITH channel_clusters AS (
    SELECT ch.id AS channel_id, ci.cluster_id
    FROM cluster_items ci
    JOIN items i ON ci.item_id = i.id
    JOIN raw_messages rm ON i.raw_message_id = rm.id
    JOIN channels ch ON rm.channel_id = ch.id
    GROUP BY ch.id, ci.cluster_id
),
cluster_counts AS (
    SELECT channel_id, COUNT(*) AS total_clusters
    FROM channel_clusters
    GROUP BY channel_id
),
shared AS (
    SELECT c1.channel_id AS channel_a, c2.channel_id AS channel_b, COUNT(*) AS shared_clusters
    FROM channel_clusters c1
    JOIN channel_clusters c2
      ON c1.cluster_id = c2.cluster_id AND c1.channel_id < c2.channel_id
    GROUP BY c1.channel_id, c2.channel_id
)
SELECT s.channel_a,
       s.channel_b,
       s.shared_clusters,
       c1.total_clusters AS total_a,
       c2.total_clusters AS total_b,
       (s.shared_clusters::double precision / (c1.total_clusters + c2.total_clusters - s.shared_clusters)) AS jaccard
FROM shared s
JOIN cluster_counts c1 ON s.channel_a = c1.channel_id
JOIN cluster_counts c2 ON s.channel_b = c2.channel_id;

CREATE MATERIALIZED VIEW mv_cluster_stats AS
SELECT c.id AS cluster_id,
       c.topic,
       MIN(rm.tg_date) AS first_seen_at,
       MAX(rm.tg_date) AS last_seen_at,
       COUNT(*) AS item_count,
       COUNT(DISTINCT ch.id) AS unique_channels
FROM clusters c
JOIN cluster_items ci ON c.id = ci.cluster_id
JOIN items i ON ci.item_id = i.id
JOIN raw_messages rm ON i.raw_message_id = rm.id
JOIN channels ch ON rm.channel_id = ch.id
GROUP BY c.id, c.topic;

CREATE UNIQUE INDEX mv_topic_timeline_unique_idx ON mv_topic_timeline (bucket_date, topic);
CREATE UNIQUE INDEX mv_channel_overlap_unique_idx ON mv_channel_overlap (channel_a, channel_b);
CREATE UNIQUE INDEX mv_cluster_stats_unique_idx ON mv_cluster_stats (cluster_id);

CREATE INDEX mv_channel_overlap_jaccard_idx ON mv_channel_overlap (jaccard DESC);
CREATE INDEX mv_cluster_stats_first_seen_idx ON mv_cluster_stats (first_seen_at);
CREATE INDEX mv_cluster_stats_topic_idx ON mv_cluster_stats (topic);

-- +goose Down
DROP INDEX IF EXISTS mv_cluster_stats_topic_idx;
DROP INDEX IF EXISTS mv_cluster_stats_first_seen_idx;
DROP INDEX IF EXISTS mv_channel_overlap_jaccard_idx;
DROP INDEX IF EXISTS mv_cluster_stats_unique_idx;
DROP INDEX IF EXISTS mv_channel_overlap_unique_idx;
DROP INDEX IF EXISTS mv_topic_timeline_unique_idx;

DROP MATERIALIZED VIEW IF EXISTS mv_cluster_stats;
DROP MATERIALIZED VIEW IF EXISTS mv_channel_overlap;
DROP MATERIALIZED VIEW IF EXISTS mv_topic_timeline;

DROP TABLE IF EXISTS cluster_language_links;
DROP TABLE IF EXISTS cluster_topic_history;
DROP TABLE IF EXISTS cluster_first_appearance;
DROP TABLE IF EXISTS claims;
DROP TABLE IF EXISTS research_sessions;

DROP INDEX IF EXISTS evidence_sources_search_vector_idx;
DROP INDEX IF EXISTS items_search_vector_idx;

DROP TRIGGER IF EXISTS evidence_sources_search_vector_trigger ON evidence_sources;
DROP FUNCTION IF EXISTS update_evidence_sources_search_vector;

DROP TRIGGER IF EXISTS raw_messages_search_vector_trigger ON raw_messages;
DROP FUNCTION IF EXISTS update_items_search_vector_from_raw_message;

DROP TRIGGER IF EXISTS items_search_vector_trigger ON items;
DROP FUNCTION IF EXISTS update_items_search_vector;

ALTER TABLE items DROP COLUMN IF EXISTS search_vector;
ALTER TABLE evidence_sources DROP COLUMN IF EXISTS search_vector;
