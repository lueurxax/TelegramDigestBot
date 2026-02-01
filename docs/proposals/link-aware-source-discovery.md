# Link‑Aware Source Discovery & Summaries

## Goal
When Telegram posts include links, use the linked article text to improve summaries and bullets, and find the original source even if it is in a different language.

## Problem
- Summaries/bullets can repeat short Telegram preview text instead of the linked article.
- Query generation often ignores the original source if it is in a different language (e.g., RU repost of EN article).
- Bullet extraction currently only uses `PreviewText`, not resolved link content.

## Non‑Goals
- Full cross‑language clustering.
- No *additional* translation passes beyond the existing pipeline defaults (avoid per‑link/per‑query translation explosion).
- No duplicate link‑resolution logic (reuse existing resolver/cache).

## Proposed Changes

### 1) Link Content as Primary Context (Guarded)
If resolved link content exists, treat it as primary context **only when the link is likely the main article**.

**Rules:**
- Use link content as primary **only if** at least one of:
  - Message text is short (< 120 chars), or
  - Link has high word count (e.g., ≥ 200 words), or
  - Domain is on a curated “news‑source allowlist”.
- If link looks like CTA/donation/promo (contains “donate”, “patreon”, “subscribe”, “support us”), **do not** treat as primary.
- Prefer `ResolvedLink.Content`, else `Title + Description`, else `PreviewText`.
- If message text is substantial and link looks supplemental, keep message as primary and use link content as secondary context only.

**Implementation:**
- Extend `BulletExtractionInput` to include `LinkContext` (or reuse `PreviewText` with resolved link content).
- Summary prompt should include the same `LinkContext` when the link is primary, and as secondary context otherwise.
- `buildResolvedLinksText()` should prioritize `link.Content`, then `Title + Description`.

**Decision Logic (single best link):**
- Choose the first link that satisfies the “primary” rule.
- If multiple links qualify, pick the highest `WordCount`.
- If none qualify, keep message as primary.

**Prompt Examples (primary vs secondary):**
- Primary link:
  - `[PRIMARY ARTICLE] <link content>`
  - `MESSAGE: <telegram text>`
  - Instruction: “Summarize the PRIMARY ARTICLE. Use MESSAGE only for context.”
- Secondary link:
  - `MESSAGE: <telegram text>`
  - `[SUPPLEMENTAL LINK] <link content>`
  - Instruction: “Summarize MESSAGE. Use SUPPLEMENTAL LINK only if it clarifies or adds facts.”

### 2) Cross‑Language Query Routing (Link‑aware)
Do not drop link data just because link language differs from item language.

**Rules:**
- If `link.Language` is present and differs from item language, still allow link‑based queries in `link.Language`.
- Generate two sets of queries:
  - Item‑language queries from summary/text
  - Link‑language queries from link content

**Implementation:**
- Relax `filterLinksForQueries` so it **does not discard** links with different language.
- Tag link‑language queries with `query.language = link.Language`.
- Use existing `ENRICHMENT_LANGUAGE_POLICY` for final routing (no duplicate policy logic).

### 3) Canonical Source Detection
Detect original sources from link metadata.

**Rules:**
- Parse `<link rel="canonical">`, `og:url`, and JSON‑LD `url`.
- Priority: JSON‑LD `url` > `<link rel="canonical">` > `og:url`.
- If canonical domain differs from current domain, use it as a source hint.

**Implementation:**
- Extend `ExtractWebContent` to return `CanonicalURL` (and domain).
- Add canonical domain/title to `buildLinkHints()` for query generation.

**Canonical Trust Rules (avoid bad canonicals):**
- Accept canonical only if:
  - Domain is in allowlist **or**
  - Canonical domain is the same as the current link domain **or**
  - Canonical domain is in a curated “trusted publisher” list.
- Reject canonical if:
  - Canonical points to known aggregators or syndication hosts (denylist).
  - Canonical URL path is empty or points to a homepage/root.
- If canonical fails trust checks, ignore it and keep current link domain.

**Deduplication with Existing Items (Canonical Match):**
- If canonical URL maps to an existing item:
  - Attach the canonical URL as a corroboration/evidence hint to the current item.
  - Prefer using the existing item’s summary if similarity is high (avoid duplicate summaries).
  - Link items for clustering: store a `canonical_item_id` or add a cluster edge.
- If no match, proceed normally.

### 4) Debug/Observability
Add per‑item debug fields:
- `link_context_used` (bool)
- `link_content_len`
- `link_lang_queries`
- `canonical_source_detected` (bool)

Add counters:
- `link_context_used_total`
- `link_lang_queries_total`
- `canonical_source_detected_total`

## Config & Thresholds
- `link_primary_min_words` (default: 200)
- `link_primary_short_msg_chars` (default: 120)
- `link_primary_allowlist` (domains)
- `link_primary_cta_terms` (case‑insensitive; default: donate, patreon, subscribe, support us, подпишись, поддержи, донат; can include RU/EL terms)
- `link_primary_max_links_considered` (default: 3)
- `link_primary_donation_denylist` (domains; used for CTA suppression)

## Link Resolution & Caching
- Reuse existing link resolver and cache (`link_enrichment_enabled`, `link_cache_ttl`, `tg_link_cache_ttl`).
- If resolution fails or content is empty, fall back to current behavior (preview + message text).
- Canonical parsing uses the already-fetched HTML (no extra request).

## CTA Detection (Reduce False Positives)
- Only treat as CTA if:
  - CTA phrase appears in the last 20% of link content, OR
  - The link domain is on a “donation” denylist.
- Do not reject if CTA appears in the middle of a full article with high word count.

## Performance Impact
- Link‑language queries add at most `maxQueriesPerItem` (already capped).
- Canonical parsing uses existing HTML bytes (no extra fetch).
- Expect small latency increase from extra query set; monitor per‑item query count.

## Fallback Behavior
- If link resolution times out or returns empty content, keep existing summary/bullet behavior.
- If language detection is unknown, use item language and skip link‑language queries.

## Metrics for Success
- Lower duplicate/repetitive summaries (manual review sample of 100 items).
- Increase in “original source found” rate for reposted/translated items.
- Enrichment match rate by language pair (ru→en, ru→el).

## Migration / Backfill
- Optional: no backfill by default.
- If needed, reprocess only items with resolved links in the last N days.

## Acceptance Criteria
- For RU posts linking to EN originals, queries include EN keywords and the original site becomes discoverable.
- Bullet extraction uses link content when available (not just preview text).
- Summaries become less repetitive and closer to the linked article.

## Rollout Plan
1) Ship link content → bullet prompt.
2) Enable link‑language queries (behind setting if needed).
3) Add canonical source detection.

## Testing Strategy
- Unit tests for link language routing + canonical parsing.
- Integration test: link with RU translation + EN original produces EN queries.
- Regression test: no links behaves unchanged.
- CTA detection test (CTA at end vs CTA mid‑article).
- Multi‑link selection test (primary link chosen by word count).
- Latency test for doubled queries (ensure within budget).
- Canonical dedup test: canonical URL matches an existing item → link & summary reuse path triggered.
