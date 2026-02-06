# Bulletability Gate for Digest Bullet Extraction

> **Status: PROPOSAL** (February 2026)
>
> Focus: avoid low-quality/duplicated bullets by deciding first whether a message is actually bullet-friendly.

## Summary
Before bullet extraction, add a **bulletability gate**:
1. Try deterministic regex/structure detection.
2. If uncertain, ask a lightweight LLM classifier (`bulletable` vs `not_bulletable`).
3. If `not_bulletable`, extract exactly **one** bullet from the text.
4. If `bulletable`, extract multiple bullets as today.

This reduces repeated bullets from narrative text and keeps bullet output aligned with message structure.

## Goals
- Reduce duplicated or paraphrased-near-duplicate bullets.
- Keep dense narrative messages as a single concise bullet.
- Preserve multi-point posts as multi-bullet output.

## Non-Goals
- Perfect semantic parsing of every writing style.
- Replacing the current bullet extraction model.
- Adding a new independent dedup pipeline stage.

## Design

### 1) Deterministic Bulletability Detection (first pass)
Implement in `internal/process/pipeline/bullet_extraction.go` and call from
`processBullets()` in `internal/process/pipeline/pipeline.go`, immediately before
`extractBullets()`.

Compute a deterministic score `s` in `[0,1]`:
- `marker_lines >= 3` (`-`, `•`, `*`, `—`, `\d+[\.\)]`) => `+0.5`
- `marker_lines == 2` => `+0.35` (intentional: often inconclusive without other structure)
- `short_lines >= 3` (line length <= 120 runes) => `+0.2` (except when `marker_lines == 2`, to keep two-marker cases in inconclusive band)
- `newline_density >= 0.04` (`newlines / rune_count`) => `+0.2`
- `heading_separator_pattern` (repeated `Title:`/`—`-style section separators) => `+0.1`

Decision:
- `s >= 0.65` => `bulletable=true`
- `s <= 0.35` => `bulletable=false`
- `0.35 < s < 0.65` => inconclusive (use LLM classifier fallback)

These weights are a starting point. Calibrate on a labeled production sample (>= 300 messages)
after rollout and tune for minimum fallback usage while preserving recall for true list-form posts.

### 2) LLM Bulletability Classifier (fallback)
If deterministic score is inconclusive, run a cheap classifier prompt returning strict JSON:
```json
{"bulletable": true|false, "reason": "..."}
```
Classifier only decides format suitability; it does not generate bullets.

Implementation details:
- Use existing `TaskTypeRelevanceGate` chain (cheap binary-classification task path), no new model config.
- Input budget target: <= 900 characters of combined context (truncate deterministically).
- Expected output: <= 30 tokens JSON payload.
- Timeout: 1200ms. On timeout/error, use deterministic fallback rule from Performance section.
- Prompt sketch:
  - "Decide if this text should be split into multiple digest bullets."
  - "Return only JSON with keys `bulletable` and `reason`."
  - "`bulletable=true` only when the text contains multiple independent points."
  - "If it is one narrative/news statement, return `bulletable=false`."

### 3) Extraction Rule
- `bulletable=true`: use normal multi-bullet extraction.
- `bulletable=false`: force `max_bullets=1` and generate one high-signal bullet.

### 4) Interaction with Existing Length Rule
`applyBulletLengthRules()` already limits short content to one bullet.
Precedence:
1. bulletability gate sets extraction cap (`1` or default max),
2. extraction runs,
3. `applyBulletLengthRules()` applies current constraints (including short-source behavior and total-length trimming).

So the gate is an upstream control, and existing length safety remains authoritative.

### 5) Quality Guardrails (existing dedup, no new stage)
Do not add a fourth dedup layer. Keep and tune current dedup chain:
- extraction-time dedup (`dedupeExtractedBullets`),
- semantic dedup in bullet dedup worker,
- render-time text dedup (`dedupeBulletsByText`).

Only tighten normalization in existing helpers if needed.

## Pipeline Integration
Insert gate in bullet pipeline:
1. collect source context (message + link text if available)
2. deterministic bulletability scoring
3. optional LLM classifier (only inconclusive band)
4. set extraction cap (`1` or default)
5. extract bullets
6. apply existing length rules and existing dedup chain
7. scoring/rendering

## Performance and Fallback
- Deterministic stage is O(text length) and always on.
- LLM classifier runs only for inconclusive cases (target <= 15% of messages after tuning).
- If classifier call fails/timeouts, fallback to deterministic decision:
  - `s >= 0.5` => `bulletable=true`
  - `s < 0.5` => `bulletable=false`

## Migration / Backfill
- No schema migration required.
- No historical backfill; applies to newly processed messages only.

## Observability
Add metrics:
- `digest_bulletability_decision_total{result=bulletable|not_bulletable,source=deterministic|llm}`
- `digest_bulletability_score_bucket` with buckets `[0.1, 0.2, 0.35, 0.5, 0.65, 0.8, 1.0]`
- `digest_bullet_dedup_before_total`
- `digest_bullet_dedup_after_total`
- `digest_bullet_single_mode_total`

## Testing Strategy
- Narrative text (paragraph) => `not_bulletable` => exactly 1 bullet.
- Clear list-form text (e.g., 3+ marker lines, or 2 marker lines plus strong structure) => multi-bullet output preserved.
- Ambiguous text => deterministic inconclusive => classifier path exercised.
- Classifier failure/timeout => deterministic fallback exercised.
- Regression: no duplicated bullet lines after existing dedup chain.
- Determinism applies to deterministic branch only (LLM branch is non-deterministic by design).

Example fixtures (deterministic gate):
1. `"- A\n- B\n- C"`:
   - `marker_lines=3`, `short_lines=3`, high newline density => `s=0.9` => `bulletable=true`.
2. `"Long narrative paragraph without markers ..."`:
   - no markers, low newline density => `s=0.0` => `bulletable=false`.
3. `"— Point A\n— Point B\nContinuation text..."`:
   - `marker_lines=2` + newline density => `s=0.55` => inconclusive => LLM fallback.

## Rollout Plan
1. Ship gate behind existing bullet pipeline path.
2. Run with metrics monitoring for 48h.
3. Tune deterministic threshold/classifier prompt if single-bullet ratio is too high.

## Success Criteria
- Duplicate bullet rate drops by >= 30% (`1 - after_total / before_total`) vs pre-rollout baseline.
- >= 80% of narrative posts are emitted in single-bullet mode.
- >= 90% of clearly list-form posts preserve multi-bullet output (2+ bullets).
