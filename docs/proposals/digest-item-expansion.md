# On-Demand Item Expansion and Q&A

> **Status: IMPLEMENTED** (January 2026)
>
> Add a short link per digest item that opens an expanded view with evidence, context, and optional Q&A.

## Summary
Provide a read-only expansion page for each digest item. The expansion uses existing evidence, link previews, and cluster context to avoid extra LLM cost. Q&A is handled via a deep link into ChatGPT (user subscription) only.

## Goals
- Provide deeper context without increasing baseline LLM usage.
- Enable user-driven Q&A for specific items via ChatGPT.
- Keep access restricted to bot admins.

## Non-Goals
- Public access or multi-user research portal.
- Running a full chat UI inside the bot.
- Long-term storage of Q&A sessions.

## UX
### Digest Output
- Add a short link or button per item:
  - `More: https://digest.local/i/<token>`

### Expanded Item View
- Raw message text, summary, scores, topic, and channel.
- Evidence list with agreement scores and matched claims.
- Corroborating channels and cluster context.
- Button: `Open in ChatGPT` (pre-filled prompt).

## Design

### Delivery
- Served by the existing bot HTTP server (new handler).
- HTML response (simple template) for direct viewing; no separate service.
- Uses existing ingress + TLS; `EXPANDED_VIEW_BASE_URL` must route to the bot HTTP server.

### Access Control
- Use signed, time-limited tokens to prevent guessing.
- Token payload includes `item_id`, `admin_user_id`, and `exp`.
- Verify token and admin status on every request.
- Links expire after `EXPANDED_VIEW_TTL_HOURS` (default 72).
- Mark pages as `noindex` and block public search engines.
- Token in URL path is acceptable for admin-only use; avoid logging full URLs and set `Referrer-Policy: no-referrer`.
- Optional hardening (future): exchange token for a short-lived httpOnly cookie via `POST /i`.

### Token Format
- `token = base64url(item_id|user_id|exp|sig)`
- `sig = HMAC_SHA256(secret, item_id|user_id|exp)`
- No server-side mapping table required.

### Context Assembly (No New LLM)
- Use existing evidence, link previews, and cluster summary.
- Show claim matches and agreement scores.
- If evidence is missing, show `No evidence available`.

### ChatGPT Deep Link
- Build prompt from summary + top evidence lines + original link(s).
- Open ChatGPT with the prompt (user subscription).
- URL format: `https://chat.openai.com/` + auto-copy prompt to clipboard (preferred, stable).
- If a provider-supported deep-link format exists, use it; otherwise fall back to copy.

### Prompt Size Limits
- Cap prompt size by tokens or characters to avoid exceeding ChatGPT limits.
- `EXPANDED_PROMPT_MAX_TOKENS` (default 3000) or `EXPANDED_PROMPT_MAX_CHARS` (default 12000).
- Truncate duplicate messages first, then evidence lines, then raw text if needed.

### HTML Template
- Simple server-rendered template with inline CSS.
- Mobile-first layout with `meta viewport`.
- Use a compact, readable type scale; avoid heavy assets.

### API
- `GET /i/:token` → HTML page.
- Errors:
  - `401` invalid token or non-admin
  - `404` item not found
  - `410` token expired
- Error UX: minimal HTML page with a short explanation and a “Back to chat” link.
- “Back to chat” should use a Telegram deep link (`tg://resolve?domain=<bot_username>`) with a web fallback to `https://t.me/<bot_username>`.
- Rate limit: basic IP throttling to deter brute-force token guessing.

## Schema Additions
None. Uses existing items, raw_messages, evidence, and cluster tables.

## Configuration
```bash
EXPANDED_VIEW_ENABLED=true
EXPANDED_VIEW_BASE_URL=https://digest.local
EXPANDED_VIEW_SIGNING_SECRET=changeme
EXPANDED_VIEW_TTL_HOURS=72
EXPANDED_VIEW_REQUIRE_ADMIN=true
```

## Dependencies
- Postgres (items, raw_messages, clusters, evidence tables).
- Bot HTTP server exposed via ingress/TLS.
- ChatGPT web for Q&A (external).

## Observability
- Counters: `expanded_view_hits_total`, `expanded_view_denied_total`, `expanded_view_errors_total`.
- Log token validation failures and missing item IDs (redact full token).

## Data Queries
- Item + raw message by `item_id`.
- Evidence list by `item_id` (top N by agreement).
- Cluster context (cluster ID, summary, member channels).
- Corroboration list (distinct channels from cluster).

## Rollout
- Ship behind `EXPANDED_VIEW_ENABLED=false`.
- Enable for admin only, verify access control and templates.
- Add `robots.txt` + `noindex` response headers.

## Success Criteria
- Users can open expanded context in 1 click.
- No increase in baseline LLM cost.
- Q&A cost handled by the user's ChatGPT subscription.

## Testing Strategy
- Unit: token signing/validation, expiry handling.
- HTTP: 200/401/404/410 responses.
- Render: HTML template includes evidence + links.

## Decisions
| Question | Answer |
| --- | --- |
| Should expanded views be indexed or always private? | Indexed internally only (Solr), not public web indexing. This reuses the existing item index; no new expanded-view index. |
| Should prompts include raw message text or summary+evidence only? | Maximum context: raw text, links, corroboration text, original links, and all duplicate messages in one prompt, capped by prompt limits. |
