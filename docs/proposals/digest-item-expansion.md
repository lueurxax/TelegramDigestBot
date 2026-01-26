# On-Demand Item Expansion and Q&A

> **Status: PROPOSAL** (January 2026)
>
> Add a short link per digest item that opens an expanded view with evidence, context, and optional Q&A.

## Summary
Provide a read-only expansion page for each digest item. The expansion uses existing evidence, link previews, and cluster context to avoid extra LLM cost. For Q&A, default to a deep link into ChatGPT (user subscription). Server-side Q&A is optional and strictly budgeted.

## Goals
- Provide deeper context without increasing baseline LLM usage.
- Enable user-driven Q&A for specific items.
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
- Optional: `Ask here` if server-side Q&A is enabled.

## Design

### Access Control
- Use signed, time-limited tokens to prevent guessing.
- Token payload includes `item_id`, `admin_user_id`, and `exp`.
- Verify token and admin status on every request.
- Links expire after `EXPANDED_VIEW_TTL_HOURS` (default 72).

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

### Optional Server-Side Q&A
- Enabled via `EXPANDED_QA_ENABLED=true`.
- Rate-limited and budgeted; uses existing LLM task chain.
- If budget exceeded, return a friendly error and suggest ChatGPT.

## Configuration
```bash
EXPANDED_VIEW_ENABLED=true
EXPANDED_VIEW_BASE_URL=https://digest.local
EXPANDED_VIEW_SIGNING_SECRET=changeme
EXPANDED_VIEW_TTL_HOURS=72
EXPANDED_VIEW_REQUIRE_ADMIN=true
EXPANDED_QA_ENABLED=false
EXPANDED_QA_RPM=30
```

## Observability
- Counters: `expanded_view_hits_total`, `expanded_view_denied_total`, `expanded_view_errors_total`.
- Q&A metrics: `expanded_qa_requests_total`, `expanded_qa_errors_total`, `expanded_qa_latency_seconds`.
- Log token validation failures and missing item IDs.

## Success Criteria
- Users can open expanded context in 1 click.
- No increase in baseline LLM cost.
- Q&A cost only incurred on explicit requests.

## Open Questions
- Should expanded views be indexed or always private?
- Should prompts include raw message text or summary+evidence only?
