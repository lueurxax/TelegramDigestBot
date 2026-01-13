# Content Quality System

The content quality system improves digest signal-to-noise through six integrated features: feedback-driven calibration, pre-summarization relevance filtering, semantic clustering, source credibility signals, topic balance rules, and an evaluation harness for testing.

## Overview

These features work together throughout the digest pipeline:

1. **Relevance Gate** - Filters out low-value messages before expensive LLM summarization
2. **Feedback Loop** - Uses user ratings to automatically tune thresholds over time
3. **Auto-Relevance** - Adjusts per-channel relevance thresholds based on reliability
4. **Clustering** - Groups semantically similar items to reduce redundancy
5. **Topic Balance** - Ensures diverse topic coverage in each digest
6. **Evaluation Harness** - Measures and validates quality metrics against labeled data

---

## Relevance Gate

The relevance gate filters messages **before** LLM summarization to save cost and reduce noise. Messages that fail the gate are marked as rejected and never summarized.

### How It Works

The gate operates in one of three modes:

| Mode | Behavior |
|------|----------|
| `heuristic` | Fast rule-based checks only (default) |
| `llm` | LLM-based classification |
| `hybrid` | Heuristic first, LLM for borderline cases |

**Heuristic checks** (applied in all modes):
- Empty messages - rejected with reason `empty`
- Link-only messages (no text after removing URLs) - rejected with reason `link_only`
- No alphanumeric content - rejected with reason `no_text`

**LLM classification** uses a rubric prompt that evaluates:
- Is this factual news or meaningful information?
- Is it spam, pure promotion, or non-informational chatter?

### Configuration

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `RELEVANCE_GATE_ENABLED` | bool | `false` | Enable the relevance gate |
| `RELEVANCE_GATE_MODE` | string | `heuristic` | Gate mode: `heuristic`, `llm`, or `hybrid` |
| `RELEVANCE_GATE_MODEL` | string | (empty) | LLM model for gate; falls back to `LLM_MODEL` if empty |

### Database Settings

The gate prompt can be customized via the `settings` table:

| Key | Description |
|-----|-------------|
| `prompt:relevance_gate:active` | Active prompt version (e.g., `v1`, `v2`) |
| `prompt:relevance_gate:v1` | Prompt text for version v1 |

### Implementation

- **File**: `internal/pipeline/relevance_gate.go`
- Gate decisions are logged to `relevance_gate_log` with: decision, confidence, reason, model, version

---

## Feedback Loop and Threshold Tuning

User ratings drive automatic threshold adjustments to improve digest quality over time.

### Rating System

Users provide feedback via:
```
/feedback <item_id> <good|bad|irrelevant> [comment]
```

Ratings are stored in the `item_ratings` table with the channel ID for per-source analysis.

### Threshold Tuning Algorithm

A weekly job analyzes the past 30 days of ratings with exponential decay weighting:

```
weight = exp(-age_days * ln(2) / 14)
```

This gives a 14-day half-life, so recent ratings matter more than older ones.

**Net score calculation:**
```
net = (weighted_good - (weighted_bad + weighted_irrelevant)) / weighted_total
```

**Threshold adjustment:**
- If `net > 0.20`: decrease thresholds by step (more items pass)
- If `net < -0.20`: increase thresholds by step (fewer items pass)
- Otherwise: no change

### Configuration

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `AUTO_THRESHOLD_TUNING_ENABLED` | bool | `false` | Enable automatic threshold tuning |
| `THRESHOLD_TUNING_STEP` | float32 | `0.05` | Amount to adjust thresholds per update |
| `THRESHOLD_TUNING_MIN` | float32 | `0.10` | Minimum allowed threshold value |
| `THRESHOLD_TUNING_MAX` | float32 | `0.90` | Maximum allowed threshold value |
| `THRESHOLD_TUNING_NET_POSITIVE` | float32 | `0.20` | Net score above which thresholds decrease |
| `THRESHOLD_TUNING_NET_NEGATIVE` | float32 | `-0.20` | Net score below which thresholds increase |
| `RATING_MIN_SAMPLE_GLOBAL` | int | `100` | Minimum total ratings before tuning activates |

### Implementation

- **File**: `internal/digest/threshold_tuning.go`
- Thresholds are stored in the `settings` table as `relevance_threshold` and `importance_threshold`

---

## Auto-Relevance (Per-Channel Credibility)

Channels with consistently poor-rated content automatically receive stricter relevance thresholds.

### How It Works

1. Compute per-channel reliability from 30-day weighted ratings:
   ```
   reliability = weighted_good / weighted_total  (0 to 1)
   ```

