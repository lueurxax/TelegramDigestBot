# Source Enrichment

Source enrichment extends the corroboration system by searching external sources to find evidence that supports or relates to digest items. It queries multiple providers (Solr, GDELT, NewsAPI, SearxNG, OpenSearch) and scores how well external sources agree with item content.

> **Status:** Implemented. Only USD-based budget caps remain unimplemented (count-based limits work).
> See [proposals/source-enrichment-fact-checking.md](../proposals/source-enrichment-fact-checking.md) for the original proposal.

## Overview

When enabled, the enrichment worker:
1. Generates search queries from item summaries
2. Queries configured providers for matching sources
3. Extracts content from found URLs
4. Scores agreement between item and evidence
5. Stores evidence links for digest rendering

The digest then shows a "Corroborated" line when evidence is found, and evidence embeddings can boost clustering similarity.

---

## Configuration

### Enable Enrichment

```env
ENRICHMENT_ENABLED=true
```

### Provider Configuration

At least one provider must be configured:

**Solr (self-hosted, recommended):**
Solr is automatically enabled when `SOLR_URL` is configured.
```env
SOLR_URL=http://solr:8983/solr/news
SOLR_TIMEOUT=10s
SOLR_MAX_RESULTS=10
```

**GDELT:**
```env
GDELT_ENABLED=true
GDELT_RPM=30
GDELT_TIMEOUT=30s
```

**NewsAPI:**
```env
ENRICHMENT_NEWSAPI_ENABLED=true
ENRICHMENT_NEWSAPI_KEY=your-api-key
ENRICHMENT_NEWSAPI_RPM=100
ENRICHMENT_NEWSAPI_TIMEOUT=30s
```

**SearxNG (self-hosted metasearch):**
```env
SEARXNG_ENABLED=true
SEARXNG_BASE_URL=http://searxng:8080
SEARXNG_TIMEOUT=30s
SEARXNG_ENGINES=duckduckgo,wikipedia,arxiv
```

**Event Registry:**
```env
ENRICHMENT_EVENTREGISTRY_ENABLED=true
ENRICHMENT_EVENTREGISTRY_API_KEY=your-api-key
ENRICHMENT_EVENTREGISTRY_RPM=30
```

**OpenSearch (requires pre-populated index):**
```env
ENRICHMENT_OPENSEARCH_ENABLED=true
ENRICHMENT_OPENSEARCH_URL=http://opensearch:9200
ENRICHMENT_OPENSEARCH_INDEX=news
ENRICHMENT_OPENSEARCH_RPM=60
```

### Behavior Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `ENRICHMENT_MAX_RESULTS` | `5` | Max sources per item |
| `ENRICHMENT_MAX_SECONDS` | `60` | Per-item timeout |
| `ENRICHMENT_MIN_AGREEMENT` | `0.65` | Min score to keep evidence |
| `ENRICHMENT_CACHE_TTL_HOURS` | `168` | Evidence cache TTL (7 days) |
| `ENRICHMENT_QUEUE_MAX` | `5000` | Max queue size |
| `ENRICHMENT_DEDUP_SIMILARITY` | `0.98` | Embedding similarity for dedup |
| `ENRICHMENT_MAX_EVIDENCE_PER_ITEM` | `5` | Max evidence rows per item |

### Budget Limits

```env
ENRICHMENT_DAILY_LIMIT=1000      # Max requests per day
ENRICHMENT_MONTHLY_LIMIT=20000   # Max requests per month
```

### Domain Filtering

```env
ENRICHMENT_ALLOWLIST_DOMAINS=reuters.com,apnews.com,bbc.com
ENRICHMENT_DENYLIST_DOMAINS=spam-site.com
```

Or use bot commands:
```
/enrichment domains allow reuters.com
/enrichment domains deny spam-site.com
/enrichment domains          # List all filters
```

### Language Routing

Route enrichment queries to different languages based on context. For example, search English sources for general news but Greek sources for Cyprus-local content.

