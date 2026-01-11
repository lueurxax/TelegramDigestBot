# Channel Importance Weight

Per-channel importance weight allows boosting or reducing a channel's contribution to the final digest based on source quality and user preference.

## Overview

Each channel has an `importance_weight` multiplier (0.1–2.0) that adjusts the LLM-assigned importance score:

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
- Note: Auto-calculation is not yet implemented; weight stays at 1.0 until manually changed

### View All Weights

```
/channel list
```

Shows all tracked channels with their current weights.

## Database Schema

```sql
-- Added to channels table
importance_weight        FLOAT4 DEFAULT 1.0    -- The multiplier
auto_weight_enabled      BOOLEAN DEFAULT TRUE  -- Reserved for future auto-calc
weight_override          BOOLEAN DEFAULT FALSE -- Manual override active
weight_override_reason   TEXT                  -- Why weight was set
weight_updated_at        TIMESTAMPTZ           -- Last change timestamp
weight_updated_by        BIGINT                -- Telegram user ID who changed it
```

## Pipeline Behavior

Weight is applied in `internal/pipeline/pipeline.go` after LLM scoring:

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

### Reset After Testing

```
/channel weight @testchannel auto
```

Returns to neutral weight (1.0).

## Limitations

### Not Yet Implemented

The following features from the original proposal are deferred:

1. **Auto-calculation** - Automatic weight adjustment based on channel performance metrics
2. **Stats collection** - `channel_stats` table tracking inclusion rates, average importance, etc.
3. **Scheduled updates** - Weekly job to recalculate weights
4. **Admin notifications** - Alerts when weights change significantly

Currently, all weight management is manual via bot commands.

### Weight Does Not Affect Relevance

The weight multiplier only affects `ImportanceScore`, not `RelevanceScore`. A channel with low weight can still have items included if they pass the relevance threshold.

## Files

| File | Purpose |
|------|---------|
| `migrations/20260111120000_add_importance_weight.sql` | Database migration |
| `internal/db/channels.go` | Channel model and weight queries |
| `internal/db/queries.sql` | SQL for GetChannelWeight, UpdateChannelWeight |
| `internal/pipeline/pipeline.go` | Weight application logic |
| `internal/telegrambot/handlers.go` | `/channel weight` command handler |

## See Also

- [Content Quality Improvements](../proposals/content-quality-improvements.md) - Roadmap including future auto-weighting