2. Calculate relevance threshold delta:
   ```
   penalty = (1.0 - reliability) * 0.2
   ```

3. Apply to effective threshold:
   ```
   effective_threshold = base_threshold + penalty
   ```

A channel with 100% good ratings gets no penalty. A channel with 0% good ratings gets a +0.2 penalty (harder to pass).

### Guardrails

- **Minimum samples**: Requires at least 15 ratings per channel before adjusting
- **Global minimum**: Requires 100 total ratings before any auto-relevance runs
- **Manual override**: Channels can disable auto-relevance with `auto_relevance_enabled = false`
- **Reset on sparse data**: Delta resets to 0 if a channel falls below the sample minimum

### Configuration

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `RATING_MIN_SAMPLE_CHANNEL` | int | `15` | Minimum ratings per channel for auto-adjustment |
| `RATING_MIN_SAMPLE_GLOBAL` | int | `100` | Minimum total ratings for auto-relevance to run |

### Database Fields

| Table | Column | Description |
|-------|--------|-------------|
| `channels` | `auto_relevance_enabled` | Whether auto-adjustment is active (default: true) |
| `channels` | `relevance_threshold_delta` | Current penalty applied (default: 0.0) |

### Implementation

- **File**: `internal/digest/autorelevance.go`
- Constants: 30-day window, 14-day half-life, 0.2 max penalty factor

---

## Clustering

Semantic clustering groups related items to reduce redundancy in the digest. Each cluster is represented by its highest-importance item.

### How It Works

1. **Group by topic**: Items are first grouped by their LLM-assigned topic (normalized to title case)
2. **Compute similarity**: Cosine similarity between item embeddings (using `text-embedding-3-small`)
3. **Form clusters**: Items above the similarity threshold are grouped together
4. **Validate coherence**: Clusters with low average pairwise similarity are rejected
5. **Generate summary**: LLM creates a unified topic label for multi-item clusters

### Configuration

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `CLUSTER_SIMILARITY_THRESHOLD` | float32 | `0.75` | Minimum similarity to join a cluster |
| `CLUSTER_COHERENCE_THRESHOLD` | float32 | `0.70` | Minimum average coherence for cluster acceptance |
| `CLUSTER_TIME_WINDOW_HOURS` | int | `36` | Maximum time span between clustered items |
| `CROSS_TOPIC_CLUSTERING_ENABLED` | bool | `false` | Allow clustering across different topics |
| `CROSS_TOPIC_SIMILARITY_THRESHOLD` | float32 | `0.90` | Higher threshold for cross-topic merges |

### Database Settings

These can also be overridden in the `settings` table:
- `cluster_similarity_threshold`
- `cluster_coherence_threshold`
- `cluster_time_window_hours`
- `cross_topic_clustering_enabled`
- `cross_topic_similarity_threshold`

### Implementation

- **File**: `internal/digest/clustering.go`
- Clusters stored in `clusters` table with item links via `cluster_items`
- Maximum 500 items per clustering run to prevent performance issues

---

## Topic Balance

Topic balance ensures digest diversity by limiting how many items can come from a single topic and applying freshness decay to older content.

### Diversity Cap

No single topic can exceed a configurable percentage of the digest:

```
max_items_per_topic = floor(topic_diversity_cap * digest_top_n)
```

With defaults (30% cap, 20 items), no topic gets more than 6 items.

**Selection algorithm:**
1. Ensure minimum topic count by taking one item from each available topic
2. Fill remaining slots respecting the per-topic cap
3. If unable to fill all slots due to caps, relax the cap and log a warning

### Freshness Decay

Older items receive a lower importance score:

```
decayed_importance = importance * max(exp(-age_hours / decay_hours), floor)
```

With defaults (36-hour decay, 0.4 floor):
- 0 hours old: 100% importance
- 36 hours old: ~37% importance
- 72+ hours old: 40% importance (floor)

### Configuration

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `TOPIC_DIVERSITY_CAP` | float32 | `0.30` | Maximum fraction of digest from one topic |
| `MIN_TOPIC_COUNT` | int | `3` | Minimum distinct topics to include if available |
| `FRESHNESS_DECAY_HOURS` | int | `36` | Time constant for freshness decay |
| `FRESHNESS_FLOOR` | float32 | `0.4` | Minimum decay multiplier |

### Implementation

- **File**: `internal/digest/topic_balance.go`
- Topics normalized to lowercase with trimmed whitespace
- Unknown/empty topics grouped as `__unknown__` and excluded from diversity counting

