-- name: GetActiveChannels :many
SELECT id, tg_peer_id, username, title, is_active, access_hash, invite_link, context, description, last_tg_message_id, category, tone, update_freq, relevance_threshold, importance_threshold, importance_weight, auto_weight_enabled, weight_override, auto_relevance_enabled, relevance_threshold_delta FROM channels WHERE is_active = TRUE;

-- name: SaveRawMessage :exec
INSERT INTO raw_messages (channel_id, tg_message_id, tg_date, text, entities_json, media_json, media_data, canonical_hash, is_forward)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (channel_id, tg_message_id) DO UPDATE SET media_data = EXCLUDED.media_data WHERE raw_messages.media_data IS NULL;

-- name: AddChannel :exec
INSERT INTO channels (tg_peer_id, username, title)
VALUES ($1, $2, $3)
ON CONFLICT (tg_peer_id) WHERE tg_peer_id != 0 DO UPDATE SET username = $2, title = $3, is_active = TRUE;

-- name: AddChannelByUsername :exec
INSERT INTO channels (tg_peer_id, username, title)
VALUES (0, $1, '')
ON CONFLICT (username) DO UPDATE SET is_active = TRUE;

-- name: AddChannelByID :exec
INSERT INTO channels (tg_peer_id, username, title)
VALUES ($1, '', '')
ON CONFLICT (tg_peer_id) WHERE tg_peer_id != 0 DO UPDATE SET is_active = TRUE;

-- name: AddChannelByInviteLink :exec
INSERT INTO channels (tg_peer_id, username, title, invite_link)
VALUES (0, '', '', $1)
ON CONFLICT (invite_link) DO UPDATE SET is_active = TRUE;

-- name: UpdateChannel :exec
UPDATE channels SET tg_peer_id = $2, title = $3, access_hash = $4, username = $5, description = $6, category = $7, tone = $8, update_freq = $9 WHERE id = $1;

-- name: UpdateChannelLastMessageID :exec
UPDATE channels SET last_tg_message_id = $2 WHERE id = $1;

-- name: DeactivateChannel :exec
UPDATE channels SET is_active = FALSE WHERE username = $1 OR '@' || username = $1 OR tg_peer_id::text = $1;

-- name: DeactivateChannelByID :exec
UPDATE channels SET is_active = FALSE WHERE id = $1;

-- name: GetUnprocessedMessages :many
-- Uses FOR UPDATE SKIP LOCKED to prevent multiple workers from claiming the same messages.
-- Atomically claims messages by setting processing_started_at.
WITH eligible AS (
    SELECT rm.id
    FROM raw_messages rm
    LEFT JOIN items i ON rm.id = i.raw_message_id
    WHERE (rm.processed_at IS NULL AND rm.processing_started_at IS NULL)
       OR (i.status IN ('error', 'retry') AND i.retry_count < 5 AND (i.next_retry_at IS NULL OR i.next_retry_at < now()))
    ORDER BY rm.tg_date ASC
    LIMIT $1
    FOR UPDATE OF rm SKIP LOCKED
),
claimed AS (
    UPDATE raw_messages rm
    SET processing_started_at = now()
    FROM eligible
    WHERE rm.id = eligible.id
    RETURNING rm.id
)
SELECT rm.id, rm.channel_id, rm.tg_message_id, rm.tg_date, rm.text, rm.entities_json, rm.media_json, rm.media_data, rm.canonical_hash, rm.is_forward,
       c.title as channel_title, c.context as channel_context, c.description as channel_description,
       c.category as channel_category, c.tone as channel_tone, c.update_freq as channel_update_freq,
       c.relevance_threshold as channel_relevance_threshold, c.importance_threshold as channel_importance_threshold,
       c.importance_weight as channel_importance_weight,
       c.auto_relevance_enabled as channel_auto_relevance_enabled,
       c.relevance_threshold_delta as channel_relevance_threshold_delta
FROM raw_messages rm
JOIN channels c ON rm.channel_id = c.id
WHERE rm.id IN (SELECT id FROM claimed)
ORDER BY rm.tg_date ASC;

-- name: UpdateChannelContext :exec
UPDATE channels SET context = $2 WHERE username = $1 OR '@' || username = $1 OR tg_peer_id::text = $1;

-- name: UpdateChannelMetadata :exec
UPDATE channels SET category = $2, tone = $3, update_freq = $4, relevance_threshold = $5, importance_threshold = $6 WHERE username = $1 OR '@' || username = $1 OR tg_peer_id::text = $1;

-- name: MarkAsProcessed :exec
UPDATE raw_messages SET processed_at = now(), processing_started_at = NULL WHERE id = $1;

