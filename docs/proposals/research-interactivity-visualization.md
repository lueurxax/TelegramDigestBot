# Interactivity and Visualization for Research

> **Status: PROPOSAL** (January 2026)
>
> This feature has not yet been implemented. It proposes a research-focused exploration layer for media analysis.

## Summary
Add a research-focused exploration layer on top of the existing dataset (items, clusters, evidence, channel stats, and quality signals). The goal is to move beyond passive digests and enable direct investigation of media behavior through interactive search, cluster inspection, and analytics.

This proposal targets a single primary researcher user, so the UX can be minimal but highly functional.

## Goals
- Enable ad-hoc exploration of the archive with filters (time, channel, language, topic, source type).
- Turn clusters into first-class research artifacts with timelines and corroboration views.
- Provide transparency for quality signals and ranking behavior.
- Make evidence retrieval auditable (what sources were found, how they matched).

## Non-Goals
- A public multi-user product or complex access control model.
- Real-time public dashboards or heavy BI tooling.
- Replacing the Telegram bot for daily digest consumption.

## Scope
This proposal includes the full set of research views and dashboards in a single delivery. Required schema additions are part of this proposal; code flags can be used for safe rollout, but all tables/migrations are expected to ship together.

## Experience Surfaces
### 1) Hybrid (Grafana + Web UI)
Use Grafana for analytics and trends, and a minimal web UI for deep drill-down and item-level research.

**Why hybrid**
- Grafana is ideal for time-series dashboards and aggregate metrics.
- A web UI is better for interactive exploration (clusters, items, evidence).

### 2) Lightweight Web UI (deep drill-down)
Single-page dashboard with a small set of views (implemented with Go templates first):
- Search
- Cluster inspector
- Channel overlap graph
- Topic/narrative timelines
- Source quality dashboards
- Evidence explorer

### 3) Extended Bot Commands
Out of scope for this delivery. Use the web UI to avoid duplicating query UX. Bot commands may be added later if needed.

## Core Research Views
### Search Over Archive
Query items and evidence sources with filters, showing:
- matched items + timestamps
- source channel + topic
- quick links to cluster and evidence views
 
**Pagination**
- All search endpoints are paginated (`limit`, `offset`).
- Default limit: 50; max limit: 200.
- Total count is optional; only computed when `include_count=true` to avoid expensive scans.

**Search implementation**
- Engine: Postgres full-text search (tsvector) with fallback to ILIKE for short queries.
- Searchable fields: item summary, raw message text, topic, channel title, evidence source title/description.
- Ranking: combine text rank with recency and importance (e.g., 0.5 * text_rank + 0.3 * importance + 0.2 * recency).
  - Recency is normalized to 0..1 using exponential decay: `recency = exp(-age_days / half_life_days)` (default half-life: 14 days).
  - Evidence scope uses a separate evidence search vector (title + description) and returns evidence hits linked to items when available.

### Cluster Inspection + Timeline
For a cluster:
- all items with timestamps and channel attribution
- canonical summary and topic label
- list of corroborating channels
- timeline view (first appearance, peak activity, decay)

### Channel Relationship Graph
Compute overlap based on shared clusters:
- edge weight = Jaccard index = shared_clusters / (total_clusters_a + total_clusters_b - shared_clusters)
- highlight “originators” vs “amplifiers” using first-appearance rate
 
**Performance note**
- Compute overlap only for top N channels by activity (e.g., top 200).
- Precompute in a materialized view; do not compute on-the-fly for all channels.

**Visualization approach**
- Go templates render a sortable edge list table by default.
- Optional: pre-rendered SVG (server-side) for a static graph view.
- If interactive graphs are required, add a small client-side JS library (D3/vis) later.

### Topic and Narrative Dashboards
Show topic distribution over time:
- top topics per week/month
- emerging topics (sudden growth)
- volatility metrics (topic churn)
 
**Performance note**
- Use time bucketing with windowed aggregates (weekly/monthly).
- Limit to top N topics per window to avoid full-archive scans.

**Bucket granularity**
- Supported buckets: daily, weekly, monthly (default weekly).
- Archive range defaults to last 12 months; older buckets are served from pre-aggregated materialized views.

### Source Quality Analytics
Expose internal signals:
- inclusion rate, noise rate, average importance, relevance variance
- weight history (manual + auto adjustments)
- per-channel contribution to digest

### Evidence & Contradiction Explorer
Per item:
- retrieved external sources
- agreement scores and matched claims
- contradiction flags where available

## Advanced Research Views
### Claim Ledger
Track recurring claims across clusters:
- first seen timestamp + originating channel
- reappears in later clusters
- contradicted by (if evidence flags conflict)

**Claim source & linking**
- Source claims from existing evidence matching (`matched_claims_json`) when available.
- If matched claims are missing, fall back to a lightweight heuristic extractor (sentence split + keyword overlap) without new LLM calls.
- Heuristic extractor: pick top N keywords (tf-idf over item text), then keep sentences with highest keyword overlap; dedupe by normalized hash.
- Deduplicate claims by normalized text hash + embedding similarity (reuse existing embeddings where possible).
- If the heuristic extractor is not implemented, the ledger is evidence-only (no new claim extraction).

