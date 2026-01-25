# Telegram Link Seeding for Search Index

> **Status: PROPOSAL** (January 2026)
>
> Add outbound links from Telegram posts to the crawler queue so the search index captures primary sources referenced in channels.

## Summary
Extract external links from Telegram messages and enqueue them into the Solr crawl queue. This improves coverage of primary sources and increases evidence quality without expanding the seed list manually.

## Goals
- Capture external sources referenced by Telegram posts.
- Improve search coverage for fast-moving topics.
- Keep crawler scope controlled (no Telegram internal links, rate-limited).

## Non-Goals
- Crawling Telegram itself.
- Unlimited link ingestion from high-volume channels.

## Design

### Extraction
- Use existing link extraction from Telegram entities and link previews.
- Normalize URLs and drop `t.me`/Telegram internal links.
- Cap to `MAX_LINKS_PER_MESSAGE`.

### Queue Enqueue
- Enqueue URLs with:
  - `crawl_status=pending`
  - `crawl_depth=0`
  - `crawl_seed_source=telegram`
  - `crawl_seed_ref=tg://peer/<peer_id>/msg/<msg_id>`
- Skip if URL already exists in Solr (pending/done/error).

### Schema Additions
```xml
<field name="crawl_seed_source" type="string" indexed="true" stored="true"/>
<field name="crawl_seed_ref" type="string" indexed="true" stored="true"/>
```

## Configuration
- `TELEGRAM_LINK_SEEDING_ENABLED` (default: false)
- `MAX_LINKS_PER_MESSAGE` (existing)

## Success Criteria
- 50%+ of Telegram external links appear in the crawl queue within 1 hour.
- Improved evidence match rate for Telegram-origin clusters.

## Open Questions
- Should link seeding be restricted to certain channels only?
- Do we need a per-channel rate cap?
