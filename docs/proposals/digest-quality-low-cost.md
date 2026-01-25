# Digest Quality Improvements (No Extra LLM Cost)

> **Status: PROPOSAL** (January 2026)
>
> Focus: improve digest quality using heuristics, caching, and metadata. No additional LLM calls.

## Summary
Improve relevance, readability, and diversity of the digest without increasing LLM usage. Changes target pre-LLM filtering, deduplication, ranking adjustments, summary polishing, and stronger use of existing metadata (link previews, channel stats, ratings).

## Goals
- Reduce low-signal items and near-duplicate spam.
- Improve summary clarity and consistency.
- Boost items corroborated across channels.
- Make inclusion decisions more transparent.
- Keep LLM usage flat or lower.

## Non-Goals
- New LLM prompts or additional LLM calls.
- Full UI redesign (covered elsewhere).

## Proposed Changes

### 1) Smarter Pre-LLM Filters (Language-Aware)
- Use language-aware minimum length rules (RU/UK/EN can differ).
- Drop emoji-only, boilerplate promos, and empty forwarded shells.
- **Do not drop link-only posts**; instead use link preview data (title/description/site) as text.

**Heuristics:**
- If message text is short but has `webpage.title/description`, synthesize a surrogate text for scoring.
- Boilerplate denylist ("subscribe", "donate", "share", "promo").

### 2) URL Normalization + Domain Quality Signals
- Normalize URLs (strip tracking params, canonicalize paths).
- Add allowlist/denylist for low-value domains.
- Prefer reputable domains when summarizing clusters (ranking boost).

### 3) Telegram Link Preview Enrichment (No LLM)
- For short messages, append preview title/description to the text used for scoring.
- Store preview-derived snippet for display or evidence context.

### 4) Language Detection Normalization
- Detect language from original text + preview text + summary.
- Avoid false RU/UK flips by using a stable precedence (original > preview > summary).

### 5) Stronger Dedup
- Combine strict hash + semantic similarity + time window.
- Treat same-channel near-duplicates as one item within a time window.

### 6) Corroboration-Based Importance Adjustment
- Boost importance when multiple channels appear in same cluster.
- Penalize repeated single-source reposts.

### 7) Channel Reliability/Weight History
- Apply reliability decay and stronger penalties for high irrelevant rates.
- Promote channels with sustained high "good" ratings.

### 8) Summary Post-Processing (Heuristic)
- Enforce single-sentence and length cap.
- Remove hedging/boilerplate phrases ("reportedly", "it was said").
- Normalize punctuation and whitespace.

### 9) Lead Sentence Extraction Fallback
- If summary is weak, pick the best lead sentence from original/preview text.
- Prefer sentences with named entities and numeric facts.

### 10) Cluster Topic Cleanup
- Canonicalize topic labels (case, punctuation, synonyms).
- Deduplicate similar topics in a window.

### 11) Explainability Line
- Optional metadata line: why included (scores, corroboration, thresholds).

### 12) Ratings-Driven Threshold Tuning (Weekly)
- Adjust relevance/importance thresholds using rating history.

### 13) Time-to-Digest Tracking
- Track time from first seen to digest inclusion.
- Flag windows with quality drops (few items, low average importance).

### 14) Cache LLM Summaries for Exact Duplicates
- Reuse prior summary for exact duplicates to reduce LLM usage.

### 15) Reuse Cluster Summaries Across Windows
- If a cluster repeats with minimal change, reuse its previous summary.

## Settings / Config Additions
- `FILTER_MIN_LENGTH_RU`, `FILTER_MIN_LENGTH_EN`, `FILTER_MIN_LENGTH_UK`
- `DOMAIN_ALLOWLIST`, `DOMAIN_DENYLIST`
- `DEDUP_SAME_CHANNEL_WINDOW_HOURS`
- `CORROBORATION_IMPORTANCE_BOOST`
- `SINGLE_SOURCE_PENALTY`
- `SUMMARY_MAX_CHARS`, `SUMMARY_STRIP_PHRASES`
- `EXPLAINABILITY_LINE_ENABLED`
- `TIME_TO_DIGEST_ALERT_THRESHOLD`

## Success Criteria
- 15-25% reduction in rejected/low-signal items.
- Improved average importance without lowering topic diversity.
- Lower duplicate rate in a single digest.

## Rollout
- Stage 1: Enable preview-based enrichment + dedup window.
- Stage 2: Importance adjustments + summary post-processing.
- Stage 3: Explainability line + weekly tuning.

## Open Questions
- Exact heuristics for lead sentence extraction?
- Should domain quality signals affect relevance, importance, or both?
- How much transparency to show in public digests?
