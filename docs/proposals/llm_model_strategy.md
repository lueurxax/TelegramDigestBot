# LLM Model Strategy for Digest Pipeline

## Overview

This document defines the **LLM and embedding model selection strategy** for all AI-driven stages of the digest pipeline:
- **Embeddings** (semantic search, deduplication, clustering)
- **Summarization** (per-message summaries)
- **Clustering** (topic grouping and labeling)
- **Narrative generation** (final digest output)

The strategy is designed to:
- Maximize output quality where it matters
- Control and predict costs
- Avoid silent degradation or skipped steps
- Allow **runtime model overrides via bot commands**
- Support **multiple providers** for redundancy and cost optimization
- Keep defaults safe and production-ready

---

## Design Principles

1. **Task–model alignment**
   Each task uses a model appropriate to its cognitive complexity and volume.

2. **No silent skips**
   If the primary model is unavailable or budget is exhausted, the system must still produce output using a fallback model.

3. **Defaults first, overrides explicit**
   Code defaults must work well without operator intervention. Overrides are intentional and visible.

4. **Provider diversity**
   Critical paths should have fallbacks across different providers to avoid single-vendor outages.

5. **Cost awareness**
   High-volume tasks (embeddings, summarization) use cheaper models; low-volume, high-impact tasks (narrative) can use premium models.

---

## Provider Support

### Supported Providers

| Provider | Models | Use Cases |
|----------|--------|-----------|
| **OpenAI** | gpt-5, gpt-5.2, gpt-5-nano, text-embedding-3-large | Fallback LLM, primary embeddings |
| **Cohere** | command-r, command-r-plus, embed-multilingual-v3.0 | Embedding fallback (multilingual) |
| **Anthropic** | claude-haiku-4.5 | Narrative, complex reasoning |
| **Google** | gemini-2.0-flash-lite, gemini-embedding-001 | Primary LLM, embedding fallback |

### Provider Priority (Default)

```
LLM: Google → Anthropic → OpenAI
Embeddings: OpenAI → Cohere → Google
```

---

## Embeddings Strategy

### Current Problem

Embeddings are the highest-volume API calls:
- Every message needs an embedding for deduplication
- Every message needs an embedding for clustering
- Semantic search requires query embeddings

**This is why OpenAI quota exhaustion hits embeddings first.**

### Embedding Model Mapping

| Task | Default Model | Fallback | Dimensions |
|------|---------------|----------|------------|
| Message dedup | `text-embedding-3-large` | `embed-multilingual-v3.0` | 3072 / 1024 |
| Clustering | `text-embedding-3-large` | `embed-multilingual-v3.0` | 3072 / 1024 |
| Semantic search | `text-embedding-3-large` | `embed-multilingual-v3.0` | 3072 / 1024 |

### Cost Comparison (per 1M tokens)

| Model | Provider | Price | Notes |
|-------|----------|-------|-------|
| text-embedding-3-large | OpenAI | $0.13 | Best quality, 3072 dimensions |
| text-embedding-3-small | OpenAI | $0.02 | Good quality/price ratio |
| embed-multilingual-v3.0 | Cohere | $0.10 | Excellent multilingual support |
| gemini-embedding-001 | Google | $0.00 (free tier) | 3072 dimensions, 1500 req/min free |

### Recommended Configuration

For quality-first with multilingual fallback:

```yaml
EMBEDDING_PROVIDER_ORDER: "openai,cohere"
EMBEDDING_DEFAULT_MODEL: "text-embedding-3-large"
EMBEDDING_FALLBACK_MODEL: "embed-multilingual-v3.0"
```

**Rationale:**
- `text-embedding-3-large` provides highest quality embeddings (3072 dimensions)
- `embed-multilingual-v3.0` is excellent fallback with strong multilingual support (100+ languages)
- Both models handle Russian, Greek, and other non-English content well

---

## LLM Prompt → Model Mapping

### 1. Summarize

**Purpose:** Generate factual one-sentence summary with scores and metadata.

| Attribute | Value |
|-----------|-------|
| Volume | High (every message) |
| Complexity | Moderate |
| Output | Structured JSON |

**Model mapping:**

| Priority | Model | Provider | Cost (1M tokens) |
|----------|-------|----------|------------------|
| Default | `gpt-5-nano` | OpenAI | $0.05 in / $0.40 out |
| Fallback 1 | `gemini-2.0-flash-lite` | Google | $0.10 in / $0.40 out |

---

### 2. Cluster Summary

**Purpose:** Merge related summaries into coherent statement.

| Attribute | Value |
|-----------|-------|
| Volume | Low (per cluster) |
| Complexity | High (multi-document reasoning) |
| Output | Text |

**Model mapping:**

| Priority | Model                   | Provider | Cost (1M tokens)      |
|----------|-------------------------|----------|-----------------------|
| Default | `cohere-command-r`      | Cohere   | need update           |
| Fallback 1 | `gpt-5`                 | OpenAI   | $2.50 in / $10 out    |
| Fallback 2 | `gemini-2.0-flash-lite` | Google   | $0.10 in / $0.40 out |

---

### 3. Cluster Topic

**Purpose:** Generate 2-4 word topic label.

| Attribute | Value |
|-----------|-------|
| Volume | Low |
| Complexity | Low (classification) |
| Output | Short text |

