# Corroboration & Fact-Check Links

The corroboration system adds context to digest items by showing which other channels reported the same story and linking to external fact-checks when available.

## Overview

Two features work together to improve content credibility:

1. **Channel corroboration** - Shows "Also reported by: @channel1, @channel2" when multiple tracked channels cover the same story
2. **Fact-check links** - Queries Google Fact Check API for related claims and displays links to human-verified fact-checks

Both features are non-blocking: if no corroboration or fact-check is found, the item renders normally.

---

## Channel Corroboration

When a digest item belongs to a cluster with multiple sources, the system shows which other channels reported similar content.

### Output Examples

Multiple channels:
```
↳ Also reported by: @channel1, @channel2, @channel3
```

Same channel, different message:
```
↳ Related: [link]
```

### Behavior

- Uses existing clustering to identify related items
- Shows up to 3 other channels (configurable via `maxCorroborationChannels`)
- Deduplicates by channel (shows each channel only once)
- If only the same channel appears (duplicate posts), shows "Related" link instead
- Omits the line entirely if no other sources exist

### Channel Identification

Channels are matched by (in priority order):
1. Username (`@channel`)
2. Peer ID (Telegram's numeric ID)
3. Title (fallback for private channels)

---

## Google Fact Check API

The system queries Google's Fact Check Tools API to find human-verified fact-checks related to each item's content.

### How It Works

1. **Claim extraction** - Extracts a short claim (first sentence of summary, 40-300 chars)
2. **API query** - Searches Google Fact Check API with rate limiting
3. **Result storage** - Stores matches in `item_fact_checks` table
4. **Rendering** - Displays link in digest if match found

### Output Example

```
↳ Fact-check: politifact.com - "Mostly False"
```

### Configuration

| Setting | Default | Description |
|---------|---------|-------------|
| `FACTCHECK_GOOGLE_ENABLED` | `false` | Enable fact-check lookups |
| `FACTCHECK_GOOGLE_API_KEY` | - | Google API key (required) |
| `FACTCHECK_GOOGLE_MAX_RESULTS` | `3` | Max results per query |
| `FACTCHECK_CACHE_TTL_HOURS` | `48` | Cache TTL for results |
| `FACTCHECK_GOOGLE_RPM` | `60` | Rate limit (requests/minute) |
| `FACTCHECK_MIN_CLAIM_LENGTH` | `40` | Min claim length to query |

### Getting an API Key

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create or select a project
3. Enable the "Fact Check Tools API"
4. Create credentials (API key)
5. Set `FACTCHECK_GOOGLE_API_KEY` in your environment

---

## Processing Flow

```
Item created
    ↓
Claim extracted from summary
    ↓
Enqueue to fact_check_queue
    ↓
Fact Check Worker picks up item
    ↓
Check cache (by normalized claim)
    ├─ Cache hit → Use cached results
    └─ Cache miss → Query Google API
                        ↓
                   Store in cache
                        ↓
                   Save matches to item_fact_checks
    ↓
Digest render
    ↓
Build corroboration line from cluster
    ↓
Fetch fact-check matches for items
    ↓
Render with "Also reported by" + fact-check link
```

---

## Database Schema

### fact_check_queue

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID | Primary key |
| `item_id` | UUID | FK to items.id |
| `claim` | TEXT | Original claim text |
| `normalized_claim` | TEXT | Lowercased, whitespace-normalized |
| `status` | TEXT | pending, processing, done, error |
| `attempt_count` | INT | Retry count |
| `error_message` | TEXT | Last error (if any) |
| `next_retry_at` | TIMESTAMPTZ | When to retry |

### fact_check_cache

| Column | Type | Description |
|--------|------|-------------|
| `normalized_claim` | TEXT | Primary key |
| `result_json` | JSONB | Cached API response |
| `cached_at` | TIMESTAMPTZ | Cache timestamp |

### item_fact_checks

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID | Primary key |
| `item_id` | UUID | FK to items.id |
| `claim` | TEXT | Matched claim text |
| `url` | TEXT | Fact-check article URL |
| `publisher` | TEXT | Publisher name |
| `rating` | TEXT | Verdict (e.g., "False", "Mostly True") |
| `matched_at` | TIMESTAMPTZ | When matched |

---

## Observability

### Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `digest_factcheck_request_duration_seconds` | Histogram | API request latency |
| `digest_factcheck_requests_total` | Counter | Requests by status (success/error) |
| `digest_factcheck_matches_total` | Counter | Total fact-check matches found |
| `digest_factcheck_cache_hits_total` | Counter | Cache hits |
| `digest_factcheck_cache_misses_total` | Counter | Cache misses |

---

## Worker Behavior

The fact-check worker runs as part of the pipeline and:

- Polls for pending items in `fact_check_queue`
- Respects rate limits via token bucket
- Retries failed requests up to 3 times with 10-minute delay
- Cleans expired cache entries every 6 hours
- Gracefully degrades if API is unavailable

### Error Handling

| Scenario | Behavior |
|----------|----------|
| API error | Retry up to 3 times, then mark as error |
| Rate limited | Wait via token bucket |
| No results | Mark as done, no matches stored |
| Cache hit | Use cached results, skip API call |

---

## Implementation Files

| File | Purpose |
|------|---------|
| `internal/output/digest/corroboration.go` | Channel corroboration logic |
| `internal/process/factcheck/worker.go` | Fact-check queue worker |
| `internal/process/factcheck/google_client.go` | Google API client |
| `internal/process/factcheck/claim.go` | Claim extraction |
| `internal/storage/factcheck.go` | Database operations |
| `migrations/20260119000000_add_fact_check_phase1.sql` | Schema |

---

## Future: Phase 2

Phase 1 provides basic corroboration signals. Phase 2 (not yet implemented) would add:

- Full evidence retrieval from multiple providers (GDELT, NewsAPI, etc.)
- Agreement scoring between sources
- Confidence tiers (High/Medium/Low)
- Evidence bullets in summaries

See [proposals/source-enrichment-fact-checking.md](../proposals/source-enrichment-fact-checking.md) for Phase 2 details.
