# Channel Discovery System

The discovery system automatically finds new channels through forwards, mentions, and links in tracked channels. It provides an admin-facing workflow to review, approve, or reject candidates.

## Overview

Channels are discovered in three ways:
1. **Forwards** - Messages forwarded from untracked channels
2. **Mentions** - @username references in message text
3. **Links** - t.me links and invite links embedded in content

Each discovery is scored based on engagement metrics and can be filtered by configurable thresholds before appearing in the review queue.

---

## Identity Model

Channels can be identified by multiple attributes:

| Identifier | Description |
|------------|-------------|
| `username` | Public @handle (normalized to lowercase) |
| `tg_peer_id` | Telegram's numeric channel ID |
| `invite_link` | Private invite link (t.me/+xxx) |

The system matches discoveries to tracked channels using **any** of these identifiers. This prevents duplicates when the same channel appears via different paths (e.g., forwarded message vs. username mention).

### Matched Channel Linking

When a channel is added to tracking, all matching discovery rows are linked via `matched_channel_id`:
- Links discovery history to the tracked channel
- Prevents the same channel from reappearing in `/discover`
- Preserves discovery stats for analytics

---

## Engagement Scoring

Discoveries are ranked by an engagement score using a log scale to balance large and small channels:

```
engagement_score = 0.3 * ln(1 + max_views) + 0.5 * ln(1 + max_forwards) + 0.2 * ln(1 + discovery_count)
```

| Factor | Weight | Description |
|--------|--------|-------------|
| `max_views` | 0.3 | Highest view count seen |
| `max_forwards` | 0.5 | Highest forward count seen |
| `discovery_count` | 0.2 | Number of times discovered |

---

## Signal Thresholds

Configurable thresholds filter low-quality discoveries from the review queue:

| Setting | Default | Description |
|---------|---------|-------------|
| `discovery_min_seen` | 2 | Minimum discovery count |
| `discovery_min_engagement` | 50 | Minimum engagement score |

Discoveries below these thresholds are excluded from `/discover` but still counted in stats.

### Setting Thresholds

```
/discover minseen 3
/discover minengagement 75
```

---

## Source Hygiene

The system filters out low-quality discovery sources:

1. **Inactive sources** - Discoveries from deactivated channels are ignored
2. **Self-discovery** - A channel cannot "discover" itself via matching peer ID, username, or invite link

---

## Admin Commands

### List Pending Discoveries

```
/discover
```

Shows pending discoveries sorted by engagement score. Displays current filter settings and which filters were applied. Each entry includes approve/reject inline buttons.

### Approve a Channel

```
/discover approve @username
```

Adds the channel to tracking and marks all matching discovery rows as `added` with `matched_channel_id` set.

### Reject a Channel

```
/discover reject @username
```

Marks the discovery as `rejected`. Rejection cascades to all rows matching by username, peer ID, or invite link.

### View Rejected

```
/discover show-rejected [limit]
/discover rejected [limit]
```

Shows recently rejected discoveries (default limit: 20). Useful for reviewing past decisions.

### Preview a Discovery

```
/discover preview @username
```

Shows detailed information about a specific discovery including:
- Title and description
- Source type and discovery count
- Engagement metrics
- Filter status (actionable or filtered)

### Statistics

```
/discover stats
```

Shows discovery counts and filter statistics:
- Total discoveries by status
- Filter breakdown (already tracked, below threshold, etc.)

### Cleanup/Reconciliation

```
/discover cleanup
```

Backfills `matched_channel_id` for existing discoveries that match tracked channels. Safe to run multiple times (idempotent).

### Help

```
/discover help
```

Shows comprehensive help for all discovery commands.

---

## Description Keyword Filters

Filter discoveries by keywords in channel titles and descriptions. This reduces noise from off-topic channels before they appear in the review queue.

### How It Works

1. Build search text from `lower(title + ' ' + description)`
2. Apply allow list: at least one keyword must match (if configured)
3. Apply deny list: exclude if any keyword matches
4. Deny list takes precedence over allow list

### Allow List

Require at least one keyword to match for a channel to appear:

