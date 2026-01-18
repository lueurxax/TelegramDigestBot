# Discovery Filtering Improvements

> **Status: Complete** (January 2026)
>
> See [docs/features/discovery.md](../features/discovery.md) for user-facing documentation.

## Implementation Status

| Feature | Status | Notes |
|---------|--------|-------|
| 1) Canonical identity matching | Done | `matched_channel_id` column, `markDiscoveryAdded` sets it |
| 2) Duplicate row handling | Done | Backfill via `/discover cleanup`, DISTINCT ON deduplication |
| 3) Signal thresholds | Done | `discovery_min_seen`, `discovery_min_engagement` settings |
| 4) Description keyword filters | Done | `discovery_description_allow`, `discovery_description_deny` settings |
| 5) Source hygiene | Done | Inactive source filtering, self-discovery prevention |
| 6) Admin controls | Done | `/discover reject`, `show-rejected`, `cleanup` |
| Reconciliation job | Done | Runs at startup + every 6 hours |
| Schema changes | Done | `matched_channel_id`, `description` columns with indexes |
| Filter summary in `/discover` | Done | Shows applied thresholds |
| `/discover preview` | Done | Includes keyword filter reasons |
| Time-series metrics | Done | Pending/actionable gauges + approval counters |
| Unit tests for keyword matching | Done | `discovery_filters_test.go` |

---

## Problem
The `/discover` list still contains entries that are already tracked or low quality. This creates noise and slows approvals. Current filtering only checks status and username presence, so discoveries created via a different identifier (peer ID, invite link, casing) can slip through.

## Goals
- Keep `/discover` actionable: only channels that are not already tracked and meet basic signal thresholds.
- Preserve discovery stats and history (do not delete rows).
- Make filtering rules explicit and configurable.

## Proposal
1) Canonical identity matching (continuous)
- Add `matched_channel_id` (UUID, nullable) to `discovered_channels`.
- When a channel is added or resolved, set `matched_channel_id` on *all* discovery rows that match by username, peer ID, or invite link.
- Update status to `added` even if it was previously `rejected` (channel is now tracked).
- `/discover` excludes rows with `matched_channel_id` set, or rows that already match an active channel.
- Add a periodic reconciliation job (or reuse `markDiscoveryAdded`) to keep matches up to date.
  - Implementation note: `markDiscoveryAdded` must set `matched_channel_id` in addition to status updates.

2) Duplicate row handling
- Accept multiple rows per channel but ensure they all point to the same `matched_channel_id`.
- Add a cleanup step to backfill `matched_channel_id` for existing rows using identifier matches.
- Optional: merge duplicates by setting a canonical row ID and hiding the rest (non-goal for v1).

3) Signal thresholds
- Add settings:
  - `discovery_min_seen` (default: 2)
  - `discovery_min_engagement` (default: 50)
- `/discover` excludes entries below these thresholds, but `/discover stats` still counts them in totals.

Engagement score definition
- `max_views` and `max_forwards` are the highest values seen across discovery events for the channel.
- The score uses a log scale to keep large channels from dominating:
  - `engagement_score = 0.3 * ln(1 + max_views) + 0.5 * ln(1 + max_forwards) + 0.2 * ln(1 + discovery_count)`

4) Description keyword filters
- If enabled later, use settings:
  - `discovery_description_allow` (JSON array of strings, optional)
  - `discovery_description_deny` (JSON array of strings, optional)
- Manage via <code>/discover allow|deny</code> commands (add/remove/clear).
- Apply filters to a normalized text built from `title` + `description` (lowercased).
- Matching is case-insensitive substring match.
- If allow list is set, at least one keyword must match; if text is empty, exclude.
- If any deny keyword matches, exclude regardless of allow list.

5) Source hygiene
- Ignore discoveries sourced from channels that are inactive.
- Do not count a channel as "discovered from" itself (same tg_peer_id).

6) Admin controls
- `/discover ignore @user` sets status `rejected`.
- `/discover show-rejected [limit]` lets admins review rejected items.
- `/discover cleanup` backfills `matched_channel_id` for existing rows and marks them `added` when a tracked channel is found.

## Schema Changes
- `discovered_channels.matched_channel_id UUID NULL REFERENCES channels(id) ON DELETE SET NULL`
- `discovered_channels.description TEXT NULL` (from channel about/description when resolved)
- Index on `matched_channel_id`.
- Optional index for pending scans:
  - `CREATE INDEX idx_discovered_channels_pending ON discovered_channels (status, engagement_score DESC) WHERE status = 'pending';`

## Settings Storage
- Settings are stored in `settings.value` as JSON.
- `discovery_description_allow`: JSON array of strings (e.g., `["ai", "security"]`).
- `discovery_description_deny`: JSON array of strings (e.g., `["gambling", "crypto"]`).

## Query Changes
`GetPendingDiscoveries` should filter out:
- `matched_channel_id IS NOT NULL`
- entries below thresholds
- entries with matches in `channels` by username/peer_id/invite_link
- entries that fail description keyword filters