-- name: ReleaseClaimedMessage :exec
-- Releases a claimed message so it can be picked up by another worker (used on error)
UPDATE raw_messages SET processing_started_at = NULL WHERE id = $1;

-- name: RecoverStuckPipelineMessages :execrows
-- Recovers messages that were claimed but not processed within the timeout.
-- This handles cases where a worker crashed after claiming messages.
UPDATE raw_messages
SET processing_started_at = NULL
WHERE processing_started_at IS NOT NULL
  AND processed_at IS NULL
  AND processing_started_at < now() - $1::interval;

-- name: SaveItem :one
INSERT INTO items (raw_message_id, relevance_score, importance_score, topic, summary, language, status, retry_count, next_retry_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, 0, NULL)
ON CONFLICT (raw_message_id) DO UPDATE SET
    relevance_score = $2, importance_score = $3, topic = $4, summary = $5, language = $6, status = $7,
    retry_count = 0, next_retry_at = NULL, error_json = NULL
RETURNING id;

-- name: SaveItemError :exec
INSERT INTO items (raw_message_id, status, error_json, retry_count, next_retry_at)
VALUES ($1, 'error', $2, 1, now() + interval '1 minute')
ON CONFLICT (raw_message_id) DO UPDATE SET 
    status = 'error', 
    error_json = $2,
    retry_count = items.retry_count + 1,
    next_retry_at = now() + (power(2, items.retry_count) * interval '1 minute');

-- name: DigestExists :one
SELECT EXISTS(
    SELECT 1 FROM digests 
    WHERE window_start = $1 AND window_end = $2 
    AND (status = 'posted' OR (status = 'error' AND posted_at > now() - interval '1 hour'))
);

-- name: SaveDigestError :exec
INSERT INTO digests (window_start, window_end, posted_chat_id, status, error_json, posted_at)
VALUES ($1, $2, $3, 'error', $4, now())
ON CONFLICT (window_start, window_end) DO UPDATE SET
    posted_chat_id = $3, status = 'error', error_json = $4, posted_at = now()
    WHERE digests.status != 'posted';

-- name: ClearDigestErrors :exec
DELETE FROM digests WHERE status = 'error';

-- name: GetItemsForWindow :many
SELECT i.id, i.raw_message_id, i.relevance_score, i.importance_score, i.topic, i.summary, i.language, i.status, rm.tg_date, c.username as source_channel, c.title as source_channel_title, c.tg_peer_id as source_channel_id, rm.tg_message_id as source_msg_id, e.embedding
FROM items i
JOIN raw_messages rm ON i.raw_message_id = rm.id
JOIN channels c ON rm.channel_id = c.id
LEFT JOIN embeddings e ON i.id = e.item_id
WHERE rm.tg_date >= $1 AND rm.tg_date < $2
  AND i.status = 'ready'
  AND i.importance_score >= COALESCE(c.importance_threshold, $3)
  AND i.digested_at IS NULL
ORDER BY i.importance_score DESC, i.relevance_score DESC
LIMIT $4;

-- name: GetItemsForWindowWithMedia :many
SELECT i.id, i.raw_message_id, i.relevance_score, i.importance_score, i.topic, i.summary, i.language, i.status, rm.tg_date, rm.media_data, c.username as source_channel, c.title as source_channel_title, c.tg_peer_id as source_channel_id, rm.tg_message_id as source_msg_id, e.embedding
FROM items i
JOIN raw_messages rm ON i.raw_message_id = rm.id
JOIN channels c ON rm.channel_id = c.id
LEFT JOIN embeddings e ON i.id = e.item_id
WHERE rm.tg_date >= $1 AND rm.tg_date < $2
  AND i.status = 'ready'
  AND i.importance_score >= COALESCE(c.importance_threshold, $3)
  AND i.digested_at IS NULL
ORDER BY i.importance_score DESC, i.relevance_score DESC
LIMIT $4;

-- name: CountItemsInWindow :one
SELECT COUNT(*) FROM items i
JOIN raw_messages rm ON i.raw_message_id = rm.id
WHERE rm.tg_date >= $1 AND rm.tg_date < $2;

-- name: CountReadyItemsInWindow :one
SELECT COUNT(*) FROM items i
JOIN raw_messages rm ON i.raw_message_id = rm.id
WHERE rm.tg_date >= $1 AND rm.tg_date < $2 AND i.status = 'ready' AND i.digested_at IS NULL;

-- name: MarkItemsAsDigested :exec
UPDATE items SET digested_at = now() WHERE id = ANY($1::uuid[]);

-- name: SaveDigest :one
INSERT INTO digests (id, window_start, window_end, posted_chat_id, posted_msg_id, status, posted_at)
VALUES ($1, $2, $3, $4, $5, 'posted', now())
ON CONFLICT (window_start, window_end) DO UPDATE SET
    posted_chat_id = $4, posted_msg_id = $5, status = 'posted', posted_at = now()