**Policy Configuration** (JSON format):
```env
ENRICHMENT_LANGUAGE_POLICY='{
  "default": ["en"],
  "context": [
    {
      "name": "cyprus",
      "languages": ["el"],
      "keywords": ["cyprus", "nicosia", "limassol", "larnaca", "paphos"]
    }
  ],
  "channel": {
    "@russiancyprusnews": ["el"]
  }
}'
```

Or store in database via settings:
```
/settings set enrichment_language_policy {"default":["en"],"channel":{"@example":["el"]}}
```

**Policy Structure:**

| Key | Description |
|-----|-------------|
| `default` | Languages to use when no other rule matches |
| `context` | Context-based rules with keyword detection |
| `channel` | Per-channel language overrides |

**Evaluation Order:** channel → context → default. First match wins.

**Context Detection:**
1. Channel description (highest confidence)
2. Historical messages (rolling keyword match)
3. Current item text (summary + channel title)

**Query Translation:**
```env
ENRICHMENT_QUERY_TRANSLATE=true    # Enable LLM translation
ENRICHMENT_LLM_TIMEOUT=120s        # Translation timeout
```

When enabled, queries are translated to target languages before searching. The digest output language remains unchanged (typically Russian).

See [proposals/enrichment-language-routing.md](../proposals/enrichment-language-routing.md) for design rationale.

### Evidence-Enhanced Clustering

When items share evidence sources, their clustering similarity gets boosted. This is enabled by default.

```env
EVIDENCE_CLUSTERING_BOOST=0.15      # Max boost amount
EVIDENCE_CLUSTERING_MIN_SCORE=0.5   # Min agreement to apply boost
```

---

## Provider Fallback Order

Providers are queried in this order until results are found:

1. **Solr** - Self-hosted, no cost/rate limits
2. **GDELT** - Free news API
3. **Event Registry** - Commercial, global coverage
4. **NewsAPI** - News aggregation
5. **SearxNG** - Self-hosted metasearch
6. **OpenSearch** - Self-hosted, requires index

Each provider has circuit breaker protection with configurable cooldown (`ENRICHMENT_PROVIDER_COOLDOWN=10m`).

---

## Bot Commands

**Status:**
```
/enrichment              # Show status and queue stats
/enrichment help         # Show all commands
```

**Domain Management:**
```
/enrichment domains                    # List all filters
/enrichment domains allow              # List allowlist
/enrichment domains allow example.com  # Add to allowlist
/enrichment domains allow remove example.com
/enrichment domains deny               # List denylist
/enrichment domains deny spam.com      # Add to denylist
/enrichment domains deny remove spam.com
/enrichment domains clear              # Clear all filters
```

---

## Database Schema

### enrichment_queue

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID | Primary key |
| `item_id` | UUID | FK to items.id |
| `summary` | TEXT | Item summary for query generation |
| `status` | TEXT | pending, processing, done, error |
| `attempt_count` | INT | Retry count |
| `error_message` | TEXT | Last error |
| `next_retry_at` | TIMESTAMPTZ | When to retry |

### evidence_sources

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID | Primary key |
| `url` | TEXT | Source URL |
| `url_hash` | TEXT | SHA256 hash for dedup |
| `domain` | TEXT | Extracted domain |
| `title` | TEXT | Page title |
| `description` | TEXT | Meta description |
| `content` | TEXT | Extracted content |
| `author` | TEXT | Author if found |
| `published_at` | TIMESTAMPTZ | Publication date |
| `language` | TEXT | Detected language |
| `provider` | TEXT | Which provider found it |
| `fetched_at` | TIMESTAMPTZ | When fetched |
| `expires_at` | TIMESTAMPTZ | Cache expiry |

### evidence_claims

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID | Primary key |
| `evidence_id` | UUID | FK to evidence_sources |
| `claim_text` | TEXT | Extracted claim |
| `entities_json` | JSONB | Extracted entities |
| `embedding` | VECTOR | Claim embedding |

