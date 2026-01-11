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
```

### Weight Ranges

| Source | Range | Description |
|--------|-------|-------------|
| **Manual override** | 0.1 – 2.0 | Full range for admin control |
| **Auto-calculated** | 0.5 – 1.5 | Conservative range to prevent extreme swings |
| **Default** | 1.0 | Neutral (no change) |

Examples:
- `0.5` = halve importance (de-prioritize noisy channel)
- `1.0` = neutral (no change)
- `1.5` = boost importance by 50%
- `2.0` = double importance (high-priority source, manual only)

### Score Calculation

```
final_importance = llm_importance * channel_importance_weight
```

The final score is capped at 1.0 to maintain the 0-1 scale. This means weights > 1.0 provide a boost but cannot push a 0.6 importance item above 1.0:

```go
finalScore := min(1.0, llmScore * channel.ImportanceWeight)
// Example: 0.6 * 1.5 = 0.9 (boosted but still valid)
// Example: 0.8 * 1.5 = 1.0 (capped, not 1.2)
```

**Note**: The cap creates a "saturation effect" where high-importance items from boosted channels hit the ceiling. This is intentional—it prevents any single channel from dominating the digest.

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

    -- Quality metrics (nullable for new channels)
    avg_importance       FLOAT,            -- Average importance of digested items
    avg_relevance        FLOAT,            -- Average relevance score

    -- Engagement (future)
    digest_clicks        INT DEFAULT 0,    -- If tracking
    user_feedback_score  FLOAT,            -- If implementing ratings

    PRIMARY KEY (channel_id, period_start)
);
```

### Auto-Weight Formula

Calculate weekly, based on rolling 30-day stats:

```go
// Config values (from environment/settings)
type AutoWeightConfig struct {
    MinMessages       int     // MIN_MESSAGES_FOR_AUTO_WEIGHT (default: 10)
    ExpectedFrequency float32 // EXPECTED_MESSAGES_PER_DAY (default: 5.0)
    AutoMin           float32 // WEIGHT_AUTO_MIN (default: 0.5)
    AutoMax           float32 // WEIGHT_AUTO_MAX (default: 1.5)
}

func CalculateAutoWeight(stats ChannelStats, cfg AutoWeightConfig) float32 {
    // Guard: insufficient data - return neutral weight
    if stats.MessagesReceived < cfg.MinMessages {
        return 1.0
    }

    // Calculate derived metrics with null/zero guards
    var inclusionScore float32 = 0.0
    if stats.ItemsCreated > 0 {
        inclusionScore = float32(stats.ItemsDigested) / float32(stats.ItemsCreated)
    }

    // Use avg_importance directly (already 0-1 scale)
    // Only fall back to neutral if no digested items exist
    importanceScore := stats.AvgImportance
    if stats.ItemsDigested == 0 {
        importanceScore = 0.5 // No data to judge quality
    }

    // Calculate messages per day from period
    days := stats.PeriodEnd.Sub(stats.PeriodStart).Hours() / 24
    if days < 1 {
        days = 1
    }
    messagesPerDay := float32(stats.MessagesReceived) / float32(days)
    consistencyScore := min(1.0, messagesPerDay/cfg.ExpectedFrequency)

    // Signal-to-noise with divide-by-zero guard
    var signalScore float32 = 0.0
    if stats.MessagesReceived > 0 {
        signalScore = float32(stats.ItemsCreated) / float32(stats.MessagesReceived)
    }

    // Weighted sum (each component is 0-1)
    rawScore := (inclusionScore * 0.4) +
                (importanceScore * 0.3) +
                (consistencyScore * 0.2) +
                (signalScore * 0.1)

    // Map to weight range and clamp to configured bounds
    // rawScore 0.0 -> weight 0.5; rawScore 1.0 -> weight 1.5
    weight := 0.5 + rawScore
    weight = max(cfg.AutoMin, min(cfg.AutoMax, weight))

    return weight
}
```

### Auto-Update Schedule

- Run weekly (e.g., Sunday midnight)
- Only update channels where `auto_weight_enabled = true` (per-channel setting)
- Skip channels with < 10 messages in the period (insufficient data)
- Log changes for audit trail
- Notify admin of significant changes (>0.2 delta)

---

## Manual Override

### Database Schema

The `importance_weight` column is defined in the Proposed Solution section above. Additional columns for override control:

```sql
-- Per-channel control (added alongside importance_weight)
ALTER TABLE channels ADD COLUMN auto_weight_enabled BOOLEAN DEFAULT TRUE;
ALTER TABLE channels ADD COLUMN weight_override BOOLEAN DEFAULT FALSE;
ALTER TABLE channels ADD COLUMN weight_override_reason TEXT;
ALTER TABLE channels ADD COLUMN weight_updated_at TIMESTAMPTZ;
ALTER TABLE channels ADD COLUMN weight_updated_by BIGINT;
```

When `weight_override = true`, the `importance_weight` is manually set and auto-calculation is skipped for this channel regardless of `auto_weight_enabled`.

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

1. Add `importance_weight` column to channels (FLOAT DEFAULT 1.0)
2. Add control columns:
   - `auto_weight_enabled` (BOOLEAN DEFAULT TRUE)
   - `weight_override` (BOOLEAN DEFAULT FALSE)
   - `weight_override_reason` (TEXT)
   - `weight_updated_at` (TIMESTAMPTZ)
   - `weight_updated_by` (BIGINT)
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

### Global Settings (Environment/Config)

| Setting | Type | Default | Description |
|---------|------|---------|-------------|
| `AUTO_WEIGHT_ENABLED` | bool | true | Master switch for auto-calculation |
| `WEIGHT_UPDATE_INTERVAL` | duration | 7d | How often to recalculate |
| `WEIGHT_MIN` | float | 0.1 | Minimum allowed weight (manual) |
| `WEIGHT_MAX` | float | 2.0 | Maximum allowed weight (manual) |
| `WEIGHT_AUTO_MIN` | float | 0.5 | Minimum auto-calculated weight |
| `WEIGHT_AUTO_MAX` | float | 1.5 | Maximum auto-calculated weight |
| `WEIGHT_CHANGE_NOTIFY_THRESHOLD` | float | 0.2 | Notify admin if change exceeds this |
| `MIN_MESSAGES_FOR_AUTO_WEIGHT` | int | 10 | Minimum messages before auto-weight applies |
| `EXPECTED_MESSAGES_PER_DAY` | float | 5.0 | Expected frequency for consistency scoring |

### Per-Channel Settings (Database)

| Column | Type | Default | Description |
|--------|------|---------|-------------|
| `auto_weight_enabled` | bool | true | Enable auto-calculation for this channel |
| `weight_override` | bool | false | Manual override active (skips auto-calc) |

**Precedence**: Global `AUTO_WEIGHT_ENABLED=false` disables all auto-calculation. If global is enabled, per-channel `auto_weight_enabled` and `weight_override` control individual channels.

### Per-Channel Override Examples

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
| Breaking existing behavior | Default weight 1.0; auto-calc enabled by default but conservative (0.5-1.5 range) |

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
