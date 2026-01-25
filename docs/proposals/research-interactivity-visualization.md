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

### 3) Extended Bot Commands (optional)
Fast access for quick queries and mobile use:
- `/research search "<query>" [from:2026-01-01] [to:2026-01-31] [channel:@name] [lang:ru] [topic:"Local News"]`
- `/research cluster <cluster_id>`
- `/research channel <@name> overlap [days]`
- `/research evidence <item_id>`

## Core Research Views
### Search Over Archive
Query items and evidence sources with filters, showing:
- matched items + timestamps
- source channel + topic
- quick links to cluster and evidence views

### Cluster Inspection + Timeline
For a cluster:
- all items with timestamps and channel attribution
- canonical summary and topic label
- list of corroborating channels
- timeline view (first appearance, peak activity, decay)

### Channel Relationship Graph
Compute overlap based on shared clusters:
- edge weight = shared_clusters / min(total_clusters_a, total_clusters_b)
- highlight “originators” vs “amplifiers” using first-appearance rate

### Topic and Narrative Dashboards
Show topic distribution over time:
- top topics per week/month
- emerging topics (sudden growth)
- volatility metrics (topic churn)

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

### Claim Ledger (new)
Track recurring claims across clusters:
- first seen timestamp + originating channel
- reappears in later clusters
- contradicted by (if evidence flags conflict)

### Origin vs Amplifier (new)
Per channel:
- % of clusters first-seen in this channel (origin rate)
- % of clusters where channel appears after first mention (amplifier rate)
- top origin topics vs amplification topics

### Cross-Language Coverage (new)
Show how stories move across languages:
- RU-origin corroborated in EN/EL (and time lag)
- per-topic cross-language coverage rate

### Topic Drift (new)
Detect and flag topic label drift:
- same cluster evolving topic labels across windows
- “drifted clusters” list for review

### Channel Bias Lens (new)
Compare topic distributions across channels:
- agenda similarity matrix
- “over/under indexed” topics per channel

### Weekly Diff (new)
“What changed since last week”:
- top rising/falling topics
- channels with biggest volume or quality shifts

## Data and Query Strategy
- Reuse existing tables for items, clusters, channel stats, evidence, and ratings.
- Add lightweight aggregation queries (time buckets, cluster counts, overlap edges).
- Cache heavy computations (channel overlap, topic timelines, drift, origin/amplifier stats) daily or on-demand.
- Index the most common filters (time, channel, topic, language).

## Architecture
- Add a read-only “research API” in the existing app.
- Web UI consumes API endpoints; bot commands reuse the same query layer.
- Keep scope minimal: no writes except audit logs.

### Hybrid Implementation Plan
**Phase 1 (now):**
- Go template web UI served by the existing app (no separate frontend build).
- Auth: bot-based admin session (reuse admin ID list).
- Core views: Search, Cluster, Item/Evidence.
- Grafana dashboards for aggregates and trends.

**Phase 2 (later):**
- Replace UI with Vite app using the same API endpoints.
- Add graph visualization, saved searches, and exports.

### Grafana Dashboards (MVP)
- Topic trends (weekly buckets, top N topics).
- Channel quality (inclusion rate, noise rate, average relevance/importance).
- Evidence match rate (matches per day, per topic).
- Channel overlap (edge list table + optional node graph).
- Originator vs amplifier panel per channel.
- Cross-language corroboration rate over time.
- Evidence match rate by topic and by source domain.
- Time-to-digest distribution (p50/p90).

### Web UI Views (MVP)
- **Search**: query, time range, channel, language, topic.
- **Cluster**: items, timeline, corroboration list, evidence sources.
- **Item**: raw message, summary, scores, evidence matches.
- **Settings**: edit thresholds, language, schedule, and research filters (web UI is easier than bot).

### Transparency (new)
- Per item: why included / suppressed (thresholds, relevance gate decision, dedup reason).
- Per channel: weight history and auto-weight change reasoning.

### Storage + Performance (new)
- Materialized views for topic timelines, channel overlap edges, cluster stats.
- Cached aggregates refreshed hourly + on-demand rebuild endpoint.
- Retention policy section (items, evidence, translations) with default TTLs.

### Auth Model
- Bot-authenticated admin only: web UI requires a valid admin session (Telegram user ID).
- No public access; no multi-user permissions.

## Success Criteria
- Research queries under 2s for typical filters.
- Clear visibility into why items were included or suppressed.
- Ability to answer: “who amplified this story?” and “what external evidence exists?”

## Open Questions
- Preferred default UI: web-only, bot-only, or both?
- Which views should be MVP vs. follow-up?
- Do we need long-term retention for evidence and cluster history beyond current TTLs?