---

## Evaluation Harness

The evaluation harness measures quality metrics against labeled data to validate changes and prevent regressions.

### Label Export Tool

Export labeled annotations from the database to a JSONL file:

```bash
go run cmd/labels/main.go \
  -dsn "$POSTGRES_DSN" \
  -out docs/eval/golden.jsonl \
  -limit 500
```

**Output format** (one JSON object per line):
```json
{"id": "msg123", "label": "good", "relevance_score": 0.72, "importance_score": 0.65}
```

Labels can be: `good`, `bad`, or `irrelevant`.

### Evaluation Tool

Run evaluation against a labeled dataset:

```bash
go run cmd/eval/main.go \
  -input docs/eval/golden.jsonl \
  -relevance-threshold 0.5 \
  -importance-threshold 0.3
```

**Options:**

| Flag | Default | Description |
|------|---------|-------------|
| `-input` | `docs/eval/sample.jsonl` | Path to JSONL dataset |
| `-relevance-threshold` | `0.5` | Relevance score threshold |
| `-importance-threshold` | `0.3` | Importance score threshold |
| `-ignore-importance` | `false` | Only use relevance for classification |
| `-min-precision` | `-1` | Fail if precision below this (disabled if < 0) |
| `-max-noise-rate` | `-1` | Fail if noise rate above this (disabled if < 0) |

**Output metrics:**
- **Precision**: TP / (TP + FP) - fraction of selected items that are good
- **Recall**: TP / (TP + FN) - fraction of good items that were selected
- **Noise Rate**: FP / (TP + FP) - fraction of selected items that are bad/irrelevant
- **Coverage**: Selected / Total - fraction of items that passed thresholds

### CI Integration

Add to your CI pipeline to catch regressions:

```bash
go run cmd/eval/main.go \
  -input docs/eval/golden.jsonl \
  -min-precision 0.80 \
  -max-noise-rate 0.15
```

The tool exits with code 1 if thresholds are violated.

### Implementation

- **Evaluation tool**: `cmd/eval/main.go`
- **Label export**: `cmd/labels/main.go`
- **Golden datasets**: Store in `docs/eval/` directory

---

## Success Metrics

Track these metrics to measure quality improvements:

| Metric | Formula | Target |
|--------|---------|--------|
| Noise Rate | `(bad + irrelevant) / total_rated` | < 15% |
| Precision | `good / (good + bad + irrelevant)` predicted | > 80% |
| Duplicate Rate | `1 - (cluster_count / item_count)` | Lower is better |
| Topic Coverage | Distinct topics in digest | >= 3 |

---

## Quick Reference: All Configuration Variables

| Variable | Default | Feature |
|----------|---------|---------|
| `RELEVANCE_GATE_ENABLED` | `false` | Relevance Gate |
| `RELEVANCE_GATE_MODE` | `heuristic` | Relevance Gate |
| `RELEVANCE_GATE_MODEL` | (empty) | Relevance Gate |
| `AUTO_THRESHOLD_TUNING_ENABLED` | `false` | Threshold Tuning |
| `THRESHOLD_TUNING_STEP` | `0.05` | Threshold Tuning |
| `THRESHOLD_TUNING_MIN` | `0.10` | Threshold Tuning |
| `THRESHOLD_TUNING_MAX` | `0.90` | Threshold Tuning |
| `THRESHOLD_TUNING_NET_POSITIVE` | `0.20` | Threshold Tuning |
| `THRESHOLD_TUNING_NET_NEGATIVE` | `-0.20` | Threshold Tuning |
| `RATING_MIN_SAMPLE_CHANNEL` | `15` | Auto-Relevance |
| `RATING_MIN_SAMPLE_GLOBAL` | `100` | Auto-Relevance / Tuning |
| `CLUSTER_SIMILARITY_THRESHOLD` | `0.75` | Clustering |
| `CLUSTER_COHERENCE_THRESHOLD` | `0.70` | Clustering |
| `CLUSTER_TIME_WINDOW_HOURS` | `36` | Clustering |
| `CROSS_TOPIC_CLUSTERING_ENABLED` | `false` | Clustering |
| `CROSS_TOPIC_SIMILARITY_THRESHOLD` | `0.90` | Clustering |
| `TOPIC_DIVERSITY_CAP` | `0.30` | Topic Balance |
| `MIN_TOPIC_COUNT` | `3` | Topic Balance |
| `FRESHNESS_DECAY_HOURS` | `36` | Topic Balance |
| `FRESHNESS_FLOOR` | `0.4` | Topic Balance |