RETURNING id;

-- name: SaveDigestEntry :exec
INSERT INTO digest_entries (digest_id, title, body, sources_json)
VALUES ($1, $2, $3, $4);

-- name: FindSimilarItem :one
SELECT item_id FROM embeddings
WHERE (embedding <=> @embedding::vector) < @threshold::float8
  AND created_at > @min_created_at::timestamptz
ORDER BY embedding <=> @embedding::vector
LIMIT 1;

-- name: SaveEmbedding :exec
INSERT INTO embeddings (item_id, embedding)
VALUES (@item_id, @embedding::vector)
ON CONFLICT (item_id) DO UPDATE SET embedding = @embedding::vector;

-- name: GetActiveFilters :many
SELECT id, type, pattern, is_active FROM filters WHERE is_active = TRUE;

-- name: AddFilter :exec
INSERT INTO filters (type, pattern) VALUES ($1, $2);

-- name: DeactivateFilter :exec
UPDATE filters SET is_active = FALSE WHERE pattern = $1;

-- name: CreateCluster :one
INSERT INTO clusters (window_start, window_end, topic)
VALUES ($1, $2, $3)
RETURNING id;

-- name: DeleteClustersForWindow :exec
DELETE FROM clusters WHERE window_start = $1 AND window_end = $2;

