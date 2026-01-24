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
### 1) Lightweight Web UI (recommended)
Single-page dashboard with a small set of views:
- Search
- Cluster inspector
- Channel overlap graph
- Topic/narrative timelines
- Source quality dashboards
- Evidence explorer

### 2) Extended Bot Commands (optional)
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

## Data and Query Strategy
- Reuse existing tables for items, clusters, channel stats, evidence, and ratings.
- Add lightweight aggregation queries (time buckets, cluster counts, overlap edges).
- Cache heavy computations (channel overlap, topic timelines) daily or on-demand.
- Index the most common filters (time, channel, topic, language).

## Architecture
- Add a read-only “research API” in the existing app or a small sidecar service.
- Web UI consumes API endpoints; bot commands reuse the same query layer.
- Keep scope minimal: no writes except audit logs.

## Success Criteria
- Research queries under 2s for typical filters.
- Clear visibility into why items were included or suppressed.
- Ability to answer: “who amplified this story?” and “what external evidence exists?”

## Open Questions
- Preferred default UI: web-only, bot-only, or both?
- Which views should be MVP vs. follow-up?
- Do we need long-term retention for evidence and cluster history beyond current TTLs?
