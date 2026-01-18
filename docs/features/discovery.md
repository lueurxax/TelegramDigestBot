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
| `internal/storage/channels.go` | `markDiscoveryAdded` linking |
| `internal/bot/handlers.go` | Admin commands |
| `internal/app/app.go` | Background reconciliation job |
| `internal/storage/queries.sql` | SQL queries |

---

## Future Enhancements (v2)

These features are planned but not yet implemented:

- **Description keyword filters** - Allow/deny lists for channel descriptions
- **Time-series metrics** - Track pending vs actionable counts over time
- **Preview filters** - Show why a specific entry is hidden