```
/discover allow add ai security
/discover allow remove security
/discover allow clear
/discover allow
```

- If set, channels without matching keywords are excluded
- Empty titles/descriptions fail the allow filter

### Deny List

Exclude channels containing specific keywords:

```
/discover deny add gambling crypto
/discover deny remove crypto
/discover deny clear
/discover deny
```

- Matching any deny keyword excludes the channel
- Takes precedence over allow list matches

### Shortcut Syntax

You can add keywords directly without the `add` subcommand:

```
/discover deny gambling       # Same as /discover deny add gambling
/discover allow ai news       # Same as /discover allow add ai news
```

---

## Preview with Filter Reasons

The `/discover preview` command shows detailed filter status for each discovery, explaining why it appears or is hidden:

```
/discover preview @channelname
```

Shows:
- Whether the channel is actionable or filtered
- Signal threshold status (seen count, engagement score)
- Allow/deny keyword match status
- Whether already tracked or matched

This helps diagnose why a channel isn't appearing in the main `/discover` list.

---

## Observability

### Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `digest_discovery_pending` | Gauge | Pending discoveries (all, not filtered) |
| `digest_discovery_actionable` | Gauge | Actionable discoveries (after filters) |
| `digest_discovery_approval_rate` | Gauge | Approval rate (added / (added + rejected)) |
| `digest_discovery_approved_total` | Counter | Total approved discoveries |
| `digest_discovery_rejected_total` | Counter | Total rejected discoveries |

### Filter Breakdown in Stats

The `/discover stats` command shows filter breakdown:

```
Pending: 150
  Already tracked: 12
  Below threshold: 45
  Allow miss: 20
  Deny hit: 8
  Actionable: 65
```

---

## Resolution Process

When a channel is discovered by peer ID or invite link, the system attempts to resolve its public username:

1. On first discovery, queue for resolution
2. Use Telegram API to fetch channel info
3. Store resolved username and description
4. Apply exponential backoff on failures (max 5 attempts)

Resolution enables username-based matching and provides more context for admin review.

---

## Database Schema

### discovered_channels

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID | Primary key |
| `username` | TEXT | Resolved @handle |
| `tg_peer_id` | BIGINT | Telegram channel ID |
| `invite_link` | TEXT | Private invite link |
| `title` | TEXT | Channel title |
| `description` | TEXT | Channel about text |
| `source_type` | TEXT | How discovered (forward, mention, link) |
| `discovery_count` | INT | Number of discoveries |
| `max_views` | INT | Highest view count |
| `max_forwards` | INT | Highest forward count |
| `engagement_score` | FLOAT | Calculated score |
| `status` | TEXT | pending, added, rejected |
| `matched_channel_id` | UUID | FK to channels.id |
| `from_channel_id` | UUID | Source channel ID |
| `first_seen_at` | TIMESTAMP | First discovery time |
| `last_seen_at` | TIMESTAMP | Most recent discovery |

### Key Indexes

- `idx_discovered_channels_pending` - Status + engagement for fast listing
- `idx_discovered_channels_matched` - matched_channel_id for tracking

---

## Background Reconciliation

A reconciliation job runs:
- Once at application startup
- Every 6 hours thereafter

It backfills `matched_channel_id` for discoveries that match tracked channels by any identifier, ensuring consistency if channels are added through other means.

---

## Implementation Files

| File | Purpose |
|------|---------|
| `internal/storage/discovery.go` | Discovery CRUD, filtering, scoring |
| `internal/storage/discovery_filters.go` | Keyword filtering logic |
| `internal/storage/channels.go` | `markDiscoveryAdded` linking |
| `internal/bot/handlers.go` | Admin commands (`/discover` namespace) |
| `internal/app/app.go` | Background reconciliation job |
| `internal/storage/queries.sql` | SQL queries |
| `internal/platform/observability/metrics.go` | Discovery metrics |

---

## See Also

- [Channel Importance](channel-importance-weight.md) - Per-channel importance weighting
- [Content Quality](content-quality.md) - Relevance gates and threshold tuning