### item_evidence

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID | Primary key |
| `item_id` | UUID | FK to items.id |
| `evidence_id` | UUID | FK to evidence_sources |
| `agreement_score` | FLOAT | 0-1 agreement score |
| `is_contradiction` | BOOL | Whether sources disagree |
| `matched_claims_json` | JSONB | Which claims matched |
| `matched_at` | TIMESTAMPTZ | When matched |

### enrichment_usage

| Column | Type | Description |
|--------|------|-------------|
| `date` | DATE | Usage date |
| `provider` | TEXT | Provider name |
| `request_count` | INT | API requests |
| `embedding_count` | INT | Embeddings generated |

---

## Processing Flow

```
Item created (after summarization)
    ↓
Enqueue to enrichment_queue
    ↓
Enrichment Worker picks up item
    ↓
Check budget limits (daily/monthly)
    ├─ Over limit → Skip, requeue later
    └─ Under limit → Continue
    ↓
Generate search queries from summary
    ↓
Query providers (fallback order)
    ↓
For each result URL:
    ├─ Check domain filter (allow/deny)
    ├─ Check cache (by URL hash)
    │   ├─ Cache hit → Use cached
    │   └─ Cache miss → Fetch & extract
    └─ Extract claims from content
    ↓
Score agreement (embedding similarity + entity overlap)
    ↓
Store evidence in item_evidence
    ↓
Update item fact_check_score/tier
    ↓
Digest render
    ↓
Show "Corroborated by: N sources" line
```

---

## Observability

### Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `digest_enrichment_request_duration_seconds` | Histogram | Request latency by provider |
| `digest_enrichment_requests_total` | Counter | Requests by provider, status |
| `digest_enrichment_matches_total` | Counter | Evidence matches found |
| `digest_enrichment_cache_hits_total` | Counter | Cache hits |
| `digest_enrichment_cache_misses_total` | Counter | Cache misses |

---

## Implementation Files

| File | Purpose |
|------|---------|
| `internal/process/enrichment/worker.go` | Main worker loop |
| `internal/process/enrichment/providers.go` | Provider interface |
| `internal/process/enrichment/provider_*.go` | Provider implementations |
| `internal/process/enrichment/extractor.go` | Content extraction |
| `internal/process/enrichment/scoring.go` | Agreement scoring |
| `internal/process/enrichment/query_generator.go` | Query generation |
| `internal/process/enrichment/domain_filter.go` | Domain filtering |
| `internal/storage/enrichment.go` | Database operations |
| `internal/output/digest/clustering.go` | Evidence-boosted clustering |

---

## Known Gap

Only one feature from the proposal remains unimplemented:

1. **USD budgets not enforced** - `ENRICHMENT_DAILY_BUDGET_USD` / `ENRICHMENT_MONTHLY_CAP_USD` are configured but unused; only count-based limits (`ENRICHMENT_DAILY_LIMIT` / `ENRICHMENT_MONTHLY_LIMIT`) work.

All other proposal features are implemented including:
- Query translation via LLM
- Full extraction (JSON-LD, RSS/Atom, readability, TextRank ranking, optional LLM claim extraction)
- Extraction failure tracking (`extraction_failed` field)
- Entity normalization with RU↔EN transliteration and alias expansion (e.g., Kyiv/Kiev)
- "📖 Контекст" (Background) section in digest output
- Parallel source retrieval within time budget
- Corroboration coverage and circuit breaker metrics

See [proposals/source-enrichment-fact-checking.md](../proposals/source-enrichment-fact-checking.md) for the original proposal.

---

## Solr Setup

Solr is the recommended primary provider (self-hosted, no API costs).

See `deploy/k8s/` for Kubernetes deployment with SolrCloud.

---

## See Also

- [Corroboration](corroboration.md) - Channel corroboration and Google Fact Check API
- [Link Enrichment](link-enrichment.md) - URL resolution for message content
- [Content Quality](content-quality.md) - How enrichment integrates with clustering
