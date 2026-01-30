# Bulletized Digest Output

The bulletized output feature extracts individual claims from messages, scores them independently, and renders them as concise bullet points grouped by importance tier and topic. This provides a more scannable digest format compared to traditional summaries.

## Overview

Instead of rendering full message summaries, the system:

1. Extracts 1-3 key claims (bullets) from each message using LLM
2. Scores each bullet for relevance and importance
3. Generates embeddings for semantic deduplication
4. Deduplicates similar bullets across messages (higher threshold than item dedup)
5. Groups bullets by importance tier, then topic
6. Renders with corroboration counts and source attribution

---

## How It Works

### Extraction Phase

During pipeline processing, bullets are extracted from each message:

1. LLM receives message text, preview text, and summary
2. Returns up to N bullets (configurable via `BULLET_BATCH_SIZE`)
3. Each bullet includes:
   - Text (the claim)
   - Topic (falls back to parent item topic if empty)
   - Relevance score
   - Importance score

### Deduplication Phase

Bullets are deduplicated using a higher similarity threshold than items:

1. Pending bullets are compared against ready bullets from the lookback window
2. Cosine similarity calculated between embeddings
3. Bullets above threshold are marked as duplicates
4. Duplicates link to their canonical bullet for corroboration counting
5. Non-duplicate pending bullets are marked as ready

The higher threshold (0.92 default) prevents false merges on short strings where semantic similarity can be misleading (e.g., "5 people injured" vs "Casualties reported").

### Rendering Phase

Bullets are rendered in the digest:

1. Filter by minimum importance threshold
2. Limit bullets per cluster (prevents one story from dominating)
3. Group by importance tier (Breaking, Notable, Standard, Minor)
4. Within each tier, group by topic
5. Show corroboration count if multiple sources confirm the claim
6. Add source attribution for high-importance bullets

---

## Configuration

### Extraction Settings

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `BULLET_BATCH_SIZE` | int | `3` | Max bullets extracted per message |

### Deduplication Settings

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `BULLET_DEDUP_THRESHOLD` | float64 | `0.92` | Similarity threshold for deduplication |
| `BULLET_DEDUP_INTERVAL_MINS` | int | `5` | Interval between dedup runs |
| `BULLET_DEDUP_LOOKBACK_HOURS` | int | `48` | Lookback window for global dedup pool |

### Rendering Settings

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `BULLET_MAX_PER_CLUSTER` | int | `2` | Max bullets from a single cluster |
| `BULLET_MIN_IMPORTANCE` | float32 | `0.4` | Minimum importance to include |
| `BULLET_SOURCE_ATTRIBUTION` | bool | `true` | Show source channel for bullets |
| `BULLET_SOURCE_FORMAT` | string | `compact` | Attribution format: `full` or `compact` |

### Database Settings

Settings can be overridden via the `settings` table:

| Key | Description |
|-----|-------------|
| `bullet_max_per_cluster` | Max bullets per cluster (overrides env) |
| `bullet_min_importance` | Minimum importance threshold (overrides env) |
| `bullet_source_attribution` | Whether to show source attribution |
| `bullet_source_format` | Attribution format preference |

---

## Output Format

### Importance Tiers

| Tier | Emoji | Score Threshold |
|------|-------|-----------------|
| Breaking | Fire | >= 0.8 |
| Notable | Pushpin | >= 0.5 |
| Standard | Chart | >= 0.3 |
| Minor | Bullet | < 0.3 |

### Example Output

```
🔥 BREAKING

📌 CONFLICT
• Russian forces attacked Kharkiv energy infrastructure (3 sources)
• Ukrainian drones struck oil depot in Rostov region 📰K

📌 NOTABLE

🌍 DIPLOMACY
• EU extends sanctions package for another 6 months (2 sources)
• UN Security Council meets on humanitarian access

📊 STANDARD

💰 ECONOMY
• Central bank holds interest rate at 21% 📰C
```

### Source Attribution Formats

**Compact format** (default):
```
• Claim text 📰K
```
Where `📰K` shows a newspaper emoji plus the channel initial.

**Full format**:
```
• Claim text (via @channel_name)
```

### Corroboration Display

When multiple sources confirm the same claim:
```
• Claim text (3 sources)
```

---

## Deduplication Details

### Why Higher Threshold

Bullets require a stricter similarity threshold (0.92) compared to item deduplication (0.85) because:

- Short strings have higher false-positive rates
- Different factual claims can be semantically similar
- Example: "5 people injured" vs "Casualties reported" are similar but factually different

### Canonical Bullet Linking

When a bullet is deduplicated:

1. It's linked to the canonical (first-seen or highest-importance) bullet
2. This link enables corroboration counting
3. The digest shows "N sources" when multiple bullets merge to one

### Global Deduplication

Deduplication includes ready bullets from the lookback window:

- Prevents the same claim from appearing across digest windows
- Ready bullets serve as canonical candidates
- Only pending bullets can be marked as duplicates

---

## Topic Inheritance

Bullets inherit topics from their parent items:

1. LLM may return a topic with each bullet
2. System prefers the parent item's topic (from clustering)
3. LLM topic is used only as fallback when item has no topic

This prevents topic fragmentation where the same story could have bullets scattered across different topic sections.

---

## Cluster Limiting

To prevent one story from dominating the digest:

1. Build an index mapping items to their clusters
2. Track bullet count per cluster
3. Stop adding bullets from a cluster after reaching the limit

Default: 2 bullets per cluster.

---

## Database Schema

### item_bullets

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID | Primary key |
| `item_id` | UUID | FK to items.id |
| `bullet_index` | INT | Position within the item |
| `text` | TEXT | The bullet claim text |
| `topic` | TEXT | Assigned topic |
| `bullet_hash` | TEXT | SHA256 hash for exact dedup |
| `relevance_score` | FLOAT | Relevance score (0-1) |
| `importance_score` | FLOAT | Importance score (0-1) |
| `embedding` | VECTOR | Semantic embedding |
| `status` | TEXT | pending, ready, duplicate |
| `bullet_cluster_id` | UUID | Points to canonical bullet if duplicate |
| `source_channel` | TEXT | Channel username |
| `source_channel_title` | TEXT | Channel title |
| `source_channel_id` | UUID | FK to channels.id |
| `source_msg_id` | BIGINT | Telegram message ID |
| `tg_date` | TIMESTAMPTZ | Original message timestamp |
| `created_at` | TIMESTAMPTZ | Creation timestamp |

---

## Implementation Files

| File | Purpose |
|------|---------|
| `internal/process/pipeline/bullet_extraction.go` | LLM extraction and storage |
| `internal/process/pipeline/bullet_dedup.go` | Semantic deduplication |
| `internal/output/digest/render_bullets.go` | Digest rendering |
| `internal/storage/bullets.go` | Database operations |
| `internal/core/domain/bullet.go` | Domain model |
| `internal/core/llm/prompts.go` | Extraction prompt |

---

## Fallback Behavior

If bullet rendering returns empty (no bullets available or all filtered out), the digest falls back to summary-based rendering. This ensures digests are never empty.

---

## See Also

- [Content Quality](content-quality.md) - Relevance and importance scoring
- [Clustering](clustering.md) - How clusters limit bullet counts
- [Editor Mode](editor-mode.md) - Alternative narrative rendering
