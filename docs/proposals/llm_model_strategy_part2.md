# LLM Model Strategy Part 2: Additional Tasks

## Overview

This document extends the [LLM Model Strategy](llm_model_strategy.md) to cover additional LLM tasks not addressed in the original proposal:

- **Translation** (summary localization)
- **CompleteText** (claim extraction for enrichment)
- **RelevanceGate** (message filtering)
- **CompressSummariesForCover** (digest cover preparation)

---

## Task Analysis

### Current Usage

| Task | Location | Volume | Purpose |
|------|----------|--------|---------|
| TranslateText | `pipeline.go:812`, `translation_adapter.go:24` | High (every non-target-lang summary) | Translate summaries to target language |
| CompleteText | `enrichment/worker.go:437`, `extractor.go:260` | High (every enrichment query) | Extract claims from external sources |
| RelevanceGate | `relevance_gate.go:88` | High (every message, when enabled) | Filter spam/irrelevant content |
| CompressSummariesForCover | `handlers.go:2878`, `digest.go:866` | Very low (1 per digest) | Compress summaries for image generation |

---

## LLM Task → Model Mapping

### 1. Translate

**Purpose:** Translate summaries from source language to target language (usually Russian).

| Attribute | Value |
|-----------|-------|
| Volume | High (every non-target-language message) |
| Complexity | Low (straightforward translation) |
| Output | Translated text |

**Model mapping:**

| Priority | Model | Provider | Cost (1M tokens) |
|----------|-------|----------|------------------|
| Default | `meta-llama/llama-3.1-8b-instruct` | OpenRouter | $0.05 in / $0.05 out |
| Fallback 1 | `gpt-5-nano` | OpenAI | $0.05 in / $0.40 out |

**Rationale:**
- Llama 3.1 8B excels at translation tasks with minimal cost
- OpenRouter provides access to open-source models at competitive prices
- GPT-5-nano as fallback ensures reliability

---

### 2. CompleteText (Claim Extraction)

**Purpose:** Extract factual claims from external sources for evidence enrichment.

| Attribute | Value |
|-----------|-------|
| Volume | High (multiple per enrichment query) |
| Complexity | Moderate (structured extraction) |
| Output | JSON with extracted claims |

**Model mapping:**

| Priority | Model | Provider | Cost (1M tokens) |
|----------|-------|----------|------------------|
| Default | `meta-llama/llama-3.1-8b-instruct` | OpenRouter | $0.05 in / $0.05 out |
| Fallback 1 | `gpt-4o-mini` | OpenAI | $0.15 in / $0.60 out |

**Rationale:**
- High volume task requires cost-effective model
- Llama 3.1 8B provides excellent extraction at minimal cost
- GPT-5-mini fallback ensures quality for structured JSON extraction

---

### 3. RelevanceGate

**Purpose:** Filter out spam, ads, and irrelevant content before processing.

| Attribute | Value |
|-----------|-------|
| Volume | Very high (every message when enabled) |
| Complexity | Low (binary classification) |
| Output | JSON with decision, confidence, reason |

**Model mapping:**

| Priority | Model | Provider | Cost (1M tokens) |
|----------|-------|----------|------------------|
| Default | `meta-llama/llama-3.1-8b-instruct` | OpenRouter | $0.05 in / $0.05 out |
| Fallback 1 | `gpt-5-nano` | OpenAI | $0.05 in / $0.40 out |

**Rationale:**
- Highest volume task - Llama 3.1 8B is most cost-effective
- Simple yes/no decision works well with smaller models
- Consistent with Translation and CompleteText for simplified management

---

### 4. CompressSummariesForCover

**Purpose:** Compress verbose summaries into short English phrases for image generation prompts.

| Attribute | Value |
|-----------|-------|
| Volume | Very low (1 per digest) |
| Complexity | Moderate (creative compression) |
| Output | List of short phrases |

**Model mapping:**

| Priority | Model | Provider | Cost (1M tokens) |
|----------|-------|----------|------------------|
| Default | `gpt-4o-mini` | OpenAI | $0.15 in / $0.60 out |
| Fallback 1 | `openai/gpt-oss-120b` | OpenRouter | ~$0.10 in / ~$0.30 out |

**Rationale:**
- Very low volume allows higher quality model
- Creative task benefits from GPT-4o-mini's capabilities
- Output feeds into image generation, quality matters

---

## Updated Task Configuration

### Complete Task → Provider Matrix

