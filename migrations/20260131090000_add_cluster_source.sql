-- +goose Up
ALTER TABLE clusters
ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'digest';

UPDATE clusters SET source = 'digest' WHERE source IS NULL;

CREATE INDEX IF NOT EXISTS clusters_source_window_idx ON clusters (source, window_start, window_end);

DROP INDEX IF EXISTS mv_channel_overlap_unique_idx;
DROP INDEX IF EXISTS mv_channel_overlap_jaccard_idx;
DROP MATERIALIZED VIEW IF EXISTS mv_channel_overlap;

CREATE MATERIALIZED VIEW mv_channel_overlap AS
WITH channel_clusters AS (
    SELECT ch.id AS channel_id, ci.cluster_id
    FROM cluster_items ci
    JOIN clusters c ON ci.cluster_id = c.id
    JOIN items i ON ci.item_id = i.id
    JOIN raw_messages rm ON i.raw_message_id = rm.id
    JOIN channels ch ON rm.channel_id = ch.id
    WHERE c.source = 'research'
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

CREATE UNIQUE INDEX mv_channel_overlap_unique_idx ON mv_channel_overlap (channel_a, channel_b);
CREATE INDEX mv_channel_overlap_jaccard_idx ON mv_channel_overlap (jaccard DESC);

DROP INDEX IF EXISTS mv_cluster_stats_unique_idx;
DROP INDEX IF EXISTS mv_cluster_stats_first_seen_idx;
DROP INDEX IF EXISTS mv_cluster_stats_topic_idx;
DROP MATERIALIZED VIEW IF EXISTS mv_cluster_stats;

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
WHERE c.source = 'research'
GROUP BY c.id, c.topic;

CREATE UNIQUE INDEX mv_cluster_stats_unique_idx ON mv_cluster_stats (cluster_id);
CREATE INDEX mv_cluster_stats_first_seen_idx ON mv_cluster_stats (first_seen_at);
CREATE INDEX mv_cluster_stats_topic_idx ON mv_cluster_stats (topic);

-- +goose Down
DROP INDEX IF EXISTS clusters_source_window_idx;

DROP INDEX IF EXISTS mv_channel_overlap_unique_idx;
DROP INDEX IF EXISTS mv_channel_overlap_jaccard_idx;
DROP MATERIALIZED VIEW IF EXISTS mv_channel_overlap;

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

CREATE UNIQUE INDEX mv_channel_overlap_unique_idx ON mv_channel_overlap (channel_a, channel_b);
CREATE INDEX mv_channel_overlap_jaccard_idx ON mv_channel_overlap (jaccard DESC);

DROP INDEX IF EXISTS mv_cluster_stats_unique_idx;
DROP INDEX IF EXISTS mv_cluster_stats_first_seen_idx;
DROP INDEX IF EXISTS mv_cluster_stats_topic_idx;
DROP MATERIALIZED VIEW IF EXISTS mv_cluster_stats;

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

CREATE UNIQUE INDEX mv_cluster_stats_unique_idx ON mv_cluster_stats (cluster_id);
CREATE INDEX mv_cluster_stats_first_seen_idx ON mv_cluster_stats (first_seen_at);
CREATE INDEX mv_cluster_stats_topic_idx ON mv_cluster_stats (topic);

ALTER TABLE clusters DROP COLUMN IF EXISTS source;
