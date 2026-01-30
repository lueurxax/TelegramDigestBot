# Telegram Link Seeding

Link seeding extracts external URLs from Telegram messages and adds them to the Solr crawler queue. This improves evidence coverage by capturing primary sources referenced by tracked channels.

## Overview

When processing Telegram messages, the system:

1. Extracts URLs from message text and entities
2. Filters out Telegram-internal domains and denied extensions
3. Normalizes URLs for consistent deduplication
4. Enqueues valid URLs to the Solr crawler queue

The crawler later fetches these URLs, enabling the enrichment system to find evidence that supports or contradicts digest items.

---

## How It Works

### Extraction

URLs are extracted from:

- Message entity URLs (links in message text)
- Telegram webpage preview URLs
- Raw URLs detected in text

### Filtering

URLs are filtered before enqueueing:

| Filter | Behavior |
|--------|----------|
| Non-HTTP(S) schemes | Dropped |
| Telegram domains | Dropped (t.me, telegram.me, telesco.pe, telegram.org) |
| Denied extensions | Dropped (.zip, .exe, .pdf, etc.) |
| Denied domains | Dropped if in denylist |
| Not in allowlist | Dropped if allowlist is configured and domain not in it |

### Normalization

URLs are normalized for consistent hashing:

- Lowercase hostname
- Remove default ports (80, 443)
- Trim fragments
- Remove tracking parameters (utm_*, fbclid, gclid, yclid)
- Normalize trailing slash
- Sort query parameters

### Enqueueing

Each valid URL is added to Solr with:

| Field | Value |
|-------|-------|
| `id` | SHA256 hash of canonical URL |
| `source` | `web` |
| `url` | Original URL |
| `url_canonical` | Normalized URL |
| `domain` | Extracted hostname |
| `crawl_status` | `pending` |
| `crawl_depth` | `0` |
| `crawl_seed_source` | `telegram` |
| `crawl_seed_ref` | `tg://peer/<peer_id>/msg/<msg_id>` |

Deduplication is by document ID (hash of canonical URL). If the URL already exists, it's skipped.

---

## Configuration

### Enable Link Seeding

Link seeding is automatically enabled when `SOLR_URL` is configured:

```env
SOLR_URL=http://solr:8983/solr/news
```

### Limits

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `MAX_LINKS_PER_MESSAGE` | int | `3` | Max URLs to process per message |
| `CRAWLER_QUEUE_MAX_PENDING` | int | `0` | Skip seeding if queue exceeds this (0 = no limit) |

### Domain Filtering

| Variable | Type | Description |
|----------|------|-------------|
| `DOMAIN_ALLOWLIST` | string | Comma-separated domains to allow (if set, others are blocked) |
| `DOMAIN_DENYLIST` | string | Comma-separated domains to block |

Domain matching supports suffix matching: `example.com` matches `sub.example.com`.

### Extension Filtering

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `LINK_SEED_EXT_DENYLIST` | string | (empty) | Comma-separated extensions to skip (e.g., `.zip,.exe,.pdf`) |

---

## Backpressure

When the queue is full:

1. Check pending count before processing each message
2. If count exceeds `CRAWLER_QUEUE_MAX_PENDING`, skip seeding
3. Log `queue_full` skip reason
4. Continue message processing (seeding is opportunistic)

---

## Observability

### Prometheus Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `link_seed_extracted_total` | Counter | - | URLs extracted from messages |
| `link_seed_enqueued_total` | Counter | - | URLs successfully enqueued |
| `link_seed_skipped_total` | Counter | `reason` | URLs skipped by reason |
| `link_seed_errors_total` | Counter | - | Enqueue errors |
| `crawler_queue_pending` | Gauge | - | Current queue size |

### Skip Reasons

| Reason | Description |
|--------|-------------|
| `disabled` | Solr client not configured |
| `invalid_scheme` | Non-HTTP(S) URL |
| `telegram_domain` | URL points to Telegram |
| `denied_extension` | File extension in denylist |
| `denied_domain` | Domain in denylist |
| `not_in_allowlist` | Allowlist configured but domain not in it |
| `queue_full` | Queue pending count exceeded |
| `duplicate` | URL already in queue |
| `max_links_exceeded` | Exceeded per-message limit |

---

## Integration with Enrichment

Seeded URLs are processed by the crawler and become available for:

1. **Evidence matching** - Finding sources that corroborate digest items
2. **Link enrichment** - Extracting content for summary generation
3. **Fact-checking** - Cross-referencing claims with external sources

See [Source Enrichment](source-enrichment.md) for how evidence is matched to items.

---

## Error Handling

Link seeding is non-blocking and opportunistic:

- Errors are logged but don't fail message processing
- No inline retries for Solr errors
- Transient failures are acceptable (seeding improves coverage but isn't required)

---

## Implementation Files

| File | Purpose |
|------|---------|
| `internal/process/linkseeder/linkseeder.go` | Main seeder implementation |
| `internal/core/solr/client.go` | Solr client for queue operations |
| `internal/platform/observability/metrics.go` | Prometheus metrics |
| `internal/process/pipeline/enrichment.go` | Pipeline integration |

---

## See Also

- [Source Enrichment](source-enrichment.md) - How seeded URLs become evidence
- [Link Enrichment](link-enrichment.md) - URL content extraction for summaries
