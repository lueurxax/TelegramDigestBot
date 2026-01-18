# Channel Importance Weight

Per-channel importance weight allows boosting or reducing a channel's contribution to the final digest based on source quality and user preference.

## Overview

Each channel has an `importance_weight` multiplier (0.1-2.0) that adjusts the LLM-assigned importance score:

```
final_importance = min(1.0, llm_importance * channel_weight)
```

- **1.0** = neutral (default, no change)
- **< 1.0** = de-prioritize (e.g., 0.5 halves importance)
- **> 1.0** = boost (e.g., 1.5 increases importance by 50%)

The final score is capped at 1.0 to maintain a valid range and prevent any single channel from dominating.

## Usage

### View Channel Weight

```
/channel weight @username
```

Shows:
- Current weight value
- Whether it's a manual override or auto mode
- Override reason (if set)
- Last updated timestamp

### Set Manual Weight

```
/channel weight @username 1.5
/channel weight @username 1.5 Primary breaking news source
```

- Valid range: 0.1 to 2.0
- Optional reason text for audit trail
- Sets `weight_override = true`, disabling auto-calculation for this channel

### Reset to Auto Mode

```
/channel weight @username auto
```

- Resets weight to 1.0
- Sets `auto_weight_enabled = true` and `weight_override = false`
- Weight will be automatically adjusted by the weekly auto-weight job

### View All Weights

```
/channel list
```

Shows all tracked channels with their current weights.

## Auto-Weight Calculation

Channels with `auto_weight_enabled = true` and `weight_override = false` have their weights automatically calculated weekly (Sunday at midnight).

### Algorithm

The auto-weight is calculated from rolling 30-day channel statistics:

```
rawScore = (inclusionScore * 0.4) +
           (importanceScore * 0.3) +
           (consistencyScore * 0.2) +
           (signalScore * 0.1)

weight = 0.5 + rawScore  // Maps 0-1 to 0.5-1.5
```

Where:
- **inclusionScore** = items_digested / items_created (how often channel's items make the digest)
- **importanceScore** = average importance of digested items
- **consistencyScore** = messages_per_day / expected_frequency (posting regularity)
- **signalScore** = items_created / messages_received (signal-to-noise ratio)

### Auto-Weight Range

Auto-calculated weights are constrained to **0.5-1.5** (more conservative than manual 0.1-2.0) to prevent extreme swings.

### Configuration

Auto-weight behavior can be configured via database settings:

| Setting | Default | Description |
|---------|---------|-------------|
| `auto_weight_enabled` | true | Master switch for auto-calculation |
| `auto_weight_min_messages` | 10 | Minimum messages before auto-weight applies |
| `auto_weight_expected_freq` | 5.0 | Expected messages per day for consistency scoring |
| `auto_weight_min` | 0.5 | Minimum auto-calculated weight |
| `auto_weight_max` | 1.5 | Maximum auto-calculated weight |
| `auto_weight_rolling_days` | 30 | Days to look back for stats |

## Stats Collection

Channel statistics are collected automatically after each digest is posted:

- **messages_received** - Total messages from channel in window
- **items_created** - Items that passed relevance filter
- **items_digested** - Items included in digest
- **avg_importance** - Average importance score
- **avg_relevance** - Average relevance score

Stats are stored in the `channel_stats` table and aggregated daily.

## Database Schema

```sql
-- Added to channels table
importance_weight        FLOAT4 DEFAULT 1.0    -- The multiplier
auto_weight_enabled      BOOLEAN DEFAULT TRUE  -- Enable auto-calculation
weight_override          BOOLEAN DEFAULT FALSE -- Manual override active
weight_override_reason   TEXT                  -- Why weight was set
weight_updated_at        TIMESTAMPTZ           -- Last change timestamp
weight_updated_by        BIGINT                -- Telegram user ID who changed it

-- Stats table
CREATE TABLE channel_stats (
    channel_id       UUID REFERENCES channels(id),
    period_start     DATE NOT NULL,
    period_end       DATE NOT NULL,
    messages_received    INT,
    items_created        INT,
    items_digested       INT,
    avg_importance       FLOAT,
    avg_relevance        FLOAT,
    PRIMARY KEY (channel_id, period_start, period_end)
);
```

## Pipeline Behavior

Weight is applied in `internal/process/pipeline/pipeline.go` after LLM scoring:

```go
// Apply channel importance weight multiplier
channelWeight := candidates[i].ImportanceWeight
// Clamp weight to valid range [0.1, 2.0], default to 1.0 if invalid
if channelWeight < 0.1 {
    channelWeight = 1.0
} else if channelWeight > 2.0 {
    channelWeight = 2.0
}
importance := res.ImportanceScore * channelWeight
// Cap at 1.0 to maintain valid range
if importance > 1.0 {
    importance = 1.0
}
```

## Examples

### Boost a Trusted News Source

```
/channel weight @reuters 1.8 Primary breaking news source
```

Reuters items get 1.8x importance multiplier. An item with LLM importance 0.5 becomes 0.9.

### De-prioritize a Noisy Channel

```
/channel weight @tech_rumors 0.5 High noise, low signal
```

Items from this channel need higher LLM scores to make the digest.

### Let Auto-Weight Handle It

```
/channel weight @newchannel auto
```

Returns to auto mode. After 30 days of data collection, weight will be automatically calculated based on channel performance.

## Limitations

### Weight Does Not Affect Relevance

The weight multiplier only affects `ImportanceScore`, not `RelevanceScore`. A channel with low weight can still have items included if they pass the relevance threshold.

### New Channels

Channels with fewer than 10 messages in the rolling window keep the default weight of 1.0 until sufficient data is collected.

## Files

| File | Purpose |
|------|---------|
| `migrations/20260111120000_add_importance_weight.sql` | Weight columns migration |
| `migrations/20260111130000_add_channel_stats.sql` | Stats table migration |
| `internal/db/channels.go` | Channel model and weight queries |
| `internal/db/channel_stats.go` | Stats collection and auto-weight queries |
| `internal/db/queries.sql` | SQL queries |
| `internal/process/pipeline/pipeline.go` | Weight application logic |
| `internal/output/digest/autoweight.go` | Auto-weight calculation algorithm |
| `internal/output/digest/digest.go` | Stats collection and weekly job |
| `internal/telegrambot/handlers.go` | `/channel weight` command handler |

## See Also

- [Content Quality System](../features/content-quality.md) - How relevance, ratings, and clustering fit together
