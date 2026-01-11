# Proposal: Per-Channel Importance Weight System

## Problem Statement

Currently, importance scores are assigned purely by LLM based on message content. All channels are treated equally, but in practice:

- Some channels consistently produce high-quality, important content
- Some channels are noisy with low signal-to-noise ratio
- User preferences vary - a tech channel may be critical for one user, irrelevant for another
- Breaking news channels should have higher baseline importance than opinion blogs

## Current State

### What Exists
- `importance_threshold` per channel (filter, not weight)
- `category`, `tone`, `context` passed to LLM as hints
- Score normalization option (z-score per channel)
- LLM assigns importance 0-1 based on content newsworthiness

### Gap
No mechanism to **boost or reduce** a channel's importance contribution to the final digest based on channel quality/preference.

---

## Proposed Solution

### New Field: `importance_weight`

Add a multiplier field to channels that adjusts the final importance score:

```sql
ALTER TABLE channels ADD COLUMN importance_weight FLOAT DEFAULT 1.0;
-- Range: 0.1 to 2.0
-- 1.0 = neutral (no change)
-- 0.5 = halve importance (de-prioritize)
-- 1.5 = boost importance by 50%
-- 2.0 = double importance (high-priority source)
```

### Score Calculation

```
final_importance = llm_importance * channel_importance_weight
```

Capped at 1.0 to maintain the 0-1 scale:
```go
finalScore := min(1.0, llmScore * channel.ImportanceWeight)
```

---

## Auto-Calculation Algorithm

### Metrics to Track (New Table)

```sql
CREATE TABLE channel_stats (
    channel_id       UUID REFERENCES channels(id),
    period_start     DATE NOT NULL,
    period_end       DATE NOT NULL,

    -- Volume metrics
    messages_received    INT DEFAULT 0,
    items_created        INT DEFAULT 0,    -- Passed relevance filter
    items_digested       INT DEFAULT 0,    -- Included in digest

    -- Quality metrics
    avg_importance       FLOAT,
    avg_relevance        FLOAT,
    digest_inclusion_rate FLOAT,  -- items_digested / items_created

    -- Engagement (future)
    digest_clicks        INT DEFAULT 0,    -- If tracking
    user_feedback_score  FLOAT,            -- If implementing ratings

    PRIMARY KEY (channel_id, period_start)
);
```

### Auto-Weight Formula

Calculate weekly, based on rolling 30-day stats:

```go
func CalculateAutoWeight(stats ChannelStats) float32 {
    // Base components (each 0-1, weighted)

    // 1. Digest inclusion rate (40% weight)
    // How often does this channel's content make it to digest?
    inclusionScore := stats.DigestInclusionRate  // 0-1

    // 2. Average importance of digested items (30% weight)
    // Do items from this channel tend to be high-importance?
    importanceScore := stats.AvgImportanceOfDigested  // 0-1

    // 3. Consistency (20% weight)
    // Does channel produce content regularly?
    // Penalize very sporadic channels
    consistencyScore := min(1.0, stats.MessagesPerDay / expectedFrequency)

    // 4. Signal-to-noise ratio (10% weight)
    // What % of messages pass relevance filter?
    signalScore := stats.ItemsCreated / stats.MessagesReceived

    // Weighted sum
    rawScore := (inclusionScore * 0.4) +
                (importanceScore * 0.3) +
                (consistencyScore * 0.2) +
                (signalScore * 0.1)

    // Map to weight range [0.5, 1.5]
    // Score 0.0 -> weight 0.5 (de-prioritize poor channels)
    // Score 0.5 -> weight 1.0 (neutral)
    // Score 1.0 -> weight 1.5 (boost excellent channels)
    weight := 0.5 + rawScore

    return weight
}
```

### Auto-Update Schedule

- Run weekly (e.g., Sunday midnight)
- Only update if `auto_weight_enabled = true` for channel
- Log changes for audit trail
- Notify admin of significant changes (>0.2 delta)

---

## Manual Override

### Database Schema

```sql
ALTER TABLE channels ADD COLUMN importance_weight FLOAT DEFAULT 1.0;
ALTER TABLE channels ADD COLUMN weight_override BOOLEAN DEFAULT FALSE;
ALTER TABLE channels ADD COLUMN weight_override_reason TEXT;
ALTER TABLE channels ADD COLUMN weight_updated_at TIMESTAMPTZ;
ALTER TABLE channels ADD COLUMN weight_updated_by BIGINT;
```

### Bot Commands

```
/channel @username weight 1.5
  -> Sets importance_weight to 1.5, marks as override

/channel @username weight auto
  -> Clears override, enables auto-calculation

/channel @username stats
  -> Shows current weight, auto-calculated suggestion, stats

/channels weights
  -> Lists all channels with weights, sorted by weight desc
```