| Task | Default | Fallback 1 | Fallback 2 |
|------|---------|------------|------------|
| **Summarize** | Google (gemini-2.0-flash-lite) | OpenAI (gpt-5-nano) | - |
| **Cluster Summary** | Cohere (command-r) | OpenAI (gpt-5) | Google (gemini-2.0-flash-lite) |
| **Cluster Topic** | OpenAI (gpt-5-nano) | Google (gemini-2.0-flash-lite) | OpenRouter (mistral-7b-instruct) |
| **Narrative** | Google (gemini-2.0-flash-lite) | Anthropic (claude-haiku-4.5) | OpenAI (gpt-5.2) |
| **Translate** | OpenRouter (llama-3.1-8b-instruct) | OpenAI (gpt-5-nano) | - |
| **CompleteText** | OpenRouter (llama-3.1-8b-instruct) | OpenAI (gpt-4o-mini) | - |
| **RelevanceGate** | OpenRouter (llama-3.1-8b-instruct) | OpenAI (gpt-5-nano) | - |
| **Compress** | OpenAI (gpt-4o-mini) | OpenRouter (gpt-oss-120b) | - |
| **ImageGen** | OpenAI (gpt-image-1.5) | - | - |

---

## Cost Estimation (Monthly Addition)

Assuming 10,000 messages/day, 30 digests/month:

| Task | Calls/Month | Tokens/Call | Model | Cost |
|------|-------------|-------------|-------|------|
| Translate | 150,000 | 300 | llama-3.1-8b-instruct | ~$2.25 |
| CompleteText | 100,000 | 500 | llama-3.1-8b-instruct | ~$2.50 |
| RelevanceGate | 300,000 | 200 | llama-3.1-8b-instruct | ~$1.50 |
| Compress | 30 | 1,000 | gpt-4o-mini | ~$0.02 |
| **Subtotal Part 2** | | | | **~$6.25/month** |

**Combined with Part 1:** ~$50 + ~$6.25 = **~$56.25/month**

---

## Environment Variables

```bash
# Additional to Part 1
OPENROUTER_API_KEY=sk-or-...

# Per-task model overrides (optional)
LLM_TRANSLATE_MODEL=meta-llama/llama-3.1-8b-instruct
LLM_COMPLETE_MODEL=meta-llama/llama-3.1-8b-instruct
LLM_RELEVANCE_GATE_MODEL=meta-llama/llama-3.1-8b-instruct
LLM_COMPRESS_MODEL=gpt-4o-mini
```

---

## Implementation Changes

### task_config.go Updates

```go
// Translate: OpenRouter (Llama) → OpenAI
TaskTypeTranslate: {
    Default: ProviderModel{Provider: ProviderOpenRouter, Model: "meta-llama/llama-3.1-8b-instruct"},
    Fallbacks: []ProviderModel{
        {Provider: ProviderOpenAI, Model: "gpt-5-nano"},
    },
},

// CompleteText: OpenRouter (Llama) → OpenAI
TaskTypeComplete: {
    Default: ProviderModel{Provider: ProviderOpenRouter, Model: "meta-llama/llama-3.1-8b-instruct"},
    Fallbacks: []ProviderModel{
        {Provider: ProviderOpenAI, Model: "gpt-4o-mini"},
    },
},

// RelevanceGate: OpenRouter (Llama) → OpenAI
TaskTypeRelevanceGate: {
    Default: ProviderModel{Provider: ProviderOpenRouter, Model: "meta-llama/llama-3.1-8b-instruct"},
    Fallbacks: []ProviderModel{
        {Provider: ProviderOpenAI, Model: "gpt-5-nano"},
    },
},

// Compress: OpenAI → OpenRouter
TaskTypeCompress: {
    Default: ProviderModel{Provider: ProviderOpenAI, Model: "gpt-4o-mini"},
    Fallbacks: []ProviderModel{
        {Provider: ProviderOpenRouter, Model: "openai/gpt-oss-120b"},
    },
},
```

---

## Summary

This proposal completes the LLM model strategy by covering all remaining tasks:

- **Translation, CompleteText & RelevanceGate** use cost-effective Llama 3.1 8B via OpenRouter
- **Compress** uses GPT-4o-mini for quality (very low volume)
- **Total estimated cost:** ~$56/month for complete pipeline

**Immediate action:** Update `task_config.go` with the new task configurations and add `OPENROUTER_API_KEY` to deployment secrets.