### Origin vs Amplifier
Per channel:
- % of clusters first-seen in this channel (origin rate)
- % of clusters where channel appears after first mention (amplifier rate)
- top origin topics vs amplification topics

**Data note**
- `cluster_items` has no timestamp, so origin detection requires joining `items.tg_date`.
- For performance, denormalize `first_item_id` + `first_seen_at` onto `clusters` (or a `cluster_first_appearance` table) during cluster build.

### Cross-Language Coverage
Show how stories move across languages:
- RU-origin corroborated in EN/EL (and time lag)
- per-topic cross-language coverage rate

**Linking approach**
- Item language exists (`items.language`) and is required for cross-language analysis.
- Cross-language linkage is approximate and based on embedding similarity at the cluster level.
- Link clusters within a rolling time window (e.g., 7 days) and require similarity > 0.8.
- No manual linking; accept imperfect matches and display confidence scores.

### Topic Drift
Detect and flag topic label drift:
- same cluster evolving topic labels across windows
- “drifted clusters” list for review

**Drift detection**
- Track topic labels per cluster per time window.
- Compare labels with normalized string similarity + embedding distance (e.g., cosine).
- Flag drift when similarity drops below a threshold (e.g., <0.6) across consecutive windows.
- Provide a manual review list rather than automatic relabeling.

### Channel Bias Lens
Compare topic distributions across channels:
- agenda similarity matrix
- “over/under indexed” topics per channel

**Visualization approach**
- Render as a table/heatmap using HTML + CSS first.
- Optional: client-side JS heatmap if needed for better readability.

### Weekly Diff
“What changed since last week”:
- top rising/falling topics
- channels with biggest volume or quality shifts

## Data and Query Strategy
- Reuse existing tables for items, clusters, channel stats, evidence, and ratings. Items should carry `language`; if missing, add a migration to support language filtering.
- Add lightweight aggregation queries (time buckets, cluster counts).
- Cache heavy computations daily or on-demand.
- Index the most common filters (time, channel, topic, language).

### Required Search Migration
- Add `items.search_vector` (tsvector) computed from summary + raw message text + topic.
- Add GIN index on `items.search_vector`.
- Use a trigger or generated column to keep the vector updated on item/raw message changes.
- Add `evidence_sources.search_vector` (tsvector) computed from title + description.
- Add GIN index on `evidence_sources.search_vector`.

### Required Schema Additions
These features require new tables/columns:
- **Research sessions:** `research_sessions` (token, user_id, expires_at, created_at).
- **Claim Ledger:** `claims` table with `first_seen_at`, `origin_cluster_id`, `cluster_ids[]`, `contradicted_by[]`.
- **Origin vs Amplifier:** `cluster_first_appearance` (cluster_id, channel_id, first_item_id, first_seen_at).
- **Topic Drift:** `cluster_topic_history` (cluster_id, topic, window_start, window_end).
- **Cross-language Coverage:** supported by `items.language`, but cross-language linking would require a bridge table like `cluster_language_links` or a precomputed view keyed by cluster_id + language.

## Architecture
- Add a read-only “research API” in the existing app.
- Web UI consumes API endpoints; the query layer is reusable for future bot commands.
- Keep scope minimal: no user-initiated writes; only session/auth, cache refreshes, and audit logs.

### Research API Endpoints
- `GET /research/search?q=&from=&to=&channel=&topic=&lang=&limit=&offset=&scope=&include_count=`  
  Returns items + timestamps + channel + topic; supports pagination.
- `GET /research/item/:id`  
  Returns raw message, summary, scores, evidence matches.
- `GET /research/cluster/:id`  
  Returns cluster items, timeline buckets, corroborating channels.
- `GET /research/evidence/:item_id`  
  Returns evidence sources with agreement scores and matched claims.
- `GET /research/channels/overlap?limit=&from=&to=`  
  Returns channel overlap edges (Jaccard) for top channels.
- `GET /research/topics/timeline?bucket=weekly&from=&to=&limit=`  
  Returns topic timelines with counts and averages.
- `GET /research/channels/:id/quality?from=&to=`  
  Returns channel quality metrics and weight history.
- `GET /research/claims?from=&to=&limit=`  
  Returns claim ledger entries with first seen and cluster links.
- `GET /research/channels/:id/origin-stats?from=&to=`  
  Returns origin vs amplifier stats for a channel.
- `GET /research/diff/weekly?from=&to=`  
  Returns weekly diff summary (top rising/falling topics, channel shifts).
- `POST /research/rebuild`  
  Triggers cache/materialized view refresh (admin-only).

**Search scopes**
- `scope=items` (default) searches item text/summary/topic.
- `scope=evidence` searches evidence source title/description.
- `scope=all` returns two result lists in one response.

**Errors**
- `400` invalid parameters
- `401` unauthorized
- `404` not found
- `429` rate limited

