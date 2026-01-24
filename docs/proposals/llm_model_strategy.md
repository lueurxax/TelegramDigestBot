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
| **OpenAI** | gpt-4o, gpt-4o-mini, text-embedding-3-small/large | Primary LLM and embeddings |
| **Anthropic** | claude-3-5-sonnet, claude-3-5-haiku | Narrative, complex reasoning |
| **Google** | gemini-2.0-flash, gemini-1.5-pro, text-embedding-004 | Fallback LLM and embeddings |
| **Ollama** | llama3.2, mistral, nomic-embed-text | Local/free fallback |
| **Cohere** | command-r, command-r-plus, embed-v3 | Alternative embeddings |

### Provider Priority (Default)

```
OpenAI → Google → Ollama (local)
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

| Task | Default Model | Fallback 1 | Fallback 2 | Dimensions |
|------|---------------|------------|------------|------------|
| Message dedup | `text-embedding-3-small` | `text-embedding-004` | `nomic-embed-text` | 1536 / 768 / 768 |
| Clustering | `text-embedding-3-small` | `text-embedding-004` | `nomic-embed-text` | 1536 / 768 / 768 |
| Semantic search | `text-embedding-3-small` | `text-embedding-004` | `nomic-embed-text` | 1536 / 768 / 768 |

### Cost Comparison (per 1M tokens)

| Model | Provider | Price | Notes |
|-------|----------|-------|-------|
| text-embedding-3-small | OpenAI | $0.02 | Best quality/price |
| text-embedding-3-large | OpenAI | $0.13 | Higher quality |
| text-embedding-004 | Google | $0.00 (free tier) | 1500 req/min free |
| embed-v3 | Cohere | $0.10 | Good multilingual |
| nomic-embed-text | Ollama | $0.00 | Local, unlimited |

### Recommended Configuration

For cost optimization with redundancy:

```yaml
EMBEDDING_PROVIDER_ORDER: "google,openai,ollama"
EMBEDDING_DEFAULT_MODEL: "text-embedding-004"
EMBEDDING_FALLBACK_MODEL: "text-embedding-3-small"
EMBEDDING_LOCAL_MODEL: "nomic-embed-text"
```

This uses Google's free tier first, falls back to OpenAI, then local Ollama.

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
| Default | `gpt-4o-mini` | OpenAI | $0.15 in / $0.60 out |
| Fallback 1 | `gemini-2.0-flash` | Google | $0.075 in / $0.30 out |
| Fallback 2 | `llama3.2:8b` | Ollama | Free (local) |

---

### 2. Cluster Summary

**Purpose:** Merge related summaries into coherent statement.

| Attribute | Value |
|-----------|-------|
| Volume | Low (per cluster) |
| Complexity | High (multi-document reasoning) |
| Output | Text |

**Model mapping:**

| Priority | Model | Provider | Cost (1M tokens) |
|----------|-------|----------|------------------|
| Default | `gpt-4o` | OpenAI | $2.50 in / $10 out |
| Fallback 1 | `claude-3-5-sonnet-20241022` | Anthropic | $3.00 in / $15 out |
| Fallback 2 | `gemini-1.5-pro` | Google | $1.25 in / $5.00 out |

---

### 3. Cluster Topic

**Purpose:** Generate 2-4 word topic label.

| Attribute | Value |
|-----------|-------|
| Volume | Low |
| Complexity | Low (classification) |
| Output | Short text |

**Model mapping:**

| Priority | Model | Provider | Cost (1M tokens) |
|----------|-------|----------|------------------|
| Default | `gpt-4o-mini` | OpenAI | $0.15 in / $0.60 out |
| Fallback 1 | `gemini-2.0-flash` | Google | $0.075 in / $0.30 out |
| Fallback 2 | `mistral:7b` | Ollama | Free (local) |

---

### 4. Narrative

**Purpose:** Generate final user-facing digest text.

| Attribute | Value |
|-----------|-------|
| Volume | Very low (1 per digest) |
| Complexity | High (editorial, style matters) |
| Output | Long-form text |

**Model mapping:**

| Priority | Model | Provider | Cost (1M tokens) |
|----------|-------|----------|------------------|
| Default | `gpt-4o` | OpenAI | $2.50 in / $10 out |
| Fallback 1 | `claude-3-5-sonnet-20241022` | Anthropic | $3.00 in / $15 out |
| Fallback 2 | `gemini-1.5-pro` | Google | $1.25 in / $5.00 out |

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
/llm set narrative claude-3-5-sonnet-20241022
/llm set embeddings text-embedding-004
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
  embeddings: text-embedding-004 (google) [override]
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
  ('llm_override_embeddings', 'text-embedding-004'),
  ('llm_override_summarize', NULL),  -- NULL = use default
  ('llm_override_narrative', 'claude-3-5-sonnet-20241022');
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
GOOGLE_AI_API_KEY=...
OLLAMA_URL=http://localhost:11434

# Embedding Configuration
EMBEDDING_PROVIDER_ORDER=google,openai,ollama
EMBEDDING_DEFAULT_MODEL=text-embedding-004

# LLM Configuration
LLM_PROVIDER_ORDER=openai,anthropic,google,ollama
LLM_SUMMARIZE_MODEL=gpt-4o-mini
LLM_CLUSTER_MODEL=gpt-4o
LLM_NARRATIVE_MODEL=gpt-4o

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
| Embeddings | 300,000 | 500 | text-embedding-004 | $0 (free) |
| Summarize | 300,000 | 800 | gpt-4o-mini | ~$36 |
| Cluster Summary | 3,000 | 2,000 | gpt-4o | ~$15 |
| Cluster Topic | 3,000 | 200 | gpt-4o-mini | ~$0.50 |
| Narrative | 30 | 10,000 | gpt-4o | ~$3 |
| **Total** | | | | **~$55/month** |

Using Google embeddings saves ~$3/month vs OpenAI embeddings.

---

## Summary

This strategy provides:
- **Multi-provider redundancy** to avoid single-vendor outages
- **Cost optimization** using free tiers and cheaper models for high-volume tasks
- **Runtime flexibility** via bot commands
- **Graceful degradation** with automatic fallbacks

Immediate action: Switch embeddings to Google's free `text-embedding-004` to resolve the current OpenAI quota issue.