### Admin API (Future)

```
PUT /api/channels/{id}/weight
{
  "weight": 1.5,
  "reason": "Primary breaking news source"
}
```

---

## Implementation Plan

### Phase 1: Database & Model (Migration)

1. Add `importance_weight` column to channels
2. Add `weight_override`, `weight_override_reason` columns
3. Create `channel_stats` table
4. Update Go models

**Files:**
- `migrations/YYYYMMDD_add_channel_weight.sql`
- `internal/db/channels.go`
- `internal/db/sqlc/queries.sql`

### Phase 2: Apply Weight in Pipeline

1. Load channel weight when processing messages
2. Multiply LLM importance by weight before storing
3. Add logging for weight application

**Files:**
- `internal/pipeline/pipeline.go` (lines ~400-450)
- `internal/db/items.go`

### Phase 3: Stats Collection

1. Track metrics during digest generation
2. Store daily stats in `channel_stats`
3. Aggregate to weekly summaries

**Files:**
- `internal/digest/digest.go`
- `internal/db/channel_stats.go` (new)

### Phase 4: Auto-Calculation

1. Implement weight calculation algorithm
2. Add scheduled job (weekly)
3. Respect override flag

**Files:**
- `internal/digest/channel_weight.go` (new)
- `cmd/digest-bot/main.go` (scheduler)

### Phase 5: Bot Commands

1. `/channel weight` command
2. `/channels weights` list command
3. Admin notifications for changes

**Files:**
- `internal/telegrambot/handlers.go`

---

## Configuration

### New Settings

| Setting | Type | Default | Description |
|---------|------|---------|-------------|
| `auto_weight_enabled` | bool | true | Enable auto-calculation globally |
| `weight_update_interval` | duration | 7d | How often to recalculate |
| `weight_min` | float | 0.1 | Minimum allowed weight |
| `weight_max` | float | 2.0 | Maximum allowed weight |
| `weight_change_notify_threshold` | float | 0.2 | Notify admin if change exceeds this |

### Per-Channel Override

```sql
-- Example: Boost @breaking_news to always be high priority
UPDATE channels
SET importance_weight = 1.8,
    weight_override = true,
    weight_override_reason = 'Primary breaking news source'
WHERE username = 'breaking_news';

-- Example: De-prioritize @memes_daily
UPDATE channels
SET importance_weight = 0.3,
    weight_override = true,
    weight_override_reason = 'Entertainment only, low news value'
WHERE username = 'memes_daily';
```

---

## Example Scenarios

### Scenario 1: Breaking News Channel
- Channel: @reuters
- Auto-calculated stats: 80% digest inclusion, avg importance 0.75
- Auto weight: 1.4
- Override: 1.8 (manual boost for trusted source)
- Result: Reuters items get 1.8x importance multiplier

### Scenario 2: Noisy Tech Blog
- Channel: @tech_rumors
- Auto-calculated stats: 15% digest inclusion, avg importance 0.35
- Auto weight: 0.65
- No override
- Result: Items de-prioritized, only exceptional content makes digest

### Scenario 3: New Channel (No History)
- Channel: @new_source
- No stats yet
- Default weight: 1.0
- After 30 days: auto-calculation kicks in

---

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Weight too aggressive, excludes good content | Cap at 0.1 minimum, admin notifications |
| Auto-calculation gaming | Track anomalies, require manual review for extremes |
| Complexity in debugging | Detailed logging of weight application |
| Breaking existing behavior | Default weight 1.0, opt-in for auto |

---

## Success Metrics

1. **Digest quality improvement**: Higher user engagement with weighted digests
2. **Noise reduction**: Fewer low-quality items in digest
3. **Admin efficiency**: Less manual curation needed
4. **Transparency**: Clear audit trail of weight changes

---

## Open Questions

1. Should weight affect relevance filtering too, or only importance?
2. Should users be able to set personal channel preferences (future multi-user)?
3. How to handle channels with very low volume (< 5 messages/month)?
4. Should weight decay over time if channel goes inactive?

---

## Timeline Estimate

| Phase | Complexity | Dependencies |
|-------|------------|--------------|
| Phase 1: Database | Low | None |
| Phase 2: Apply Weight | Low | Phase 1 |
| Phase 3: Stats Collection | Medium | Phase 1 |
| Phase 4: Auto-Calculation | Medium | Phase 3 |
| Phase 5: Bot Commands | Low | Phase 2 |

Recommended order: 1 → 2 → 5 → 3 → 4

Start with manual weights (phases 1-2-5), then add auto-calculation later.