### Hybrid Implementation Plan
- Go template web UI served by the existing app (no separate frontend build).
- Auth: bot-based admin session (reuse admin ID list).
- Grafana dashboards for aggregates and trends.
- Replace UI with Vite app using the same API endpoints when needed.

### Grafana Dashboards
- Topic trends (weekly buckets, top N topics).
- Channel quality (inclusion rate, noise rate, average relevance/importance).
- Evidence match rate (matches per day, per topic).
- Time-to-digest distribution (p50/p90).

**Grafana data sources & provisioning**
- Data source: Postgres read-only user (primary). Prometheus metrics for latency/volume overlays.
- Provisioning: dashboard JSON in `deploy/k8s/grafana-dashboards.yaml` (manual updates in repo).
- No Terraform required for initial delivery.

### Web UI Views
- **Search**: query, time range, channel, language, topic.
- **Cluster**: items, timeline, corroboration list, evidence sources.
- **Item**: raw message, summary, scores, evidence matches.
- **Settings (read-only)**: show current thresholds, language, schedule, and research filters (edits remain bot-only).

### Transparency (new)
- Per item: why included / suppressed (thresholds, relevance gate decision, dedup reason).
- Per channel: weight history and auto-weight change reasoning.

**Data/UI**
- Item detail shows a “Why included / suppressed” panel using existing drop logs and relevance gate metadata.
- Channel page uses `channel_quality_history`; extend if additional fields are needed.

### Storage + Performance (new)
- Materialized views for topic timelines, channel overlap edges, cluster stats.
- Cached aggregates refreshed hourly + on-demand rebuild endpoint.
- Retention policy section (items, evidence, translations) with default TTLs.

### Retention Defaults
- items: 18 months
- evidence: 12 months
- translations: 6 months
- cluster stats: indefinite

**Materialized views location**
- Stored in Postgres under the `public` schema.
- Refresh via scheduled job in worker mode or an admin-only `/research/rebuild` endpoint.

**Materialized view definitions (outline)**
- `mv_topic_timeline`: `topic`, `bucket_date`, `item_count`, `avg_importance`, `avg_relevance`.
- `mv_channel_overlap`: `channel_a`, `channel_b`, `shared_clusters`, `total_a`, `total_b`, `jaccard`.
- `mv_cluster_stats`: `cluster_id`, `topic`, `first_seen_at`, `last_seen_at`, `item_count`, `unique_channels`.

**Refresh strategy**
- Frequency: hourly (worker cron) + on-demand admin rebuild.
- Use `REFRESH MATERIALIZED VIEW CONCURRENTLY` when possible (requires unique index).

**Indexes**
- `mv_topic_timeline`: index on `(bucket_date, topic)`.
- `mv_channel_overlap`: index on `(channel_a, channel_b)` and `(jaccard DESC)`.
- `mv_cluster_stats`: index on `(first_seen_at)` and `(topic)`.

**Cache semantics**
- Cache is stored in Postgres tables (not Redis/memory) for durability.
- On-demand refresh is triggered by admin action via `/research/rebuild`.
- Invalidation is time-based (hourly refresh) plus explicit rebuild.
- Cache keys are deterministic by view name + time window + filter params (e.g., `topic_timeline:2026-01-01:2026-01-31`).

### Auth Model
- Bot-authenticated admin only: web UI requires a valid admin session (Telegram user ID).
- Use the existing expanded-view HMAC token scheme for short-lived access:
  - User requests `/research login` in the bot.
  - Bot returns a signed token link: `https://<host>/research/login?token=<...>`.
  - Web UI sets an HTTP-only session cookie after token validation.
  - Tokens expire quickly (e.g., 10 minutes); sessions expire after a short TTL (e.g., 24h).
- No public access; no multi-user permissions.

**Session storage**
- Store sessions in Postgres (`research_sessions` table) keyed by random token.
- Cookie is opaque (random bytes), not signed.
- CSRF: required only for write endpoints (none in this scope); read-only API does not need CSRF protection.

## Observability
- Counters: `research_api_requests_total{route,status}`.
- Histogram: `research_api_latency_seconds{route}`.
- Gauges: `research_api_result_size{route}` (avg results per request).
- Logs: validation errors and slow query warnings (include query hash, not full text).

## Testing Strategy
- Unit: query param validation, pagination bounds, filter combinations.
- Integration: seeded fixture dataset for `/research/search`, `/research/cluster/:id`, `/research/evidence/:item_id`.
- Performance: load test p50/p95 latency for `/research/search` and `/research/cluster/:id` (target <2s).

## Dependencies
- Postgres (items, clusters, evidence, channel stats).
- Grafana optional for dashboards (no hard dependency for the web UI).

## Deployment Changes
- Add new routes under `/research/*` (UI + API).
- Ingress update to expose `/research/` on the same host.
- Optional Grafana dashboards deployment if not already present.

## Success Criteria
- Research queries under 2s for typical filters.
- Clear visibility into why items were included or suppressed.
- Ability to answer: “who amplified this story?” and “what external evidence exists?”

## Open Questions
None.
