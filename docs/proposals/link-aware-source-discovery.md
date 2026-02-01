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

### 3) Canonical Source Detection
Detect original sources from link metadata.

**Rules:**
- Parse `<link rel="canonical">`, `og:url`, and JSON‑LD `url`.
- If canonical domain differs from current domain, use it as a source hint.

**Implementation:**
- Extend `ExtractWebContent` to return `CanonicalURL` (and domain).
- Add canonical domain/title to `buildLinkHints()` for query generation.

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
- `link_primary_cta_terms` (case‑insensitive; default: donate, patreon, subscribe, support us; can include RU/EL terms)
- `link_primary_max_links_considered` (default: 3)

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
