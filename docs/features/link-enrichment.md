# Link Enrichment

Link enrichment pulls context from URLs found in Telegram messages and feeds that content into multiple pipeline stages. It helps generate better summaries, improve relevance scoring, topic detection, deduplication, and enrichment queries when messages are short or link-heavy.

## How it works

1. The pipeline extracts up to N URLs from each message.
2. Each URL is resolved and fetched.
3. The resolver extracts readable content using RSS/Atom (if the URL is a feed), JSON-LD/OpenGraph metadata, and readability as a fallback.
4. The extracted content is cached and used in pipeline stages based on `LINK_ENRICHMENT_SCOPE`.

If link enrichment is disabled or a URL fetch fails, processing continues with message text only.

---

## Pipeline Integration (Scope)

Link content can be used in multiple pipeline stages. Configure which stages use link content via `LINK_ENRICHMENT_SCOPE`:

```env
LINK_ENRICHMENT_SCOPE=summary,relevance,topic,dedup,queries,factcheck
```

| Scope | Description |
|-------|-------------|
| `summary` | Include link content in LLM summarization prompt (default) |
| `relevance` | Use link snippets for relevance scoring when message is short |
| `topic` | Include link context for topic detection when message < 120 chars |
| `dedup` | Compute embeddings over `message + link snippet` for better deduplication |
| `queries` | Use link keywords/entities for enrichment query generation |
| `factcheck` | Extract claims from link headlines for fact-check lookups |

Default: `summary` (original behavior). Set multiple scopes as comma-separated values.

### Scope Behavior

**Relevance scoring** (`relevance`):
- Appends a short extracted snippet to the relevance prompt
- Uses strict token budget (400-600 tokens) to avoid overwhelming the prompt
- Falls back to message-only when no links resolve

**Topic assignment** (`topic`):
- Includes link snippets only if raw message text < 120 characters
- Avoids diluting topics with long articles when the message is already descriptive

**Semantic deduplication** (`dedup`):
- If link content exists and message < 200 chars, embeddings include `message + link snippet`
- Helps detect duplicates across channels linking to the same article with different text

**Enrichment queries** (`queries`):
- Query generator pulls keywords/entities from resolved link content
- Used when message is short or LLM summary is vague (< 100 chars)

**Fact-check claims** (`factcheck`):
- Extracts claims from link headlines when message is short
- Uses top 1-2 factual sentences from link content

---

## Link Context Selection

When resolved link content is available, the system intelligently selects whether to treat the link as primary context (the main article) or supplemental context (supporting material).

### Primary vs. Supplemental

**Primary context** is used when the link is likely the main article being discussed:
- Message text is short (< 120 characters), OR
- Link has high word count (>= 200 words), OR
- Domain is on the curated news-source allowlist

**Supplemental context** is used when the message itself is substantial:
- Message text is long and detailed
- Link appears to add supporting information only

### CTA Detection

Links that appear to be donation/subscription pages are deprioritized:
- Detected terms: "donate", "patreon", "subscribe", "support us" (and Russian equivalents)
- CTA detection checks if terms appear in the last 20% of link content
- Domain-based denylist for known donation platforms
- Links with CTA markers are never treated as primary

### Prompt Structure

Based on link role, prompts are structured differently:

**Primary link:**
```
[PRIMARY ARTICLE]
Source: example.com
Title: Article Title
Content: <link content truncated to max chars>

MESSAGE: <telegram text>
Instruction: Summarize the PRIMARY ARTICLE. Use MESSAGE only for context.
```

**Supplemental link:**
```
MESSAGE: <telegram text>
[SUPPLEMENTAL LINK]
<link details>
Instruction: Summarize MESSAGE. Use SUPPLEMENTAL LINK only if it clarifies or adds facts.
```

---

## Canonical Source Detection

The system detects original sources from link metadata to improve deduplication and attribution.

### Canonical URL Extraction

Canonical URLs are extracted from (in priority order):
1. JSON-LD `url` field
2. `<link rel="canonical">` tag
3. `og:url` meta tag

### Trust Rules

Canonical URLs are only accepted if:
- Domain matches the current link domain, OR
- Domain is in the trusted publisher allowlist, OR
- Domain is in the general allowlist

Canonical URLs are rejected if:
- Domain is on the aggregator/syndication denylist
- URL path is empty or points to homepage/root

### Implementation

When a trusted canonical URL is detected:
- `CanonicalURL` and `CanonicalDomain` fields are populated on `ResolvedLink`
- Used for cross-item deduplication (same canonical = same source)
- Stored in item debug fields for observability

---

## Cross-Language Query Routing

Link language detection enables enrichment queries in the original article's language.

### How It Works

1. Content language is detected from extracted text
2. If link language differs from item language:
   - Item-language queries generated from Telegram summary/text
   - Link-language queries generated from link content
3. Both query sets are sent to enrichment providers
4. Results are merged with existing deduplication

### Language Detection

- Uses text analysis on extracted content
- Falls back to Telegram message language if detection fails
- Tagged on queries for provider routing

