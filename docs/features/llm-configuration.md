# LLM Configuration

The digest pipeline uses multiple LLM providers to process messages through various AI-driven stages. This document describes the multi-provider architecture, model selection strategy, and runtime configuration options.

## Overview

The LLM system handles these core tasks:
- **Summarization** - Generate factual summaries with scores and metadata
- **Cluster summarization** - Merge related summaries into coherent statements
- **Topic labeling** - Generate short topic labels for clusters
- **Narrative generation** - Create user-facing digest text
- **Translation** - Translate content to target language
- **Claim extraction** - Extract factual claims from external sources
- **Relevance gate** - Filter spam and irrelevant content
- **Summary compression** - Compress summaries for image generation prompts

## Supported Providers

| Provider | API Key Variable | Primary Use Cases |
|----------|-----------------|-------------------|
| **Google** | `GOOGLE_API_KEY` | Summarization, narrative generation (cost-effective) |
| **OpenAI** | `OPENAI_API_KEY` | Fallback LLM, embeddings, image generation |
| **Anthropic** | `ANTHROPIC_API_KEY` | Narrative generation (high quality) |
| **Cohere** | `COHERE_API_KEY` | Cluster summarization, embedding fallback |
| **OpenRouter** | `OPENROUTER_API_KEY` | Translation, claim extraction, relevance gate |

All providers are optional. Configure only the providers you need by setting their API keys.

## Task Configuration

Each task has a default provider chain with automatic fallback. If the primary provider fails or is unavailable, the system automatically tries the next provider in the chain.

### Default Task Chains

| Task | Default | Fallback 1 | Fallback 2 |
|------|---------|------------|------------|
| **Summarize** | Google (gemini-2.0-flash-lite) | OpenAI (gpt-5-nano) | OpenRouter (gpt-oss-120b) |
| **Cluster Summary** | Cohere (command-r) | OpenAI (gpt-5) | Google (gemini-2.0-flash-lite) |
| **Cluster Topic** | OpenAI (gpt-5-nano) | Google (gemini-2.0-flash-lite) | OpenRouter (mistral-7b-instruct) |
| **Narrative** | Google (gemini-2.0-flash-lite) | Anthropic (claude-haiku-4.5) | OpenAI (gpt-5.2) |
| **Translate** | OpenRouter (llama-3.1-8b-instruct) | OpenAI (gpt-5-nano) | - |
| **Complete (Claims)** | OpenRouter (llama-3.1-8b-instruct) | OpenAI (gpt-4o-mini) | - |
| **Relevance Gate** | OpenRouter (llama-3.1-8b-instruct) | OpenAI (gpt-5-nano) | - |
| **Compress** | OpenAI (gpt-4o-mini) | OpenRouter (gpt-oss-120b) | - |
| **Image Gen** | OpenAI (gpt-image-1.5) | - | - |

### Fallback Behavior

When the primary model is unavailable:
1. The system tries each fallback in order
2. Circuit breakers prevent repeated calls to failing providers
3. Prometheus metrics track fallback usage
4. The task completes with a fallback model rather than failing

## Runtime Model Overrides

Operators can override the default model for any task without redeployment.

### Via Bot Commands

**Set a model override:**
```
/llm set <task> <model>
```

Available tasks: `summarize`, `cluster`, `narrative`, `topic`

**Examples:**
```
/llm set narrative claude-haiku-4.5
/llm set summarize gpt-4o
/llm set cluster gpt-5
```

**Reset overrides:**
```
/llm reset <task>      # Reset single task
/llm reset all         # Reset all overrides
```

**View current status:**
```
/llm status
```

Example output:
```
LLM Provider Status

google (primary)
anthropic
openai
cohere
openrouter

Model Overrides:
- narrative: claude-haiku-4.5

Legend:
  healthy |  circuit open |  unavailable
```

### Via Environment Variables

Per-task model overrides can also be set via environment variables:

```bash
LLM_SUMMARIZE_MODEL=gpt-5-nano
LLM_CLUSTER_MODEL=gpt-5
LLM_NARRATIVE_MODEL=gpt-5.2
LLM_TRANSLATE_MODEL=meta-llama/llama-3.1-8b-instruct
LLM_COMPLETE_MODEL=meta-llama/llama-3.1-8b-instruct
LLM_RELEVANCE_GATE_MODEL=meta-llama/llama-3.1-8b-instruct
LLM_COMPRESS_MODEL=gpt-4o-mini
```

