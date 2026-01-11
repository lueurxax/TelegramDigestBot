# Proposal: Content Quality Improvement Roadmap

## Problem Statement

Digest quality depends on multiple weak links: noisy sources, inconsistent relevance scoring, redundant items, and limited feedback. We need a systematic plan to improve signal-to-noise while keeping operator effort low.

## Goals

- Reduce low-value items in the digest without losing important coverage.
- Improve perceived coherence (fewer duplicates, clearer grouping).
- Provide measurable quality signals for iteration and A/B testing.

## Proposed Initiatives

### 1) Feedback Loop (Ratings -> Calibration)
- Status: partially implemented (`item_ratings` table, `/feedback <item_id> <good|bad|irrelevant> [comment]` command).
- Aggregation: weekly job computes rolling 30-day ratios per channel and globally (good/bad/irrelevant), with exponential decay (e.g., weight = exp(-age_days * ln(2) / 14)).
- Threshold tuning: adjust relevance/importance thresholds in small steps (e.g., ±0.05) based on net score; clamp to safe bounds and log deltas.
- Guardrails: require minimum sample size per channel (e.g., ≥15) and a global minimum (e.g., ≥100) before auto-tuning; cap per-week change; fallback to defaults when confidence is low.
- Prompt tuning: store prompt templates in `settings` with versioned keys (e.g., `prompt:summarize:v3`, `prompt:cluster:v2`) and keep history for rollback.

### 2) Relevance Gate Before Summarization
- Clarify scope: this is a **pre-LLM gate** before the full `ProcessBatch` summarization step, not a replacement for the existing `RelevanceScore`.
- Mechanisms:
  - **Cheap model gate**: small/fast LLM or embedding-based classifier returns `relevant|irrelevant` + confidence.
  - **Rubric prompt**: a short, fixed checklist prompt (e.g., “Is this actionable news? Is it time-sensitive? Is it non-duplicative?”) scored by a fast model.
  - **Heuristic fallback**: simple rules for spam/ads/empty content when model access is unavailable.
- Interaction with existing threshold: items that pass the gate still get the full `RelevanceScore`; items that fail are marked `rejected` before summarization (no cost).
- Logging: add a lightweight `relevance_gate_log` table or a `raw_messages.gate_result` JSONB column with `{decision, confidence, reason, model, gate_version}`.

### 3) Stronger Dedup and Clustering
- Status: partially implemented (hash-based dedup, semantic clustering, cluster summaries).
- Improve means: tune similarity thresholds, reduce false merges, and consolidate across near-duplicate sources.
- Options:
  - **Threshold tuning**: lower/raise `SIMILARITY_THRESHOLD` and measure duplicate rate vs. missed merges.
  - **Algorithm tweaks**: add time-window constraints, require minimum overlap in named entities, or use multi-stage clustering (topic then semantic).
  - **Embedding model upgrades**: current model is OpenAI `text-embedding-3-small`; switch to higher-quality embeddings when cost allows.
- Cross-topic clustering: optional mode to allow merges across topics when semantic similarity is high; default stays topic-first to avoid accidental merges.
- Output: ensure a single cluster summary with multiple sources, and keep `SimilarItemID` links for traceability.

### 4) Source Credibility Signals
- Status: partially implemented via Channel Importance Weight (per-channel multiplier + manual `/channel weight`).
- Track per-channel reliability scores (manual + stats-driven).
- Downweight chronic low-trust sources in importance (current implementation).
- **Gap**: higher relevance requirements for weak sources are not implemented.
- Proposed mechanism for relevance gating:
  - Use existing `channels.relevance_threshold` as the **base** threshold.
  - Reliability score is normalized to 0–1 (higher is better). Compute penalty as `penalty = (1.0 - reliabilityScore) * factor` (default `factor = 0.2`) and apply: `effective = clamp(base + penalty, min, max)`.
  - Persist auto adjustments in new fields (e.g., `auto_relevance_enabled`, `relevance_threshold_delta`), updated weekly alongside weights.
  - Manual override disables auto adjustments and freezes `effective` at the base value.

### 5) Topic Balance Rules
- Define topic source: default to LLM `Topic` string, normalized (lowercase + trimmed). Optional taxonomy map in settings for stable buckets.
- Diversity cap: no topic exceeds 30% **of item count** per digest (configurable). If not enough topics exist, relax the cap and log.
- Freshness: apply decay to **importance score** based on `tg_date` (e.g., `importance *= exp(-age_hours/36)`), with a minimum floor (e.g., 0.4) to avoid starving long-running stories.
- Coverage floor: require at least N distinct topics (e.g., 3) **if available**; otherwise accept fewer and emit a warning.

### 6) Evaluation Harness
- Labeled set sources:
  - Manual annotation queue (admin-reviewed samples).
  - Historical ratings mapped to labels (good/bad/irrelevant).
  - A curated “golden set” stored in `docs/eval/` or a small DB table.
- CI integration: add a GitHub Actions job that runs on PRs or nightly; compare metrics against a baseline and fail on regression thresholds (e.g., precision ≥ 0.80, noise_rate ≤ 0.15).
- A/B tests: requires feature flags + user segmentation; phase in later with a minimal holdout (e.g., 10%) and rating-based metrics only.

## Phased Plan

### Phase 1 (Quick Wins)
- Add rating dashboards + aggregation job (capture already exists).
- Add simple relevance gate and logging.
- Tune `SimilarityThreshold` (config) with measured targets (e.g., 0.85–0.95) and track duplicate/merge rates.
- Source credibility scoring: validate current channel weight usage and add reporting.

### Phase 2 (Mid-Term)
- Add topic diversity and freshness rules.
- Standardize prompts for cluster summaries.

### Phase 3 (Long-Term)
- Build evaluation harness and CI checks.
- Run A/B tests and automated regressions.
- Establish an ops cadence for reviewing thresholds and model changes.

## Success Metrics

- Lower “noise rate”: `(bad + irrelevant) / total rated` over a 30-day window.
- Higher average item rating and digest engagement (proxy: rating volume + avg score).
- Fewer duplicates per digest (cluster count vs. item count ratio).
- Stable coverage across key topics (admin-defined list in settings or top-N observed topics).

## Database Schema Changes

- `relevance_gate_log` table or `raw_messages.gate_result` JSONB column.
- `channels.auto_relevance_enabled` (bool, default true).
- `channels.relevance_threshold_delta` (float, default 0.0).
- Prompt templates stored in `settings` with versioned keys (`prompt:*`).
- Migration note: add to `20260111120000_add_importance_weight.sql` or create a follow-up like `20260112000000_add_auto_relevance.sql`.

## Config Variables

- `RELEVANCE_GATE_ENABLED` (bool)
- `TOPIC_DIVERSITY_CAP` (float, default 0.30)
- `FRESHNESS_DECAY_HOURS` (int, default 36)
- `FRESHNESS_FLOOR` (float, default 0.4)
- `MIN_TOPIC_COUNT` (int, default 3)
- `RATING_MIN_SAMPLE_CHANNEL` (int, default 15)
- `RATING_MIN_SAMPLE_GLOBAL` (int, default 100)

## Risks & Mitigations

- Risk: Over-filtering reduces coverage. Mitigation: caps, review thresholds.
- Risk: Sparse feedback. Mitigation: default priors, slower updates.
- Risk: Bias toward popular channels. Mitigation: diversity rules.