**Model mapping:**

| Priority | Model                   | Provider | Cost (1M tokens)      |
|----------|-------------------------|----------|-----------------------|
| Default | `gpt-5-nano`            | OpenAI   | $0.05 in / $0.40 out  |
| Fallback 1 | `gemini-2.0-flash-lite` | Google   | $0.10 in / $0.40 out |
| Fallback 2 | `mistral-7b-instruct`   | OpenRouter     | $0.20 in /$0.20  out  |

---

### 4. Narrative

**Purpose:** Generate final user-facing digest text.

| Attribute | Value |
|-----------|-------|
| Volume | Very low (1 per digest) |
| Complexity | High (editorial, style matters) |
| Output | Long-form text |

**Model mapping:**

| Priority   | Model                       | Provider | Cost (1M tokens)      |
|------------|-----------------------------|----------|-----------------------|
| Default | `gemini-2.0-flash-lite`     | Google | $0.10 in / $0.40 out |
| Fallback 1 | `claude-haiku-4.5` | Anthropic | $1 in / $5 out        |
|  Fallback 2   | `gpt-5.2`                   | OpenAI | $2.50 in / $10 out    |

---

## Runtime Model Overrides

### Resolution Order

```
bot override → environment override → default → fallback chain
```

### Bot Commands

**Set override:**
```
/llm set summarize gpt-4o
/llm set narrative claude-haiku-4.5
/llm set embeddings gemini-embedding-001
```

**Reset overrides:**
```
/llm reset summarize
/llm reset all
```

**View configuration:**
```
/llm status
```

**Example output:**
```
LLM Configuration:
  embeddings: gemini-embedding-001 (google) [override]
  summarize: gpt-4o-mini (openai) [default]
  cluster_summary: gpt-4o (openai) [default]
  cluster_topic: gpt-4o-mini (openai) [default]
  narrative: gpt-4o (openai) [default]

Provider Status:
  openai: ⚠️ quota warning (80% used)
  google: ✓ healthy
  anthropic: ✓ healthy
  ollama: ✓ healthy (local)
```

---

### Storage Model

Overrides stored in `settings` table:

```sql
INSERT INTO settings (key, value) VALUES
  ('llm_override_embeddings', 'gemini-embedding-001'),
  ('llm_override_summarize', NULL),  -- NULL = use default
  ('llm_override_narrative', 'claude-haiku-4.5');
```

---

## Implementation Plan

### Phase 1: Multi-Provider Embeddings (Immediate)

1. Create `internal/core/embeddings/` package with provider interface
2. Implement providers: OpenAI, Google, Ollama
3. Add fallback chain with circuit breaker per provider
4. Configure Google as default (free tier)

### Phase 2: Multi-Provider LLM

1. Extend `internal/core/llm/` with provider abstraction
2. Implement providers: OpenAI, Anthropic, Google, Ollama
3. Add per-task model configuration
4. Implement `/llm` bot commands

### Phase 3: Cost Tracking

1. Add token counting per request
2. Store daily usage per provider/model
3. Add `/llm costs` command for reporting
4. Implement budget alerts

---

## Environment Variables

```bash
# Provider API Keys
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...
COHERE_API_KEY=...

# Embedding Configuration
EMBEDDING_PROVIDER_ORDER=openai,cohere
EMBEDDING_DEFAULT_MODEL=text-embedding-3-large
EMBEDDING_FALLBACK_MODEL=embed-multilingual-v3.0

# LLM Configuration
LLM_PROVIDER_ORDER=openai,anthropic,google,ollama
LLM_SUMMARIZE_MODEL=gpt-5-nano
LLM_CLUSTER_MODEL=gpt-5
LLM_NARRATIVE_MODEL=gpt-5.2

# Fallback behavior
LLM_FALLBACK_ENABLED=true
LLM_CIRCUIT_BREAKER_THRESHOLD=5
LLM_CIRCUIT_BREAKER_TIMEOUT=60s
```

---

## Cost Estimation (Monthly)

Assuming 10,000 messages/day, 30 digests/month:

| Task | Calls/Month | Tokens/Call | Model | Cost |
|------|-------------|-------------|-------|------|
| Embeddings | 300,000 | 500 | text-embedding-3-large | ~$19.50 |
| Summarize | 300,000 | 800 | gpt-5-nano | ~$12 |
| Cluster Summary | 3,000 | 2,000 | gpt-5 | ~$15 |
| Cluster Topic | 3,000 | 200 | gpt-5-nano | ~$0.25 |
| Narrative | 30 | 10,000 | gpt-5.2 | ~$3 |
| **Total** | | | | **~$50/month** |

Using gpt-5-nano instead of gpt-4o-mini saves ~$25/month on summarization.
Using text-embedding-3-large provides highest quality embeddings for better deduplication and clustering accuracy.

---

## Summary

This strategy provides:
- **Quality-first embeddings** using OpenAI's best model (text-embedding-3-large)
- **Multilingual fallback** using Cohere's embed-multilingual-v3.0 for excellent non-English support
- **Multi-provider redundancy** to avoid single-vendor outages
- **Runtime flexibility** via bot commands
- **Graceful degradation** with automatic fallbacks

Immediate action: Implement Cohere provider as fallback for embeddings to handle OpenAI quota exhaustion gracefully.