Example query (parameters provided by the service after reading settings). Keyword filtering is applied in Go to avoid expensive `unnest + LIKE` scans:
```sql
SELECT dc.id, dc.username, dc.tg_peer_id, dc.invite_link, dc.title, dc.description, dc.source_type,
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
      (c.username = dc.username AND c.username != '') OR
      ('@' || c.username = dc.username AND c.username != '') OR
      (c.tg_peer_id = dc.tg_peer_id AND dc.tg_peer_id != 0 AND c.tg_peer_id != 0) OR
      (c.invite_link = dc.invite_link AND dc.invite_link != '' AND c.invite_link != '')
    )
  )
ORDER BY dc.engagement_score DESC, dc.discovery_count DESC, dc.last_seen_at DESC
LIMIT $3;
```

Add `GetRejectedDiscoveries` for `/discover show-rejected [limit]`:
```sql
SELECT dc.id, dc.username, dc.tg_peer_id, dc.invite_link, dc.title, dc.description, dc.source_type,
       dc.discovery_count, dc.first_seen_at, dc.last_seen_at, dc.max_views, dc.max_forwards, dc.engagement_score
FROM discovered_channels dc
WHERE dc.status = 'rejected'
ORDER BY dc.last_seen_at DESC
LIMIT $1;
```

Handler integration sketch:
```go
minSeen := getSettingInt(ctx, "discovery_min_seen", 2)
minEngagement := getSettingInt(ctx, "discovery_min_engagement", 50)
allow := getSettingStringSlice(ctx, "discovery_description_allow")
deny := getSettingStringSlice(ctx, "discovery_description_deny")

rows, err := b.database.GetPendingDiscoveries(ctx, DiscoveriesLimit, minSeen, minEngagement)
if err != nil { /* handle */ }

discoveries := filterDiscoveriesByKeywords(rows, allow, deny)
```

## Performance Notes
- Apply keyword filtering in Go after the DB prefilter (status, thresholds, tracked check).
- If DB-side filtering is required later, consider a `discovered_channels.search_text` column with `lower(title || ' ' || description)` and a trigram index.

## Acceptance Criteria
- `/discover` does not list channels already tracked (by username, peer ID, or invite link).
- A tracked channel marks all matching discovery rows as `added` with `matched_channel_id`.
- Description keyword filters hide low-quality entries as configured.
- `/discover stats` totals are unchanged by filters.
- With `discovery_min_seen=2`, discoveries with `discovery_count=1` are excluded.
- With `discovery_min_engagement=50`, discoveries with `engagement_score < 50` are excluded.

## Instrumentation & UX
- Add filter reason counters to `/discover stats`:
  - `already_tracked`, `matched_channel_id`, `below_threshold`, `allow_miss`, `deny_hit`.
- `/discover preview @user` shows which filters apply and why an entry is hidden.
- Log reconciliation results (rows updated, matched, added) each run.
- Track time-series metrics: pending vs actionable counts and approval rate.
- Add unit tests for keyword matching (case, null description, allow+deny interaction).

## Rollback
- Remove new columns (`matched_channel_id`, `description`) and their indexes.
- Remove settings keys for description filters.
- Revert `GetPendingDiscoveries` to status + username filtering only.
  
## Already Implemented (Baseline)
- DISTINCT ON deduplication for discoveries (already in code).
- Cross-identifier rejection cascade.
- `markDiscoveryAdded` exists (needs to set `matched_channel_id`).
- `IsChannelTracked` already checks username, peer ID, and invite link.

## Edge Cases
- If a matched channel is deactivated, keep `matched_channel_id` but allow rediscovery once reactivated.
- Resolution attempts should continue respecting the existing backoff/attempt limit.

## Reconciliation Job
- Add to worker mode; run every 6 hours (and once at startup).
- Behavior: backfill `matched_channel_id`, mark matching rows `added`, and log counts.
- Failure: log and continue; no retry storm (next scheduled run will reattempt).

## `/discover cleanup` Behavior
- Idempotent: safe to run multiple times.
- Processes in batches of 100.
- Reports: "Updated X discoveries, matched Y to tracked channels".
- On error: log and continue with next batch.

## Rollout
- Migrate schema and add settings defaults.
- Run `/discover cleanup` once.
- Ensure `markDiscoveryAdded` (or equivalent) sets `matched_channel_id` for new adds.
- Monitor `/discover stats` for changes in actionable count.

## Implementation Checklist
- [x] Migration: add `description` and `matched_channel_id` columns with index and ON DELETE SET NULL.
- [x] Update discovery resolution to store `description` (channel about text).
- [x] Update `markDiscoveryAdded` to set `matched_channel_id`.
- [x] Add `GetRejectedDiscoveries` query for `/discover show-rejected`.
- [x] Add signal thresholds (`discovery_min_seen`, `discovery_min_engagement`).
- [x] Add source hygiene (inactive source filtering, self-discovery prevention).
- [x] Add `/discover cleanup` command.
- [x] Add reconciliation job (startup + every 6 hours).
- [x] Add `/discover preview` filter reason display.
- [x] Add time-series metrics tracking.
- [x] Add description keyword filters.
