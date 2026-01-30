# Bulletized Digest Output (Pre-Scoring Bullets)

> **Status: PROPOSED (Deferred)**
>
> Implementation deferred pending resolution of design issues documented below.
> Infrastructure code retained for future implementation.

## Summary

Split each message into short bullets before scoring, then group bullets by importance tier and topic. Each bullet can optionally link to the expanded view of its source item.

## Implementation Status

| Component | Status | Notes |
|-----------|--------|-------|
| Database schema (`item_bullets` table) | Done | Migration `20260128001000_add_item_bullets.sql` |
| Domain model (`Bullet` struct) | Done | `internal/core/domain/bullet.go` |
| LLM extraction interface | Done | `ExtractBullets` in Provider interface |
| Task config | Done | `TaskTypeBulletExtract` configured |
| Bullet extraction logic | Not started | — |
| LLM scoring integration | Not started | — |
| Deduplication | Not started | — |
| Digest rendering | Not started | — |

## Design Issues to Address

The following issues must be resolved before implementation proceeds.

### 1. Noise Amplification (Critical)

**Problem:** The proposal lacks definition of how bullets interact with clusters:
- If a cluster contains 5 items, and each item yields 4 bullets → 20 bullets for one story
- This defeats the "Noise Reduction" goal entirely
- Digest becomes longer than the current summary-based version
- Shifts from "Tell me the news" to "Give me every single claim found in the news"

**Required Solution:** Define a bullet aggregation strategy that:
- Limits total bullets per cluster (e.g., top 3-5 highest-scoring)
- Deduplicates semantically identical bullets across items
- Preserves corroboration signal (how many sources reported same claim)

### 2. Language-Dependent Splitting

**Problem:** Character-based splitting (`:`, `—`, `;`) is incompatible with Russian/Ukrainian:
- The dash (`—`) is frequently used as a copula (replacement for "is/are")
- Example: "Цель — топливный склад" (The target — a fuel depot)
- Heuristic split produces:
  - Bullet 1: "Цель" (The target)
  - Bullet 2: "топливный склад" (a fuel depot)
- Result: Two meaningless fragments instead of one clear claim

**Required Solution:** Keep the message whole and use LLM-based extraction:
```
Identify the 1-3 most important self-contained claims in this text.
Each claim must be understandable without additional context.
Return as JSON array: [{"text": "...", "score": 0.0-1.0, "topic": "..."}]
```

This approach:
- Keeps full message context during extraction
- Leverages LLM semantic understanding
- Avoids language-specific heuristics
- Limits bullet count at extraction time (not post-hoc filtering)
- Produces coherent, self-contained bullets with scores

**Topic Inheritance Fallback:** If the model fails or returns a generic/empty topic, fall back to the parent item's topic. This ensures bullets never end up in an "Uncategorized" bucket.

### 3. Center-of-List Bias

**Problem:** Bundling extraction, scoring, and topic assignment in one call risks LLM center-of-list bias:
- Current batch size of 10 messages × 4 bullets = 40 bullets per call
- LLMs give generic scores to later items in large batches

**Required Solution:** Reduce batch size when bulletization is active:
- Default: 10 messages per batch
- With bulletization: 5 messages per batch
- Add config: `BULLET_BATCH_SIZE` (default: 5)

**Cost Note:** Output token costs will roughly double or triple because the model returns structured JSON for multiple bullets instead of one short sentence. This is acceptable for quality gain but must be monitored in budget guardrails.

### 4. Source Attribution

**Problem:** The proposal states "no per-bullet author/source attribution":
- Credibility is critical for a noise-reduction bot
- Users need to know which channel reported what
- Anonymous bullets reduce trust

**Required Solution:** Add source attribution to maintain trust/credibility layer:
- Full format: `• Claim text (via @channel)`
- Compact format: `• Claim text 📰CH` (emoji + channel initial)
- Only show for "High" importance tier to reduce noise
- Config: `BULLET_SOURCE_ATTRIBUTION` (default: true)
- Config: `BULLET_SOURCE_FORMAT` (`full` | `compact`, default: `compact`)

### 5. Deduplication Threshold

**Problem:** Deduplicating bullets is harder than deduplicating messages:
- The proposal mentions "semantic similarity" but doesn't define a threshold
- Current `CLUSTER_SIMILARITY_THRESHOLD` (0.75) is designed for full summaries
- Short strings have higher false-positive rates at this threshold
- Example false merge: "5 people injured" vs "Casualties reported" (semantically similar but factually different)

**Required Solution:** Use higher similarity threshold for bullets:
- Summaries: 0.85 threshold (current behavior)
- Bullets: 0.92 threshold (stricter to avoid false merges)
- Config: `BULLET_DEDUP_THRESHOLD` (default: 0.92)

### 6. Item Inclusion Logic

**Problem:** Undefined behavior when bullets have mixed importance:
- Include item if 1 bullet is "High" but 3 are "Irrelevant"?
- How to handle partial relevance?

**Required Solution:** Define clear inclusion rules:
- Item included if any bullet meets importance threshold
- Only included bullets are rendered
- Track `included_bullet_count` vs `total_bullet_count`

### 7. Corroboration Rendering

**Problem:** If bullet is backed by 5 channels, only "primary source" shown:
- Ignores corroboration value
- "5 channels report X" is more credible than "1 channel reports X"

**Required Solution:** Show corroboration count for deduplicated bullets:
- Format: `• Claim text (5 sources)`
- Or: `• Claim text (via @primary +4)`

## Relationship to Editor Mode

The codebase has Editor Mode (`internal/output/digest/render_clusters.go`) that generates cohesive narratives from clusters. This feature is currently disabled in production.

**Key distinction:**
- **Editor Mode:** Generates prose narrative from cluster items
- **Bulletized Output:** Extracts and scores individual claims

These features serve different use cases:
- Editor Mode: "Tell me the story"
- Bulletized Output: "Show me the key facts"

Both features can coexist, but bulletized output must be implemented with the cluster interaction strategy defined above to avoid noise amplification.

## Configuration Summary

| Variable | Default | Description |
|----------|---------|-------------|
| `BULLET_BATCH_SIZE` | `3` | Max bullets extracted per message |
| `BULLET_DEDUP_THRESHOLD` | `0.88` | Similarity threshold for dedup |
| `BULLET_SOURCE_ATTRIBUTION` | `true` | Show source channel |
| `BULLET_SOURCE_FORMAT` | `compact` | Attribution format (`full` or `compact`) |
| `BULLET_MAX_PER_CLUSTER` | `2` | Max bullets per cluster |
| `BULLET_MIN_IMPORTANCE` | `0.4` | Minimum importance threshold |

## Next Steps

1. Define cluster aggregation strategy in detail
2. Implement LLM-based extraction prompt with topic inheritance fallback
3. Add bullet deduplication with 0.92 threshold
4. Implement cluster-level bullet limiting (AFTER dedup - merge identical claims first, then pick top N unique)
5. Update digest renderer for bullet format
6. Add A/B testing to compare with summary-based output
7. Monitor output token costs vs budget guardrails

## References

- Existing infrastructure: `internal/core/domain/bullet.go`
- Migration: `migrations/20260128001000_add_item_bullets.sql`
- Editor Mode: `internal/output/digest/render_clusters.go`
