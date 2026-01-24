# LLM Model Strategy for Digest Pipeline

## Overview

This document defines the **LLM model selection strategy** for all prompt-driven stages of the digest pipeline:
- summarization
- clustering
- topic labeling
- narrative generation

The strategy is designed to:
- maximize output quality where it matters
- control and predict costs
- avoid silent degradation or skipped steps
- allow **runtime model overrides via bot commands**
- keep defaults safe and production-ready

All models listed here are **defaults in code**, but **can be overridden at runtime** through bot commands without redeployment.

---

## Design Principles

1. **Task–model alignment**  
   Each prompt type uses a model appropriate to its cognitive complexity.

2. **No silent skips**  
   If the primary model is unavailable or budget is exhausted, the system must still produce output using a fallback model.

3. **Defaults first, overrides explicit**  
   Code defaults must work well without operator intervention. Overrides are intentional and visible.

4. **Prompt-compatible fallbacks**  
   Fallback models receive simplified versions of the same prompt, not entirely different logic.

---

## Prompt → Model Mapping (Defaults)

### 1. Summarize

**Purpose**
- Generate a factual one-sentence summary
- Assign relevance and importance scores
- Detect topic and language
- Output structured JSON

**Characteristics**
- High volume
- Moderate reasoning
- Strict output format

**Default model**
```
gpt-5-nano
```

**Fallback model**
```
gemini-1.5-flash-8b
```

**Fallback behavior**
- Simplified formatting
- Slightly less precise scoring
- Facts preserved, style simplified

---

### 2. ClusterSummary

**Purpose**
- Merge multiple related summaries into a single coherent statement
- Remove duplication
- Preserve key facts

**Characteristics**
- Multi-document reasoning
- Low volume, high importance

**Default model**
```
gpt-5 (or gpt-5.2)
```

**Fallback model**
```
cohere-command-r
```

**Fallback behavior**
- More generic phrasing allowed
- Minor duplication tolerated
- Still preferable to skipping clustering

---

### 3. ClusterTopic

**Purpose**
- Generate a short (2–4 words) topic label for a cluster

**Characteristics**
- Very low complexity
- Pure classification / labeling

**Default model**
```
gpt-5-nano
```

**Fallback model**
```
mistral-7b-instruct
```

**Fallback behavior**
- Slightly more generic labels
- Acceptable variance in wording

---

### 4. Narrative

**Purpose**
- Generate the final digest text
- Group stories, highlight importance, provide context
- User-facing, editorial-style output

**Characteristics**
- Long-form generation
- Narrative coherence
- Style and tone matter

**Default model**
```
gpt-5 (or gpt-5.2)
```

**Fallback model**
```
claude-haiku-4.5
```

**Fallback behavior**
- More factual and compact style
- Less expressive language
- Still coherent and readable

---

## Runtime Model Overrides (Bot-Controlled)

### Resolution Order

For each prompt type:

```
override model (if set)
→ default model
→ fallback model (if default fails)
```

---

### Bot Commands

Set override:
```
/llm set summarize gpt-5-mini
/llm set narrative gpt-5.2
/llm set cluster_summary cohere-command-r
```

Reset overrides:
```
/llm reset summarize
/llm reset all
```

View current configuration:
```
/llm status
```

---

### Storage Model

Overrides are stored in persistent configuration (DB or KV):

```json
{
  "summarize": null,
  "cluster_summary": "cohere-command-r",
  "cluster_topic": null,
  "narrative": null
}
```

`null` means "use default".

---

## Fallback Prompt Adjustments

When a fallback model is active, the system MAY:
- disable HTML formatting constraints
- reduce strict numeric calibration
- limit output length
- force single-language output

This is handled programmatically, not via separate prompt definitions.

---

## Summary

This strategy provides:
- strong defaults
- explicit fallbacks
- runtime overrides
- graceful degradation instead of silent failure

It aligns with the core philosophy of the project:

> Prefer correctness and continuity over perfect but fragile solutions.

