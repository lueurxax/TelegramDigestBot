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
- Track tokens per model per day.
- Optional soft budget (warn) and hard cap (force fallback).

### 2) Per-Model Rate Limits (RPM)
- Configure RPM per provider/model.
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

## Configuration
```bash
# Daily budget (tokens)
LLM_BUDGET_DAILY_TOKENS=500000

# Per-model token budgets (override)
LLM_MODEL_BUDGETS="gpt-5-nano:200000,gemini-2.0-flash-lite:300000"

# Per-model RPM caps
LLM_MODEL_RPM="gpt-5-nano:120,gemini-2.0-flash-lite:300"
```

## Success Criteria
- No pipeline failures due to provider quota spikes.
- Controlled costs with minimal quality regression.

## Open Questions
- Should budget windows be UTC or local time?
- Should caps apply per task or global per model?
