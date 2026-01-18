# Source Enrichment & Fact-Checking Pipeline

## Goal
Improve factual accuracy, context, and clustering by enriching each item with corroborating sources and structured evidence before summarization.

## Scope
- Sources: all available (RSS/Atom, public APIs, web search, and known news sites).
- Latency: end-to-end enrichment must finish within **5 minutes** per digest window.
- Language: support RU/EN and handle mixed languages; do not block other languages.
- Cost: no ceiling; prioritize quality over cost.

## Non-Goals
- Fully automated “truth” certification. We provide confidence signals, not absolute truth.
- Replacing the existing summary pipeline; this is an enrichment layer.

## Proposed Flow
1. **Item normalization**
   - Extract entities, key phrases, and a short claim summary.
2. **Query generation**
   - Build 2-4 queries per item (native + translated if needed).
3. **Source retrieval**
   - Run retrieval in parallel with per-item time budget (e.g., 30-60s).
   - Fetch top N sources (default 3-5) by relevance and source quality.
4. **Evidence extraction**
   - For each source, extract headline, snippet, publish time, and key claims.
5. **Fact-check scoring**
   - Agreement score based on entity overlap + claim similarity.
   - Contradiction detection: flag if core claim differs from majority.
6. **Context injection**
   - Add “Evidence” and “Background” bullets to LLM prompt.
7. **Clustering improvement**
   - Use evidence embeddings to improve cluster cohesion and cross-source linking.

## Source Catalog (v1)
Providers (query APIs):
- GDELT 2.1 Events + GDELT DOC 2.1
- Event Registry
- NewsAPI (if licensing allows)
- OpenSearch providers (Brave Search, SerpAPI, or self-hosted SearxNG)

Default allowlist domains (seed list, extendable):
- Global: `reuters.com`, `apnews.com`, `bbc.com`, `aljazeera.com`, `dw.com`
- Tech/Science: `nature.com`, `sciencemag.org`, `arxiv.org`, `mit.edu`, `wired.com`
- Ukraine: `pravda.com.ua`, `suspilne.media`, `unian.net`, `ukrinform.net`
- Russia (for reference/corroboration): `meduza.io`, `theins.ru`, `novayagazeta.eu`
- Official sources: `*.gov`, `*.mil`, `who.int`, `un.org`, `europa.eu`

## Source Selection Rules
- Prefer high-trust domains; demote anonymous blogs and link farms.
- Require at least one non-social source for **High** confidence.
- Keep social sources as context only (never sole evidence).

## Fact-Check Scoring (v1)
- **Agreement score**: average cosine similarity of claim embeddings + entity overlap ratio.
- **Confidence tiers**:
  - High: ≥2 independent sources agree, no contradictions.
  - Medium: 1 corroborating source or weak agreement.
  - Low: no corroboration or contradictions detected.
- **Output**: store agreement score, contradiction flag, top evidence links.

## Language Handling
- Detect language for each item and evidence snippet.
- If non-EN/RU, translate only the **query** (not the content) for retrieval.
- Use multilingual embeddings for claim similarity.

## Data Model Changes
Add tables:
- `evidence_sources` (id, url, domain, title, published_at, language, raw_text)
- `evidence_claims` (id, evidence_id, claim_text, embedding, entities)
- `item_evidence` (item_id, evidence_id, agreement_score, contradiction, matched_at)
Add fields:
- `items.fact_check_score` (float)
- `items.fact_check_tier` (enum: high|medium|low)
- `items.fact_check_notes` (text/json)

## Configuration
- `ENRICHMENT_ENABLED=true`
- `ENRICHMENT_MAX_SOURCES=5`
- `ENRICHMENT_MAX_SECONDS=60` (per item)
- `ENRICHMENT_MIN_AGREEMENT=0.65`
- `ENRICHMENT_ALLOWLIST_DOMAINS` / `ENRICHMENT_DENYLIST_DOMAINS`
- `ENRICHMENT_QUERY_TRANSLATE=true`
- `ENRICHMENT_PROVIDERS=gdelt,eventregistry,newsapi,opensearch`

## UX Impact
- Digest items include:
  - “Evidence: 3 sources, confidence: High”
  - Optional 1-2 bullet “Context” lines
- Admin view (future): `/evidence <item_id>` to inspect sources.

## Risks & Mitigations
- **Latency spikes**: enforce per-item timeout and global 5-minute cap.
- **Source noise**: domain allow/deny lists and source quality weight.
- **Hallucinations**: keep evidence links in output; never claim certainty.

## Rollout Plan
1. Implement storage + retrieval layer behind `ENRICHMENT_ENABLED`.
2. Add evidence extraction + scoring, log metrics.
3. Integrate into clustering and summarization prompts.
4. Enable by default once metrics stabilize.

## Success Metrics
- Lower “irrelevant” ratings on annotated items.
- Higher cluster cohesion (fewer fragmented clusters).
- Reduced contradiction rate reported by fact-check layer.
