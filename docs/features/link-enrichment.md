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

## Notes for Annotation

The annotation UI shows the raw message text plus the generated summary. It does not display the fetched link content, even when link enrichment is enabled.
