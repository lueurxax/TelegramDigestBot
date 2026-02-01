# LLM Budget Guardrails and Per-Model Rate Limits

> **Status: PROPOSAL** (January 2026)
>
> Focus: control costs and avoid outages by throttling and falling back per model/provider.

## Summary
Introduce per-model budgets and rate limits. When a model hits its budget or RPM cap, requests automatically fall back to a cheaper model/provider in the same task chain.

## Goals
- Prevent runaway costs without disrupting the pipeline.
- Ensure tasks continue with fallbacks when limits are reached.
- Provide clear visibility into throttling events.

## Non-Goals
- Perfect cost accuracy across providers.
- Per-user billing or quota enforcement.

## Design

### 1) Per-Model Budget Tracking
- Track tokens per model per day (UTC day boundary).
- Count both input + output tokens; if provider returns usage, trust it; otherwise estimate via tokenizer.
- Optional soft budget (warn) and hard cap (force fallback).

### 2) Per-Model Rate Limits (RPM)
- Configure RPM per provider/model.
- Enforce via token-bucket limiter per model (burst = RPM / 2 by default).
- When RPM is exceeded, skip to fallback in the task chain.

### 3) Fallback Behavior
- Use the existing task fallback chain in `llm.TaskProviderChain`.
- If all fallbacks are blocked, return a clear error and defer processing.

### 4) Observability
- Metrics:
  - `digest_llm_budget_exhausted_total{model,task}`
  - `digest_llm_rpm_throttled_total{model,task}`
  - `digest_llm_fallback_used_total{model,task}`
- Logs with reason: `budget_exhausted` / `rpm_throttled` / `provider_error`.

## Data Storage
Add a daily usage table (UTC buckets) to coordinate across replicas:
```
llm_usage_daily (
  day DATE NOT NULL,
  model TEXT NOT NULL,
  task TEXT NOT NULL,
  tokens_in BIGINT NOT NULL,
  tokens_out BIGINT NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (day, model, task)
)
```
Notes:
- Upsert on each request completion.
- If task-level tracking is too heavy, allow aggregation to `(day, model)` only.

## Token Accounting
- Preferred: provider usage metadata.
- Fallback: tokenizer estimate with model-specific encoding.
- If neither is available, count characters/4 as a coarse estimate (explicitly marked "approx").

## Concurrency / Multi-Instance
- Budget checks must be atomic across replicas.
- Use `UPDATE ... SET tokens_in = tokens_in + $n` with row-level locking for the day/model/task row.
- If the increment crosses the hard cap, record the rejection and fallback.

## Configuration
```bash
# Daily budget (tokens)
LLM_BUDGET_DAILY_TOKENS=500000

# Per-model token budgets (override)
LLM_MODEL_BUDGETS="gpt-5-nano:200000,gemini-2.0-flash-lite:300000"

# Per-model RPM caps
LLM_MODEL_RPM="gpt-5-nano:120,gemini-2.0-flash-lite:300"
```

## Error Handling
- If a model is blocked and no fallback exists: return a throttling error and enqueue retry.
- If usage tracking fails (DB error): allow request but emit `budget_tracking_error` log/metric.

## Testing Strategy
- Unit: budget increment, cap crossing, RPM limiter behavior.
- Integration: concurrent increments across goroutines; verify no cap bypass.
- E2E: simulate burst traffic, verify fallback selection.

## Success Criteria
- No pipeline failures due to provider quota spikes.
- Controlled costs with minimal quality regression.

## Open Questions
None (decided):
- Budget window uses UTC days.
- Tracking is per model per task (can be aggregated later if needed).
