# Annotation-Driven Processing Improvements (No New Configs)

## Summary
Use existing item annotations (good / bad / irrelevant) to immediately improve digest quality without increasing LLM usage. This proposal adds four low-cost mechanisms:
1) Recent-rating channel bias (fast feedback)
2) Irrelevant-similar suppression (semantic quarantine)
3) Uncertain-item sampling (better labeling efficiency)
4) Low-reliability caution styling (optional, non-LLM)

No new environment variables or settings are introduced. All parameters are code constants with sane defaults.

## Goals
- Improve quality quickly based on user feedback.
- Reduce repeated low-signal items (near-duplicates of irrelevant items).
- Increase annotation value by focusing on borderline items.
- Keep compute cost flat (no extra LLM calls).

## Non-Goals
- Replacing existing relevance/importance models.
- New admin settings or runtime toggles.
- Changing ingest logic or message history behavior.

## Current Behavior
- Annotations are saved in `item_ratings`.
- Ratings are used mostly by weekly aggregation and optional auto-relevance/auto-weight jobs.
- They do not affect immediate selection/processing of new items.

## Proposed Changes

### 1) Recent-Rating Channel Bias (Fast Feedback)
**Idea:** Apply a small, bounded bias to importance/relevance based on recent channel ratings.

**Data:** `item_ratings` + item->channel join.

**Window:** fixed lookback window (default 14 days), implemented as a code constant (no new config).

**Formula (per channel):**
- `total = good + bad + irrelevant`
- `net = good - (bad + irrelevant)`
- `score = net / max(1, total)`
- `bias = clamp(score * 0.12, -0.12, 0.12)`

**Minimum sample size:**
- Only apply bias when `total >= 10` within the lookback window.
- Otherwise, `bias = 0`.

**Time decay (fast feedback):**
- Apply exponential decay to each rating by age in days:
  - `weight = exp(-age_days * ln(2) / 7)` (7‑day half‑life).
- Use weighted counts for `good`, `bad`, `irrelevant`.

**Application:**
- `importance = clamp(importance + bias, 0, 1)`
- (optional) `relevance = clamp(relevance + bias*0.5, 0, 1)`

**Why:** immediate feedback loop without re-tuning thresholds or changing configs.

---

### 2) Irrelevant-Similar Suppression (Semantic Quarantine)
**Idea:** If a new item is highly similar to a recently marked *irrelevant* item, downrank or reject it.

**Data:** `embeddings` + `item_ratings` (irrelevant only).

**Query:**
- Find the most similar irrelevant item in the last N days (default 7 days, code constant).
- Use cosine distance with pgvector (existing index).

**Policy:**
- If similarity >= 0.92 (code constant), apply a penalty:
  - `importance -= 0.15` and `relevance -= 0.10` (clamped).
- If similarity >= 0.96, hard reject as `low_signal`.

**Notes:**
- This only uses existing embeddings and item_ratings.
- Prevents repeated low-signal variants of the same content.

---

### 3) Uncertain-Item Sampling for Annotation
**Idea:** Prioritize items near decision boundaries for annotation UI.

**Uncertainty score:**
- `u = clamp(1 - min(|importance - I_th|, |relevance - R_th|) / max(I_margin, R_margin), 0, 1)`
- Treat items with higher `u` as "reviewable".

**Margins:**
- `I_margin = 0.10` and `R_margin = 0.10` (code constants).
- This makes items within ±0.10 of either threshold high‑priority for review.

**Where used:**
- Research search view: add a “Needs Review” filter and a badge on rows.
- Default search ordering can boost `u` for quick annotation sessions.

**No new config:** thresholds use existing runtime values (importance/relevance thresholds already in use).

---

### 4) Low-Reliability Caution Styling (Optional, Non-LLM)
**Idea:** When a channel’s recent rating score is negative beyond a small threshold, mark items with a subtle caution indicator.

**Display options (pick one):**
- Append `⚠️` to the item line in the digest.
- Add `"Low confidence source"` badge in expanded view and research UI.

**Rules:**
- Only applies when `score <= -0.25` and `total >= 10` within the lookback window.
- Does not alter summary text (no hedging transformations).

## Data / SQL
No new tables required. Add queries:
- `GetChannelRatingSummary(window)` → per-channel counts of good/bad/irrelevant.
- `FindSimilarIrrelevantItem(embedding, since)` → nearest irrelevant item.

**Indexes:**
- `item_ratings (created_at)` already present via table; add if missing.
- `embeddings` uses pgvector index (existing).

## Metrics
Add counters and gauges:
- `annotation_bias_applied_total{channel}`
- `irrelevant_similarity_hits_total`
- `irrelevant_similarity_rejects_total`
- `irrelevant_similarity_score` (histogram)
- `uncertainty_flagged_total`
- `low_reliability_badge_total`

## UX Changes
- Research search table gains a “Needs Review” filter and badge.
- Expanded view shows rating source and optional reliability badge.

## Testing Strategy
- **Shadow mode:** compute bias/suppression decisions and log impacts without applying them for 7 days; compare ready/rejected counts and quality signals vs baseline.
- **Replay sample:** run a fixed 7‑day message sample through current pipeline vs. new logic; verify no large regressions in acceptance rate (±5%) and that irrelevant‑similar hits increase.
- **Guardrails:** add unit tests for bias math, decay weights, and similarity thresholds; add integration test that ensures hard‑reject only triggers above the configured similarity.
- **Rollout gating:** enable full behavior only if shadow metrics show neutral or positive impact on relevance/importance distributions.

## Rollout Plan
1) Deploy bias + suppression with conservative thresholds.
2) Enable UI badges for “Needs Review”.
3) Monitor metrics and adjust constants in code if necessary.

## Acceptance Criteria
- 15–25% reduction in repeated low-signal items (measured by irrelevant-similarity hits).
- Increased annotation yield: >30% of annotations come from “Needs Review” items.
- No increase in LLM requests.