---

## Admin Controls (Bot Commands)

Enable or disable link enrichment:
- `/config links on`
- `/config links off`

Limit the number of URLs per message:
- `/config maxlinks 3` (range 1-5)

Set cache TTL for fetched links:
- `/config link_cache 24h` (accepts `12h`, `24h`, `7d`)

---

## Environment Configuration

### Basic Settings

Link enrichment is enabled by default.

| Variable | Default | Description |
|----------|---------|-------------|
| `LINK_ENRICHMENT_SCOPE` | `summary` | Pipeline stages that use link content |
| `MAX_LINKS_PER_MESSAGE` | `3` | Max URLs to process per message |
| `LINK_CACHE_TTL` | `24h` | Cache TTL for web links |
| `TG_LINK_CACHE_TTL` | `1h` | Cache TTL for Telegram links |
| `LINK_DENYLIST_DOMAINS` | (empty) | Comma-separated domains to skip |

### Advanced Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `LINK_MIN_WORDS` | `80` | Minimum words in extracted content to use |
| `LINK_SNIPPET_MAX_CHARS` | `1200` | Max chars for link snippet in prompts |
| `LINK_EMBEDDING_MAX_MSG_LEN` | `200` | Message length threshold for using link in embeddings |

### Link Context Selection Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `LINK_PRIMARY_MIN_WORDS` | `200` | Minimum word count for link to be treated as primary |
| `LINK_PRIMARY_SHORT_MSG_CHARS` | `120` | Message length below which links become primary |
| `LINK_PRIMARY_ALLOWLIST` | (empty) | Comma-separated domains always treated as primary |
| `LINK_PRIMARY_CTA_TERMS` | `donate,patreon,...` | CTA terms to detect (comma-separated) |
| `LINK_PRIMARY_MAX_LINKS_CONSIDERED` | `3` | Max links to evaluate for primary selection |
| `LINK_PRIMARY_DONATION_DENYLIST` | (empty) | Domains to reject as CTA/donation pages |

---

## Guardrails

Link content is only used if:
- Extracted content length >= `LINK_MIN_WORDS` (default: 80)
- Domain is not in denylist (`LINK_DENYLIST_DOMAINS`)
- Content extraction succeeded

LLM summary is considered "vague" (triggering link fallback) if < 100 characters or lacks named entities.

---

## Cache Strategy

- **Storage**: Content is stored once per URL in `link_cache` table, preventing redundant fetching
- **Retrieval**: Workers join `items` → `raw_messages` → `message_links` → `link_cache`
- **Multi-message URLs**: Same URL across messages shares one cache entry
- **Expiration**: `LINK_CACHE_TTL` should cover pipeline lifecycle (recommended: 48-72h for high-backlog)
- **Refresh**: Expired entries trigger background re-resolution but processing continues with stale data

---

## Database Schema

### link_cache

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID | Primary key |
| `url` | TEXT | Original URL |
| `url_hash` | TEXT | SHA256 for deduplication |
| `title` | TEXT | Page title |
| `description` | TEXT | Meta description |
| `content` | TEXT | Extracted content |
| `language` | TEXT | Detected language |
| `canonical_url` | TEXT | Detected canonical URL (if different from original) |
| `canonical_domain` | TEXT | Domain of canonical URL |
| `word_count` | INT | Word count of extracted content |
| `fetched_at` | TIMESTAMPTZ | When fetched |
| `expires_at` | TIMESTAMPTZ | Cache expiry |

### message_links

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID | Primary key |
| `raw_message_id` | TEXT | FK to raw_messages |
| `link_cache_id` | UUID | FK to link_cache |
| `position` | INT | Link position in message |

---

## Observability

### Per-Item Debug Fields

Items store debug information about link context usage:

| Field | Description |
|-------|-------------|
| `link_context_used` | Whether resolved link content was used in processing |
| `link_content_len` | Length of link content included |
| `canonical_source_detected` | Whether a trusted canonical URL was found |

### Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `digest_link_context_used_total` | Counter | Items that used resolved link context |
| `digest_link_language_queries_total` | Counter | Enrichment queries from link language content |
| `digest_canonical_source_detected_total` | Counter | Items with trusted canonical source detected |

---

## Implementation Files

| File | Purpose |
|------|---------|
| `internal/core/links/resolver.go` | URL resolution and content fetching |
| `internal/core/links/content_extractor.go` | HTML parsing, readability extraction |
| `internal/core/links/canonical.go` | Canonical URL trust validation |
| `internal/core/llm/link_context.go` | Link context selection and formatting |
| `internal/process/pipeline/enrichment.go` | Pipeline integration for link resolution |

---

## Notes for Annotation

The annotation UI shows the raw message text plus the generated summary. It does not display the fetched link content, even when link enrichment is enabled.

---

## See Also

- [Source Enrichment](source-enrichment.md) - Multi-provider evidence retrieval
- [Corroboration](corroboration.md) - Channel corroboration and fact-check links
- [Link Seeding](link-seeding.md) - Seed external URLs from Telegram to crawler queue
