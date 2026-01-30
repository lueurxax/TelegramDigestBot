# Research Dashboard

The research dashboard provides a web interface and API for exploring the digest archive. It enables ad-hoc investigation of items, clusters, evidence, and channel analytics.

## Overview

The research system provides:

1. **Search** - Query items and evidence with filters
2. **Item inspection** - Detailed view with evidence, cluster context, and inclusion reasoning
3. **Cluster inspection** - Timeline and corroboration views
4. **Channel analytics** - Quality metrics, overlap graphs, and origin/amplifier stats
5. **Topic analytics** - Timelines, drift detection, and volatility
6. **Settings transparency** - Current configuration snapshot

Access is restricted to admin users via bot-authenticated sessions.

---

## Access and Authentication

### Login Flow

1. Admin requests login via `/research login` bot command
2. Bot returns a signed token link: `https://<host>/research/login?token=<...>`
3. User clicks link; web UI validates token and sets session cookie
4. Tokens expire in 10 minutes; sessions expire in 24 hours

### Session Management

- Sessions stored in `research_sessions` table
- Cookie-based (HTTP-only, SameSite=Lax, Secure)
- Only users in `ADMIN_IDS` can access

---

## API Endpoints

All endpoints support both HTML and JSON responses based on `Accept` header or `?format=json` parameter.

### Search

```
GET /research/search
```

| Parameter | Description |
|-----------|-------------|
| `q` | Search query (full-text) |
| `from` | Start date (YYYY-MM-DD or RFC3339) |
| `to` | End date |
| `channel` | Filter by channel (username or ID) |
| `topic` | Filter by topic |
| `lang` | Filter by language |
| `scope` | `items`, `evidence`, or `all` |
| `limit` | Max results (default 50, max 200) |
| `offset` | Pagination offset |
| `include_count` | Return total count (slower) |

### Item Detail

```
GET /research/item/:id
```

Returns:
- Full item details with scores
- Evidence matches
- Cluster context and related items
- Inclusion/exclusion explanation

### Cluster Detail

```
GET /research/cluster/:id
```

Returns:
- Cluster summary and topic
- All items with timestamps and channels
- Timeline view

### Evidence

```
GET /research/evidence/:item_id
```

Returns evidence sources for an item with agreement scores and matched claims.

### Settings

```
GET /research/settings
```

Shows current configuration snapshot including thresholds, language, and schedule.

### Channel Overlap

```
GET /research/channels/overlap
```

Returns Jaccard similarity between channels based on shared clusters.

| Parameter | Description |
|-----------|-------------|
| `from` | Start date |
| `to` | End date |
| `limit` | Max edges (default 50) |

### Channel Quality Summary

```
GET /research/channels/quality
```

Returns latest quality metrics for all channels (defaults to last 30 days).

### Channel Detail

```
GET /research/channels/:id/quality
```

Returns quality history for a specific channel.

```
GET /research/channels/:id/origin-stats
```

Returns origin vs amplifier statistics:
- Origin rate (% of clusters where channel appeared first)
- Top origin topics
- Top amplifier topics

### Channel Bias

```
GET /research/channels/bias?channel=@name
```

Per-channel: Topic over/under-indexing relative to global distribution.

Without channel parameter: Channel agenda similarity matrix.

### Topic Timeline

```
GET /research/topics/timeline
```

| Parameter | Description |
|-----------|-------------|
| `bucket` | `day`, `week`, or `month` (default: week) |
| `from` | Start date (default: 1 year ago) |
| `to` | End date (default: now) |
| `limit` | Max entries |

Returns topic distribution over time with volatility metrics (distinct topics, new topics).

### Topic Drift

```
GET /research/topics/drift
```

Returns clusters where topic labels shifted over time.

### Cross-Language Coverage

```
GET /research/languages/coverage
```

Shows how stories move across languages with average time lag.

### Claims

```
GET /research/claims
```

Returns claim ledger entries with first-seen timestamps and cluster links.

### Weekly Diff

```
GET /research/diff/weekly
```

Shows what changed since last week:
- Top rising/falling topics
- Channels with biggest volume or quality shifts

### Rebuild

```
POST /research/rebuild
```

Triggers refresh of materialized views (admin-only).

---

## Item Explanation

The item detail view includes an "explanation" section showing:

| Field | Description |
|-------|-------------|
| Status | Current item status |
| Relevance score | Score vs effective threshold |
| Importance score | Score vs threshold |
| Gate decision | Relevance gate pass/fail with reason |

This helps diagnose why items were included or suppressed.

---

## Configuration

### Enable Research Dashboard

```env
RESEARCH_ENABLED=true
EXPANDED_VIEW_BASE_URL=https://digest.example.com
EXPANDED_VIEW_SIGNING_SECRET=changeme
```

### Rate Limiting

- 30 requests per minute per IP
- Burst capacity of 60 requests

---

## Observability

### Prometheus Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `research_api_requests_total` | Counter | `route`, `status` | Request count |
| `research_api_latency_seconds` | Histogram | `route` | Request latency |
| `research_api_result_size` | Gauge | `route` | Average results per request |

### Audit Logging

Requests from authenticated sessions are logged to `research_audit_log` with:
- User ID
- Route
- Status code
- Client IP
- Query hash

### Slow Query Warnings

Queries taking longer than 2 seconds are logged with query hash (not full text).

---

## Database Schema

### research_sessions

| Column | Type | Description |
|--------|------|-------------|
| `token` | TEXT | Session token (primary key) |
| `user_id` | BIGINT | Telegram user ID |
| `expires_at` | TIMESTAMPTZ | Session expiry |
| `created_at` | TIMESTAMPTZ | Creation time |

### research_audit_log

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID | Primary key |
| `user_id` | BIGINT | Requesting user |
| `route` | TEXT | API route |
| `status` | INT | HTTP status code |
| `client_ip` | TEXT | Client IP address |
| `query_hash` | TEXT | Hash of query parameters |
| `created_at` | TIMESTAMPTZ | Request time |

---

## Implementation Files

| File | Purpose |
|------|---------|
| `internal/research/handler.go` | HTTP handler and routing |
| `internal/research/auth.go` | Token and session management |
| `internal/research/renderer.go` | HTML template rendering |
| `internal/research/metrics.go` | Prometheus metrics |
| `internal/research/templates/*.html` | HTML templates |
| `internal/storage/research.go` | Database queries |

---

## Templates

| Template | Purpose |
|----------|---------|
| `index.html` | Dashboard home |
| `search.html` | Search results |
| `item.html` | Item detail with explanation |
| `cluster.html` | Cluster detail |
| `evidence.html` | Evidence sources list |
| `table.html` | Generic table view |
| `error.html` | Error pages |

---

## See Also

- [Item Expansion](item-expansion.md) - Per-item expanded views with ChatGPT integration
- [Source Enrichment](source-enrichment.md) - Evidence retrieval shown in research views
- [Clustering](clustering.md) - How clusters are formed and analyzed
