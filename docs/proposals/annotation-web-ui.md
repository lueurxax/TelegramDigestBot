# Proposal: Web-Based Annotation (Expanded View + Research Search)

## Summary
Move item annotation from Telegram bot commands into the web UI:
- Expanded view: annotate the current item directly.
- Research search: bulk annotation from search results.
Bot commands remain available short-term but are no longer the primary flow.

## Goals
- Reduce clicks and context switching during annotation.
- Make annotation work where the content is already shown (expanded view + search).
- Support fast labeling of multiple items in search results.
- Keep permissions aligned with existing admin-only access.

## Non-Goals
- Public or multi-user annotation.
- New auth system; reuse existing expanded-view security.
- New configuration flags (reuse current settings).

## UX
### Primary Flow (List-First)
The main annotation flow is the list view (Research Search):
- Each row shows compact icon buttons on the right:
  - ✅ good
  - ⚠️ bad
  - 🚫 irrelevant
- One tap = save label. No modal.
- After tap: highlight row and show a small status badge + timestamp.
- Optional comment remains available via a small “note” icon per row (collapsible input).

### Expanded View (Secondary)
Keep annotation available in expanded view, but as a compact icon strip
to match the list UI (no large buttons).

### Status Indicators
Show last label next to each item:
- ✅ good / ⚠️ bad / 🚫 irrelevant
- Timestamp + (optional) comment preview

## Data Model
Reuse existing `item_ratings` but add a small source field so we can compare
workflows (list vs expanded):
- Add column: `source TEXT NOT NULL DEFAULT 'web-list'`
  - Allowed values: `web-list`, `web-expanded`
- If multiple ratings exist, treat the latest as the current label for UI.

## API / Endpoints
Add JSON endpoints under the research UI:

- `POST /research/annotate`
  - Body: `{ "item_id": "<uuid>", "rating": "good|bad|irrelevant", "comment": "...", "source": "web-list|web-expanded" }`
  - Response: `{ "ok": true, "item_id": "...", "rating": "...", "created_at": "..." }`

- `POST /research/annotate/batch`
  - Body: `{ "item_ids": ["..."], "rating": "good|bad|irrelevant", "comment": "...", "source": "web-list|web-expanded" }`
  - Response: `{ "ok": true, "count": 12 }`

- `GET /research/annotations?item_id=<uuid>`
  - Returns recent annotations for an item (latest first).

These endpoints live in the existing research web server and share the same auth guard as the expanded view.

## Permissions & Security
- Only allow admin users (same policy as expanded view).
- Accept HMAC-expanded-view tokens for item-level access.
- For research UI, rely on the existing admin session mechanism.
- Log actor id (admin id) with each rating for audit.
- HMAC tokens are scoped to a single item; only that item can be annotated when
  a token is used (no cross‑item access).

## Rendering & Templates
### Expanded View Template
- Add a compact annotation module with three buttons + optional comment.
- Use JS `fetch` to call `/research/annotate`.
- Update UI state inline on success.

### Research Search Template
- Add action buttons in each row.
- Add batch selection checkbox column + “Apply to selected”.
- Provide small “last label” badge.
- Support keyboard shortcuts: `g`, `b`, `i` when row focused.
- Ensure touch targets are at least 32px for mobile.

## Bot Behavior
- Remove `/annotate` commands and callbacks from the bot once web UI ships.
- Update `/help` and any internal docs to reflect web-only annotation.

## Input Validation
- `rating` must be one of: `good`, `bad`, `irrelevant` (strict enum).
- `item_id` must be a valid UUID and exist.
- `item_ids` must be non-empty for batch, max 100 IDs per request.
- `source` must be `web-list` or `web-expanded`; reject anything else.
- `comment` is optional, max 500 chars (truncate or reject on overflow).

## Error UX
- Inline error banner on a row if annotation fails.
- Button re-enabled with “Retry” option.
- For batch: show a small toast with error count and keep failed items selected.

## API Error Responses
- `400` invalid payload (bad UUID, enum, source, empty batch)
- `401` unauthorized (not admin / no session)
- `404` item not found
- `429` rate limited
- `500` server error
Response shape: `{ "ok": false, "error": "<code>", "message": "<human message>" }`

## Rate Limiting
- Per admin: 60 requests/minute with burst 20.
- Batch endpoint: max 6 requests/minute.
- Rate limit enforced per admin ID (not IP).
Return `429` with `Retry-After` seconds.

## Performance & UX
- Search list must be paginated (default 50 items per page).
- For large lists, use pagination first; virtual scroll is optional.
- UI uses optimistic updates: update label immediately, roll back on error.
- Always show saving state (debounce double‑click).

## Migration Plan
- Migration: add `source` column to `item_ratings` with default `web-list`.
- Backfill existing rows with `web-list`.
- Keep read path compatible with missing column (short rollback window).

## Observability
Add counters:
- `digest_annotations_total{rating="good|bad|irrelevant"}`
- `digest_annotation_requests_total{status="ok|error"}`
- `digest_annotation_batch_total`
Add histogram:
- `digest_annotation_request_duration_seconds`

## Rollout
1. Deploy web UI annotation in expanded view.
2. Add search-result annotation actions.
3. Update bot help text.

## Testing Strategy
- Unit tests for annotation handlers (valid/invalid rating, missing id).
- Error-path tests: bad enum, bad UUID, empty batch, oversize batch, missing item.
- Rate‑limit test: exceed limit, expect 429 + Retry‑After.
- Concurrency test: two writes to same item, latest wins.
- Integration test: create item, call annotate, verify `item_ratings` row.
- UI smoke test: annotate from expanded view & search list.

## Decisions
- Comments remain optional for all ratings to minimize friction.
- Debounce and saving state are always on.