### Resolution Order

Model selection follows this priority:
1. Bot override (via `/llm set`)
2. Environment variable override
3. Default model for task
4. Fallback chain (if default fails)

## Cost Tracking

The system tracks token usage and estimates costs for all LLM requests.

### Viewing Costs

Use the `/llm costs` command to see usage statistics:

```
/llm costs
```

Example output:
```
LLM Cost Tracking

Today's Usage:
- Requests: 1,234
- Tokens: 456k (prompt: 234k, completion: 222k)
- Est. Cost: $2.3456

This Month:
- Requests: 28,500
- Tokens: 12.3M (prompt: 6.1M, completion: 6.2M)
- Est. Cost: $45.67

By Provider:
  google: 8.2M tokens, 20,100 reqs, $8.20
  openai: 3.5M tokens, 7,200 reqs, $35.00
  openrouter: 600k tokens, 1,200 reqs, $2.47

Real-time Metrics:
View detailed metrics in Grafana dashboard.
Prometheus metrics: digest_llm_*
```

### Cost Estimation

Costs are estimated based on published pricing for each provider and model. The system tracks:
- Prompt tokens (input)
- Completion tokens (output)
- Estimated USD cost per request

Usage data is stored in the database and aggregated by provider, model, and task.

## Budget Controls

Set a daily token budget to receive alerts when approaching limits.

### Viewing Budget Status

```
/llm budget
```

Example output:
```
LLM Budget Status

Today's Usage: 450K tokens
Daily Limit: 500K tokens
Usage: 90.0%

Warning: Approaching budget limit

Commands:
- /llm budget set <tokens> - Set daily limit
- /llm budget off - Disable budget alerts
```

### Setting Budget Limits

```
/llm budget set 500000    # Set daily limit to 500K tokens
/llm budget off           # Disable budget alerts
```

### Alert Thresholds

Budget alerts trigger at:
- **80%** - Warning alert
- **100%** - Critical alert

Alerts are sent to the admin chat. The pipeline continues to function even when the budget is exceeded - alerts are informational only.

## Circuit Breaker

Each provider has a circuit breaker that prevents repeated calls to failing services.

### Behavior

1. **Closed** - Normal operation, requests flow to provider
2. **Open** - Provider has failed repeatedly, requests skip to fallback
3. **Half-Open** - After timeout, one request is allowed to test recovery

### Configuration

```bash
LLM_CIRCUIT_THRESHOLD=5      # Failures before opening circuit
LLM_CIRCUIT_TIMEOUT=60s      # Time before attempting recovery
```

### Monitoring

Circuit breaker state is exposed via Prometheus metrics:
- `digest_llm_circuit_breaker_state` - Current state (0=closed, 1=open)
- `digest_llm_circuit_breaker_opens_total` - Total times circuit opened

## Embeddings

The system uses embeddings for semantic search, deduplication, and clustering.

### Provider Priority

Configure the provider order via environment variable:

```bash
EMBEDDING_PROVIDER_ORDER=openai,cohere,google
```

Providers are tried in order. The first available provider is used; if it fails, the next is attempted.

### Supported Embedding Providers

| Provider | Model | Dimensions | Notes |
|----------|-------|------------|-------|
| **OpenAI** | text-embedding-3-large | 3072 | Highest quality |
| **Cohere** | embed-multilingual-v3.0 | 1024 | Excellent multilingual support |
| **Google** | gemini-embedding-001 | 3072 | Free tier available |

### Embedding Configuration

```bash
# Provider order
EMBEDDING_PROVIDER_ORDER=openai,cohere,google

# OpenAI settings
OPENAI_EMBEDDING_MODEL=text-embedding-3-large
OPENAI_EMBEDDING_DIMENSIONS=1536

# Cohere settings
COHERE_EMBEDDING_MODEL=embed-multilingual-v3.0

# Circuit breaker
EMBEDDING_CIRCUIT_THRESHOLD=5
EMBEDDING_CIRCUIT_TIMEOUT=1m
```

### Dimension Handling

Different providers produce embeddings of different dimensions. The system normalizes all embeddings to a target dimension by padding with zeros or truncating as needed.

## Prometheus Metrics

The LLM system exports comprehensive metrics for monitoring:

### Request Metrics