-- name: AddToCluster :exec
INSERT INTO cluster_items (cluster_id, item_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: GetClustersForWindow :many
SELECT c.id as cluster_id, c.topic as cluster_topic, i.id as item_id, i.summary as item_summary, ch.username as channel_username, ch.tg_peer_id as channel_peer_id, rm.tg_message_id as rm_msg_id
FROM clusters c
JOIN cluster_items ci ON c.id = ci.cluster_id
JOIN items i ON ci.item_id = i.id
JOIN raw_messages rm ON i.raw_message_id = rm.id
JOIN channels ch ON rm.channel_id = ch.id
WHERE c.window_start = $1 AND c.window_end = $2
ORDER BY c.id;

-- name: GetItemEmbedding :one
SELECT embedding::text FROM embeddings WHERE item_id = $1;

-- name: SaveSetting :exec
INSERT INTO settings (key, value)
VALUES ($1, $2)
ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = now();

-- name: GetRecentErrors :many
SELECT i.id, i.raw_message_id, i.error_json, i.created_at, c.username as channel_username, c.tg_peer_id as channel_peer_id, rm.tg_message_id as source_msg_id
FROM items i
JOIN raw_messages rm ON i.raw_message_id = rm.id
JOIN channels c ON rm.channel_id = c.id
WHERE i.status = 'error'
ORDER BY i.created_at DESC
LIMIT $1;

-- name: RetryFailedItems :exec
UPDATE items SET status = 'retry', retry_count = 0, next_retry_at = now() WHERE status = 'error';

-- name: RetryItem :exec
UPDATE items SET status = 'retry', retry_count = 0, next_retry_at = now() WHERE id = $1 AND status = 'error';

-- name: GetItemByID :one
SELECT id, raw_message_id, relevance_score, importance_score, topic, summary, language, status, error_json, created_at, digested_at
FROM items WHERE id = $1;

-- name: GetSetting :one
SELECT value FROM settings WHERE key = $1;

-- name: DeleteSetting :exec
DELETE FROM settings WHERE key = $1;

-- name: GetAllSettings :many
SELECT key, value FROM settings;

-- name: GetBacklogCount :one
SELECT count(*) FROM raw_messages WHERE processed_at IS NULL;

-- name: GetChannelStats :many
SELECT rm.channel_id, 
       (COUNT(i.id) FILTER (WHERE i.status = 'ready')::float4 * 100.0 / NULLIF(COUNT(rm.id), 0)::float4)::float4 as conversion_rate,
       COALESCE(AVG(i.relevance_score) FILTER (WHERE i.status = 'ready'), 0)::float4 as avg_relevance,
       COALESCE(STDDEV(i.relevance_score) FILTER (WHERE i.status = 'ready'), 0)::float4 as stddev_relevance,
       COALESCE(AVG(i.importance_score) FILTER (WHERE i.status = 'ready'), 0)::float4 as avg_importance,
       COALESCE(STDDEV(i.importance_score) FILTER (WHERE i.status = 'ready'), 0)::float4 as stddev_importance
FROM raw_messages rm
LEFT JOIN items i ON rm.id = i.raw_message_id
WHERE rm.tg_date > now() - interval '7 days'
GROUP BY rm.channel_id;

-- name: CheckStrictDuplicate :one
SELECT EXISTS(
    SELECT 1 FROM raw_messages rm
    LEFT JOIN items i ON rm.id = i.raw_message_id
    WHERE rm.canonical_hash = $1 AND rm.id != $2 
    AND (rm.processed_at IS NOT NULL AND (i.status IS NULL OR i.status != 'error'))
);

-- name: TryAcquireAdvisoryLock :one
SELECT pg_try_advisory_lock($1);

-- name: ReleaseAdvisoryLock :exec
SELECT pg_advisory_unlock($1);

-- name: GetRecentMessagesForChannel :many
SELECT text, tg_date FROM raw_messages
WHERE channel_id = $1 AND processed_at IS NOT NULL AND tg_date < $2
ORDER BY tg_date DESC
LIMIT $3;

-- name: GetLastPostedDigest :one
SELECT window_start, window_end, posted_at FROM digests WHERE status = 'posted' ORDER BY posted_at DESC LIMIT 1;

-- name: CountActiveChannels :one
SELECT COUNT(*) FROM channels WHERE is_active = TRUE;

-- name: CountRecentlyActiveChannels :one
SELECT COUNT(DISTINCT channel_id) FROM raw_messages WHERE tg_date > now() - interval '24 hours';

-- name: CountReadyItems :one
SELECT COUNT(*) FROM items WHERE status = 'ready' AND digested_at IS NULL;

-- name: SaveRating :exec
INSERT INTO digest_ratings (digest_id, user_id, rating, feedback)
VALUES ($1, $2, $3, $4)
ON CONFLICT (digest_id, user_id) DO UPDATE SET rating = $3, feedback = $4;

-- name: SaveItemRating :exec
INSERT INTO item_ratings (item_id, user_id, rating, feedback)
VALUES ($1, $2, $3, $4)
ON CONFLICT (item_id, user_id) DO UPDATE SET rating = $3, feedback = $4;

-- name: AddSettingHistory :exec
INSERT INTO setting_history (key, old_value, new_value, changed_by)
VALUES ($1, $2, $3, $4);

-- name: GetRecentSettingHistory :many
SELECT key, old_value, new_value, changed_by, changed_at
FROM setting_history
ORDER BY changed_at DESC
LIMIT $1;

-- name: GetLinkCache :one
SELECT * FROM link_cache WHERE url = $1;

-- name: SaveLinkCache :one
INSERT INTO link_cache (
    url, domain, link_type, title, content, author, published_at,
    description, image_url, word_count,
    channel_username, channel_title, channel_id, message_id,
    views, forwards, has_media, media_type,
    status, error_message, language, resolved_at, expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    $8, $9, $10,
    $11, $12, $13, $14,
    $15, $16, $17, $18,
    $19, $20, $21, $22, $23
)
ON CONFLICT (url) DO UPDATE SET
    title = EXCLUDED.title,
    content = EXCLUDED.content,
    author = EXCLUDED.author,
    published_at = EXCLUDED.published_at,
    description = EXCLUDED.description,
    image_url = EXCLUDED.image_url,
    word_count = EXCLUDED.word_count,
    channel_username = EXCLUDED.channel_username,
    channel_title = EXCLUDED.channel_title,
    channel_id = EXCLUDED.channel_id,
    message_id = EXCLUDED.message_id,
    views = EXCLUDED.views,
    forwards = EXCLUDED.forwards,
    has_media = EXCLUDED.has_media,
    media_type = EXCLUDED.media_type,
    status = EXCLUDED.status,
    error_message = EXCLUDED.error_message,
    language = EXCLUDED.language,
    resolved_at = EXCLUDED.resolved_at,
    expires_at = EXCLUDED.expires_at
RETURNING id;

-- name: LinkMessageToLink :exec
INSERT INTO message_links (raw_message_id, link_cache_id, position)
VALUES ($1, $2, $3)
ON CONFLICT (raw_message_id, link_cache_id) DO NOTHING;

-- name: GetDigestCoverImage :one
SELECT rm.media_data
FROM items i
JOIN raw_messages rm ON i.raw_message_id = rm.id
WHERE rm.tg_date >= $1 AND rm.tg_date < $2
  AND i.status = 'ready'
  AND i.importance_score >= $3
  AND rm.media_data IS NOT NULL
  AND length(rm.media_data) > 0
ORDER BY i.importance_score DESC, i.relevance_score DESC
LIMIT 1;

-- name: GetLinksForMessage :many
SELECT lc.* 
FROM link_cache lc
JOIN message_links ml ON lc.id = ml.link_cache_id
WHERE ml.raw_message_id = $1
ORDER BY ml.position;

-- name: GetChannelByPeerID :one
SELECT id, tg_peer_id, username, title, is_active, added_at, added_by_tg_user, access_hash, invite_link, context, description, last_tg_message_id, category, tone, update_freq, relevance_threshold, importance_threshold, importance_weight, auto_weight_enabled, weight_override, weight_override_reason, weight_updated_at, weight_updated_by, auto_relevance_enabled, relevance_threshold_delta FROM channels WHERE tg_peer_id = $1;

-- name: GetChannelByID :one
SELECT id, tg_peer_id, username, invite_link, is_active FROM channels WHERE id = $1;

-- Channel Discovery queries

-- name: UpsertDiscoveredChannelByUsername :exec
INSERT INTO discovered_channels (username, title, source_type, discovered_from_channel_id, max_views, max_forwards, engagement_score)
VALUES ($1, $2, $3, $4, $5, $6, ln(1 + COALESCE($5, 0)) * 0.3 + ln(1 + COALESCE($6, 0)) * 0.5 + ln(2) * 0.2)
ON CONFLICT (username) WHERE username IS NOT NULL AND username != ''
DO UPDATE SET
    discovery_count = discovered_channels.discovery_count + 1,
    last_seen_at = now(),
    title = COALESCE(NULLIF($2, ''), discovered_channels.title),
    max_views = GREATEST(discovered_channels.max_views, COALESCE($5, 0)),
    max_forwards = GREATEST(discovered_channels.max_forwards, COALESCE($6, 0)),
    engagement_score = ln(1 + GREATEST(discovered_channels.max_views, COALESCE($5, 0))) * 0.3 +
                       ln(1 + GREATEST(discovered_channels.max_forwards, COALESCE($6, 0))) * 0.5 +
                       ln(1 + discovered_channels.discovery_count + 1) * 0.2;

-- name: UpsertDiscoveredChannelByPeerID :exec
INSERT INTO discovered_channels (tg_peer_id, title, source_type, discovered_from_channel_id, max_views, max_forwards, engagement_score, access_hash)
VALUES ($1, $2, $3, $4, $5, $6, ln(1 + COALESCE($5, 0)) * 0.3 + ln(1 + COALESCE($6, 0)) * 0.5 + ln(2) * 0.2, $7)
ON CONFLICT (tg_peer_id) WHERE tg_peer_id != 0
DO UPDATE SET
    discovery_count = discovered_channels.discovery_count + 1,
    last_seen_at = now(),
    title = COALESCE(NULLIF($2, ''), discovered_channels.title),
    max_views = GREATEST(discovered_channels.max_views, COALESCE($5, 0)),
    max_forwards = GREATEST(discovered_channels.max_forwards, COALESCE($6, 0)),
    engagement_score = ln(1 + GREATEST(discovered_channels.max_views, COALESCE($5, 0))) * 0.3 +
                       ln(1 + GREATEST(discovered_channels.max_forwards, COALESCE($6, 0))) * 0.5 +
                       ln(1 + discovered_channels.discovery_count + 1) * 0.2,
    access_hash = COALESCE(NULLIF($7, 0), discovered_channels.access_hash);

-- name: UpsertDiscoveredChannelByInvite :exec
INSERT INTO discovered_channels (invite_link, source_type, discovered_from_channel_id, max_views, max_forwards, engagement_score)
VALUES ($1, $2, $3, $4, $5, ln(1 + COALESCE($4, 0)) * 0.3 + ln(1 + COALESCE($5, 0)) * 0.5 + ln(2) * 0.2)
ON CONFLICT (invite_link) WHERE invite_link IS NOT NULL AND invite_link != ''
DO UPDATE SET
    discovery_count = discovered_channels.discovery_count + 1,
    last_seen_at = now(),
    max_views = GREATEST(discovered_channels.max_views, COALESCE($4, 0)),
    max_forwards = GREATEST(discovered_channels.max_forwards, COALESCE($5, 0)),
    engagement_score = ln(1 + GREATEST(discovered_channels.max_views, COALESCE($4, 0))) * 0.3 +
                       ln(1 + GREATEST(discovered_channels.max_forwards, COALESCE($5, 0))) * 0.5 +
                       ln(1 + discovered_channels.discovery_count + 1) * 0.2;

-- name: GetPendingDiscoveries :many
-- Only return actionable discoveries (with username for approve/reject)
-- Uses DISTINCT ON to deduplicate multiple rows for the same channel (discovered via different identifiers)
SELECT * FROM (
  SELECT DISTINCT ON (dc.username)
    dc.id, dc.username, dc.tg_peer_id, dc.invite_link, dc.title, dc.description, dc.source_type,
    dc.discovery_count, dc.first_seen_at, dc.last_seen_at, dc.max_views, dc.max_forwards, dc.engagement_score
  FROM discovered_channels dc
  WHERE dc.status = 'pending'
    AND dc.username IS NOT NULL AND dc.username != ''
    AND dc.matched_channel_id IS NULL
    AND dc.discovery_count >= $1
    AND dc.engagement_score >= $2
    AND NOT EXISTS (
      SELECT 1
      FROM channels c
      WHERE c.is_active = TRUE AND (
        (c.username != '' AND lower(c.username) = lower(dc.username)) OR
        (c.username != '' AND lower('@' || c.username) = lower(dc.username)) OR
        (c.tg_peer_id = dc.tg_peer_id AND dc.tg_peer_id != 0 AND c.tg_peer_id != 0) OR
        (c.invite_link = dc.invite_link AND dc.invite_link != '' AND c.invite_link != '')
      )
    )
  ORDER BY dc.username, dc.engagement_score DESC
) deduped
ORDER BY engagement_score DESC, discovery_count DESC, last_seen_at DESC
LIMIT $3;

-- name: GetPendingDiscoveriesForFiltering :many
-- Same as GetPendingDiscoveries but without a limit, used for keyword filtering stats.
SELECT * FROM (
  SELECT DISTINCT ON (dc.username)
    dc.id, dc.username, dc.tg_peer_id, dc.invite_link, dc.title, dc.description, dc.source_type,
    dc.discovery_count, dc.first_seen_at, dc.last_seen_at, dc.max_views, dc.max_forwards, dc.engagement_score
  FROM discovered_channels dc
  WHERE dc.status = 'pending'
    AND dc.username IS NOT NULL AND dc.username != ''
    AND dc.matched_channel_id IS NULL
    AND dc.discovery_count >= $1
    AND dc.engagement_score >= $2
    AND NOT EXISTS (
      SELECT 1
      FROM channels c
      WHERE c.is_active = TRUE AND (
        (c.username != '' AND lower(c.username) = lower(dc.username)) OR
        (c.username != '' AND lower('@' || c.username) = lower(dc.username)) OR
        (c.tg_peer_id = dc.tg_peer_id AND dc.tg_peer_id != 0 AND c.tg_peer_id != 0) OR
        (c.invite_link = dc.invite_link AND dc.invite_link != '' AND c.invite_link != '')
      )
    )
  ORDER BY dc.username, dc.engagement_score DESC
) deduped
ORDER BY engagement_score DESC, discovery_count DESC, last_seen_at DESC;

-- name: GetRejectedDiscoveries :many
SELECT dc.id, dc.username, dc.tg_peer_id, dc.invite_link, dc.title, dc.description, dc.source_type,
       dc.discovery_count, dc.first_seen_at, dc.last_seen_at, dc.max_views, dc.max_forwards, dc.engagement_score
FROM discovered_channels dc
WHERE dc.status = 'rejected'
ORDER BY dc.last_seen_at DESC
LIMIT $1;

-- name: GetDiscoveryByUsername :one
SELECT dc.id, dc.username, dc.tg_peer_id, dc.invite_link, dc.title, dc.description, dc.source_type,
       dc.discovery_count, dc.first_seen_at, dc.last_seen_at, dc.max_views, dc.max_forwards, dc.engagement_score,
       dc.status, dc.matched_channel_id
FROM discovered_channels dc
WHERE lower(dc.username) = lower($1) OR lower('@' || dc.username) = lower($1)
ORDER BY dc.last_seen_at DESC
LIMIT 1;

-- name: UpdateDiscoveryStatus :exec
UPDATE discovered_channels
SET status = $2, status_changed_at = now(), status_changed_by = $3
WHERE id = $1;

-- name: UpdateDiscoveryStatusByUsername :exec
-- Rejects the target row AND any related rows that share peer_id or invite_link
-- This prevents the same channel from reappearing via different discovery paths
WITH target AS (
    SELECT src.username AS t_username, src.tg_peer_id AS t_peer_id, src.invite_link AS t_invite
    FROM discovered_channels src
    WHERE lower(src.username) = lower($1) OR lower('@' || src.username) = lower($1)
    LIMIT 1
)
UPDATE discovered_channels dc
SET status = $2, status_changed_at = now(), status_changed_by = $3
FROM target
WHERE (dc.username != '' AND target.t_username != '' AND lower(dc.username) = lower(target.t_username))
   OR (dc.tg_peer_id = target.t_peer_id AND dc.tg_peer_id != 0 AND target.t_peer_id != 0)
   OR (dc.invite_link = target.t_invite AND dc.invite_link != '' AND target.t_invite != '');

-- name: GetDiscoveryStats :one
SELECT
    COUNT(*) FILTER (WHERE status = 'pending' AND username IS NOT NULL AND username != '') as pending_count,
    COUNT(*) FILTER (WHERE status = 'pending' AND (username IS NULL OR username = '')) as unresolved_count,
    COUNT(*) FILTER (WHERE status = 'approved') as approved_count,
    COUNT(*) FILTER (WHERE status = 'rejected') as rejected_count,
    COUNT(*) FILTER (WHERE status = 'added') as added_count,
    COUNT(*) as total_count,
    COALESCE(SUM(discovery_count), 0) as total_discoveries
FROM discovered_channels;

-- name: GetDiscoveryFilterStats :one
SELECT
    COUNT(*) FILTER (
        WHERE status = 'pending'
          AND matched_channel_id IS NOT NULL
    ) as matched_channel_id_count,
    COUNT(*) FILTER (
        WHERE status = 'pending'
          AND matched_channel_id IS NULL
          AND username IS NOT NULL AND username != ''
          AND (discovery_count < $1 OR COALESCE(engagement_score, 0) < $2)
    ) as below_threshold_count,
    COUNT(*) FILTER (
        WHERE status = 'pending'
          AND matched_channel_id IS NULL
          AND username IS NOT NULL AND username != ''
          AND discovery_count >= $1
          AND COALESCE(engagement_score, 0) >= $2
          AND EXISTS (
            SELECT 1
            FROM channels c
            WHERE c.is_active = TRUE AND (
              (c.username != '' AND lower(c.username) = lower(discovered_channels.username)) OR
              (c.username != '' AND lower('@' || c.username) = lower(discovered_channels.username)) OR
              (c.tg_peer_id = discovered_channels.tg_peer_id AND discovered_channels.tg_peer_id != 0 AND c.tg_peer_id != 0) OR
              (c.invite_link = discovered_channels.invite_link AND discovered_channels.invite_link != '' AND c.invite_link != '')
            )
          )
    ) as already_tracked_count
FROM discovered_channels;

-- name: IsChannelTracked :one
SELECT EXISTS(
    SELECT 1 FROM channels
    WHERE is_active = TRUE AND (
        (username != '' AND lower(username) = lower($1)) OR
        (tg_peer_id = $2 AND tg_peer_id != 0) OR
        (invite_link = $3 AND invite_link != '')
    )
);

-- name: IsChannelDiscoveredRejected :one
SELECT EXISTS(
    SELECT 1 FROM discovered_channels
    WHERE status = 'rejected' AND (
        (username != '' AND lower(username) = lower($1)) OR
        (tg_peer_id = $2 AND tg_peer_id != 0) OR
        (invite_link = $3 AND invite_link != '')
    )
);

-- name: CheckAndMarkDiscoveriesExtracted :one
UPDATE raw_messages
SET discoveries_extracted = TRUE
WHERE channel_id = $1 AND tg_message_id = $2 AND (discoveries_extracted IS NULL OR discoveries_extracted = FALSE)
RETURNING id;

-- name: GetDiscoveriesNeedingResolution :many
SELECT id, tg_peer_id, COALESCE(access_hash, 0) as access_hash
FROM discovered_channels
WHERE status = 'pending'
  AND tg_peer_id != 0
  AND (title IS NULL OR title = '')
  AND (username IS NULL OR username = '')
  AND (resolution_attempts IS NULL OR resolution_attempts < 3)
  AND (last_resolution_attempt IS NULL OR last_resolution_attempt < now() - interval '1 hour')
ORDER BY discovery_count DESC
LIMIT $1;

-- name: UpdateDiscoveryChannelInfo :exec
UPDATE discovered_channels
SET title = COALESCE(NULLIF(@title, ''), title),
    username = COALESCE(NULLIF(@username, ''), username),
    description = COALESCE(NULLIF(@description, ''), description),
    resolution_attempts = 0
WHERE id = @id;

-- name: IncrementDiscoveryResolutionAttempts :exec
UPDATE discovered_channels
SET resolution_attempts = COALESCE(resolution_attempts, 0) + 1,
    last_resolution_attempt = now()
WHERE id = $1;

-- name: GetInviteLinkDiscoveriesNeedingResolution :many
SELECT id, invite_link
FROM discovered_channels
WHERE status = 'pending'
  AND invite_link IS NOT NULL AND invite_link != ''
  AND (title IS NULL OR title = '')
  AND (resolution_attempts IS NULL OR resolution_attempts < 3)
  AND (last_resolution_attempt IS NULL OR last_resolution_attempt < now() - interval '1 hour')
ORDER BY discovery_count DESC
LIMIT $1;

-- name: UpdateDiscoveryFromInvite :exec
UPDATE discovered_channels
SET title = COALESCE(NULLIF(@title, ''), title),
    username = COALESCE(NULLIF(@username, ''), username),
    description = COALESCE(NULLIF(@description, ''), description),
    tg_peer_id = COALESCE(NULLIF(@tg_peer_id::bigint, 0::bigint), tg_peer_id),
    access_hash = COALESCE(NULLIF(@access_hash::bigint, 0::bigint), access_hash),
    resolution_attempts = 0
WHERE id = @id;

-- Channel importance weight queries

-- name: UpdateChannelWeight :one
UPDATE channels
SET importance_weight = $2,
    auto_weight_enabled = $3,
    weight_override = $4,
    weight_override_reason = $5,
    weight_updated_at = NOW(),
    weight_updated_by = $6
WHERE username = $1 OR '@' || username = $1 OR tg_peer_id::text = $1
RETURNING username, title;

-- name: UpdateChannelRelevanceDelta :exec
UPDATE channels
SET relevance_threshold_delta = $2,
    auto_relevance_enabled = $3
WHERE id = $1;

-- name: GetChannelWeight :one
SELECT username, title, importance_weight, auto_weight_enabled, weight_override, weight_override_reason, weight_updated_at
FROM channels
WHERE username = $1 OR '@' || username = $1 OR tg_peer_id::text = $1;

-- name: GetItemRatingsSince :many
SELECT rm.channel_id, ir.rating, ir.created_at
FROM item_ratings ir
JOIN items i ON ir.item_id = i.id
JOIN raw_messages rm ON i.raw_message_id = rm.id
WHERE ir.created_at >= $1;

-- Channel stats queries

-- name: UpsertChannelStats :exec
INSERT INTO channel_stats (channel_id, period_start, period_end, messages_received, items_created, items_digested, avg_importance, avg_relevance)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (channel_id, period_start, period_end) DO UPDATE SET
    messages_received = channel_stats.messages_received + EXCLUDED.messages_received,
    items_created = channel_stats.items_created + EXCLUDED.items_created,
    items_digested = channel_stats.items_digested + EXCLUDED.items_digested,
    avg_importance = EXCLUDED.avg_importance,
    avg_relevance = EXCLUDED.avg_relevance,
    updated_at = NOW();

-- name: GetChannelStatsForPeriod :many
SELECT cs.*, c.username, c.title
FROM channel_stats cs
JOIN channels c ON cs.channel_id = c.id
WHERE cs.period_start >= $1 AND cs.period_end <= $2;

-- name: GetChannelStatsRolling :one
SELECT
    COALESCE(SUM(messages_received), 0)::int as total_messages,
    COALESCE(SUM(items_created), 0)::int as total_items_created,
    COALESCE(SUM(items_digested), 0)::int as total_items_digested,
    COALESCE(AVG(avg_importance), 0)::float as avg_importance,
    COALESCE(AVG(avg_relevance), 0)::float as avg_relevance
FROM channel_stats
WHERE channel_id = $1 AND period_start >= $2;

-- name: GetChannelsForAutoWeight :many
SELECT id, username, title, importance_weight, auto_weight_enabled, weight_override
FROM channels
WHERE is_active = TRUE AND auto_weight_enabled = TRUE AND weight_override = FALSE;

-- name: UpdateChannelAutoWeight :exec
UPDATE channels
SET importance_weight = $2,
    weight_updated_at = NOW()
WHERE id = $1 AND weight_override = FALSE;

-- name: GetChannelStatsForWindow :many
SELECT
    c.id as channel_id,
    COUNT(DISTINCT rm.id) as messages_received,
    COUNT(DISTINCT CASE WHEN i.status = 'ready' OR i.status = 'digested' THEN i.id END) as items_created,
    COUNT(DISTINCT CASE WHEN i.status = 'digested' THEN i.id END) as items_digested,
    COALESCE(AVG(CASE WHEN i.status IN ('ready', 'digested') THEN i.importance_score END), 0) as avg_importance,
    COALESCE(AVG(CASE WHEN i.status IN ('ready', 'digested') THEN i.relevance_score END), 0) as avg_relevance
FROM channels c
LEFT JOIN raw_messages rm ON rm.channel_id = c.id AND rm.tg_date >= $1 AND rm.tg_date < $2
LEFT JOIN items i ON i.raw_message_id = rm.id
WHERE c.is_active = TRUE
GROUP BY c.id
HAVING COUNT(DISTINCT rm.id) > 0;

-- name: RetryFailedEnrichmentItems :exec
UPDATE enrichment_queue
SET status = 'pending', error_message = NULL, attempt_count = 0, next_retry_at = NULL
WHERE status = 'error';

-- name: GetEnrichmentQueueStats :many
SELECT status, COUNT(*) as count FROM enrichment_queue GROUP BY status;

-- name: GetEnrichmentErrors :many
SELECT id, item_id, error_message, attempt_count, created_at
FROM enrichment_queue
WHERE status = 'error'
ORDER BY created_at DESC
LIMIT $1;
