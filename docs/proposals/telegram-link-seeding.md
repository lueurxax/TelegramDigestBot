# Telegram Link Seeding for Search Index

> **Status: PROPOSAL** (January 2026)
>
> Add outbound links from Telegram posts to the crawler queue so the search index captures primary sources referenced by channels.

## Summary
Seed external links from Telegram messages into the crawler queue. This improves evidence coverage for fast-moving topics without expanding the static seed list or increasing LLM usage.

## Goals
- Capture primary sources linked from Telegram posts.
- Improve evidence availability for clustering and fact-checking.
- Keep crawler scope controlled and predictable.

## Non-Goals
- Crawling Telegram itself.
- Unlimited link ingestion from high-volume channels.
- Backfilling historical messages.

## Design

### Pipeline Placement
- Seed links during pipeline processing after link extraction and before LLM steps.
- Use existing message entities and Telegram webpage preview fields.
- Never block message processing if seeding fails.

### Link Sources
- Entity URLs in message text.
- URLs detected in raw text (if not already in entities).
- Telegram preview `Webpage.URL` (when present).

### Normalization
- Lowercase host, trim fragments, drop default ports.
- Remove common tracking params (`utm_*`, `fbclid`, `gclid`, `yclid`).
- Normalize trailing slash and sort query params for stable hashing.
- Produce:
  - `url_canonical` (normalized URL)
  - Doc ID is derived from hash of `url_canonical` via `solr.WebDocID()`, used for idempotent dedupe

### Filtering & Scope Control
- Drop non-http(s) schemes.
- Drop Telegram-internal domains (`t.me`, `telegram.me`, `telesco.pe`).
- Optional allow/deny list for domains.
- Optional extension denylist (e.g., `.zip`, `.exe`, `.pdf`) to avoid non-content payloads.

### Queue Enqueue
Each seeded URL is inserted into the crawler queue with:
- `crawl_status=pending`
- `crawl_depth=0`
- `crawl_seed_source=telegram`
- `crawl_seed_ref=tg://peer/<peer_id>/msg/<msg_id>`
- `url_canonical`
- `domain` (normalized hostname for analytics/filtering)

Idempotency:
- Doc ID is hash of `url_canonical`; if doc already exists (any status), skip enqueue.
- If queue is unavailable, log and continue (no inline retries).

### Backpressure
- Enforce `MAX_LINKS_PER_MESSAGE`.
- If `CRAWLER_QUEUE_MAX_PENDING` is exceeded, skip seeding and log `queue_full`.
- Optional per-channel hourly caps can be added later if needed.

## Schema Additions (Solr Queue)
```xml
<field name="crawl_seed_source" type="string" indexed="true" stored="true"/>
<field name="crawl_seed_ref" type="string" indexed="true" stored="true"/>
<field name="url_canonical" type="string" indexed="true" stored="true"/>
```
Note: Doc ID is hash of `url_canonical` (via `solr.WebDocID()`), so no separate `url_hash` field is needed.

## Configuration
- `TELEGRAM_LINK_SEEDING_ENABLED` (default: false)
- `MAX_LINKS_PER_MESSAGE` (existing)
- `CRAWLER_QUEUE_MAX_PENDING` (optional)
- `DOMAIN_ALLOWLIST`, `DOMAIN_DENYLIST` (optional)
- `LINK_SEED_EXT_DENYLIST` (optional, comma-separated)

## Success Criteria
- 50%+ of external links appear in the queue within 1 hour.
- 10–20% increase in evidence matches for Telegram-origin clusters.
- No material increase in crawler error rate or backlog.

## Observability
- Counters: `link_seed_extracted_total`, `link_seed_enqueued_total`, `link_seed_skipped_total` (reason), `link_seed_errors_total`.
- Gauge: `crawler_queue_pending` at enqueue time.
- Logs include URL, channel, msg_id, and skip reason.

## Testing Strategy
- Unit tests: URL normalization, Telegram-domain filtering, dedupe by doc ID.
- Integration: seed a known link and verify queue insertion in Solr.

## Rollout
- Ship behind `TELEGRAM_LINK_SEEDING_ENABLED=false`.
- Enable for a small subset of channels (if supported), then expand.

## Decisions
| Question | Answer |
| --- | --- |
| Transient Solr errors retry? | No. Log and continue. Seeding is opportunistic, not critical path. |
| Prioritize by channel weight? | No for v1. Keep FIFO. Revisit if evidence gaps appear. |