| Metric | Labels | Description |
|--------|--------|-------------|
| `digest_llm_requests_total` | provider, model, task, status | Total LLM requests |
| `digest_llm_tokens_prompt_total` | provider, model, task | Total prompt tokens used |
| `digest_llm_tokens_completion_total` | provider, model, task | Total completion tokens used |
| `digest_llm_request_latency_seconds` | provider, model, task | Request latency histogram |

### Provider Health

| Metric | Labels | Description |
|--------|--------|-------------|
| `digest_llm_provider_available` | provider | Provider availability (0/1) |
| `digest_llm_circuit_breaker_state` | provider | Circuit breaker state |
| `digest_llm_circuit_breaker_opens_total` | provider | Times circuit opened |

### Fallback Tracking

| Metric | Labels | Description |
|--------|--------|-------------|
| `digest_llm_fallback_total` | from_provider, to_provider, task | Fallback events |

## Environment Variables Reference

### API Keys

```bash
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...
GOOGLE_API_KEY=...
COHERE_API_KEY=...
OPENROUTER_API_KEY=sk-or-...
```

### Model Configuration

```bash
# Per-task model overrides
LLM_SUMMARIZE_MODEL=gemini-2.0-flash-lite
LLM_CLUSTER_MODEL=command-r
LLM_NARRATIVE_MODEL=gemini-2.0-flash-lite
LLM_TRANSLATE_MODEL=meta-llama/llama-3.1-8b-instruct
LLM_COMPLETE_MODEL=meta-llama/llama-3.1-8b-instruct
LLM_RELEVANCE_GATE_MODEL=meta-llama/llama-3.1-8b-instruct
LLM_COMPRESS_MODEL=gpt-4o-mini
```

### Fallback and Resilience

```bash
LLM_FALLBACK_ENABLED=true
LLM_CIRCUIT_THRESHOLD=5
LLM_CIRCUIT_TIMEOUT=60s
```

### Embeddings

```bash
EMBEDDING_PROVIDER_ORDER=openai,cohere,google
OPENAI_EMBEDDING_MODEL=text-embedding-3-large
OPENAI_EMBEDDING_DIMENSIONS=1536
COHERE_EMBEDDING_MODEL=embed-multilingual-v3.0
EMBEDDING_CIRCUIT_THRESHOLD=5
EMBEDDING_CIRCUIT_TIMEOUT=1m
```

## Cost Estimation

Estimated monthly costs for a typical deployment (10,000 messages/day, 30 digests/month):

| Task | Calls/Month | Tokens/Call | Model | Est. Cost |
|------|-------------|-------------|-------|-----------|
| Embeddings | 300,000 | 500 | text-embedding-3-large | ~$19.50 |
| Summarize | 300,000 | 800 | gemini-2.0-flash-lite | ~$12 |
| Cluster Summary | 3,000 | 2,000 | command-r | ~$9 |
| Cluster Topic | 3,000 | 200 | gpt-5-nano | ~$0.25 |
| Narrative | 30 | 10,000 | gemini-2.0-flash-lite | ~$0.12 |
| Translate | 150,000 | 300 | llama-3.1-8b-instruct | ~$2.25 |
| Complete | 100,000 | 500 | llama-3.1-8b-instruct | ~$2.50 |
| Relevance Gate | 300,000 | 200 | llama-3.1-8b-instruct | ~$1.50 |
| **Total** | | | | **~$47/month** |

Actual costs vary based on:
- Message volume and length
- Cluster count
- Model pricing changes
- Fallback usage

## Troubleshooting

### Provider Not Available

If a provider shows as unavailable in `/llm status`:
1. Verify the API key is set correctly
2. Check for rate limiting or quota exhaustion
3. Review provider status pages for outages

### High Fallback Usage

If fallbacks are triggering frequently:
1. Check circuit breaker metrics
2. Review provider error logs
3. Consider adjusting circuit breaker thresholds
4. Verify network connectivity to provider APIs

### Cost Spikes

If costs are higher than expected:
1. Review `/llm costs` for per-provider breakdown
2. Check for unusual message volume
3. Verify model configuration (cheaper models for high-volume tasks)
4. Set budget alerts to catch issues early

## See Also

- [Content Quality](content-quality.md) - Relevance gates and quality scoring
- [Source Enrichment](source-enrichment.md) - Evidence retrieval using LLM claim extraction
- [Architecture](../architecture.md) - System overview and component interactions
