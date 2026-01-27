# Bulletized Digest Output (Pre-Scoring Bullets)

> **Status: PROPOSED**
>
> Split each message into short bullets before scoring, then group bullets by importance tier and topic. Each bullet can optionally link to the expanded view of its source item.

## Summary
Today, each digest item is a single summary sentence. This proposal introduces bullet extraction ahead of scoring so each message can yield multiple concise claims. Bullets are scored, deduplicated, and grouped by importance tier and topic. The expanded view remains the full context; bullets are a compact entry point.

## Goals
- Make key claims easier to scan without losing context (expanded view).
- Score relevance/importance at the bullet level.
- Avoid additional LLM calls (batch within existing calls only).

## Non-Goals
- No new per-bullet LLM calls.
- No per-bullet author/source attribution in the digest (only in expanded view).

## UX (Example Format)
```
High Importance
Topic: War / Region
- Claim 1 ... (linked)
- Claim 2 ... (linked)

Medium Importance
Topic: International Politics
- Claim 1 ... (linked)
- Claim 2 ... (linked)
```

## Design

### Bullet Extraction (Pre-Scoring)
- Extract bullets from raw message text + preview text.
- Use sentence splitting + clause splitting on separators (".", ";", "—", ":").
- Remove boilerplate and empty lines; cap per message (default 6).
- If extraction fails, fall back to a single bullet containing the existing summary.

### Scoring & Topics
- Score bullets for relevance/importance inside the existing batch LLM call.
- Assign topic per bullet (same call), or inherit topic from the parent item if token budget is tight.
- Group output by **importance tier first**, then by topic.

### Deduplication
- Deduplicate bullets across items using semantic similarity and time window.
- When duplicates are found, keep the highest-scoring bullet and store the list of backing items for expanded view context.

### Expanded View Links
- Each bullet is a link to the expanded view for its primary source item.
- If expanded view is disabled, show plain bullets without links.

## Data Model
Add a lightweight `item_bullets` table:
- `id`, `item_id`, `bullet_index`, `text`
- `topic`, `relevance_score`, `importance_score`
- `bullet_hash`, `status`, `created_at`

Optionally store `bullet_cluster_id` for dedup groups.

## Configuration
Use existing configuration settings only; no new settings are introduced for this feature.

## Observability
- `bullet_count_total`, `bullet_dedup_dropped_total`
- `bullet_importance_avg`, `bullet_relevance_avg`
- `bullet_extraction_fail_total`

## Rollout
1. Implement bullet extraction + storage behind a code flag (no new config).
2. Shadow-mode scoring for 7 days (no user impact).
3. Enable bullet output for one digest window and compare engagement.

## Risks & Mitigations
- **Fragmentation risk:** cap bullets per item and keep expanded view as primary context.
- **Token budget pressure:** cap bullets and truncate long texts before LLM scoring.
- **Noise from duplicates:** apply semantic dedup and time window limits.
