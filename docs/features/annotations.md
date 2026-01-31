# Annotation System

The annotation system allows administrators to label digest items for quality evaluation and threshold tuning. Labels feed into the feedback loop that automatically adjusts relevance thresholds over time.

## Overview

Annotations provide a structured workflow for reviewing processed items:

1. **Enqueue** items from a time window into the annotation queue
2. **Assign** items one at a time to reviewers
3. **Label** each item as good, bad, or irrelevant
4. **Export** labeled data for evaluation tooling

Labels are stored in `item_ratings` and used by the auto-threshold tuning system (see [Content Quality](content-quality.md)).

---

## Commands

### Enqueue Items

```
/annotate enqueue [hours] [limit]
```

Adds items from the last N hours to the annotation queue.

| Parameter | Default | Description |
|-----------|---------|-------------|
| hours | 24 | Look back window |
| limit | 50 | Maximum items to enqueue |

Only items with status `ready` or `rejected` that have non-empty text are enqueued. Duplicate items are skipped.

### Get Next Item

```
/annotate next
```

Assigns the next pending item to you and displays it with:
- Channel name and message link
- Item ID and timestamp
- Relevance and importance scores
- Topic (if assigned)
- Summary and original text

The message includes inline buttons for quick labeling.

### Label an Item

```
/annotate label <good|bad|irrelevant> [comment]
```

Labels your currently assigned item.

| Label | Meaning |
|-------|---------|
| good | Item belongs in the digest |
| bad | Item is low quality or misleading |
| irrelevant | Item is off-topic or noise |

The optional comment is stored for review.

### Skip an Item

```
/annotate skip
```

Skips your currently assigned item without labeling. Use when you're unsure or need more context.

### View Statistics

```
/annotate stats
```

Shows queue status breakdown:
- Pending: awaiting assignment
- Assigned: currently being reviewed
- Labeled: completed with rating
- Skipped: skipped without rating

---

## Inline Buttons

Each annotation card includes quick-action buttons:

| Button | Action |
|--------|--------|
| 👍 Good | Label as good |
| 👎 Bad | Label as bad |
| 🚫 Irrelevant | Label as irrelevant |
| ⏭ Skip | Skip without labeling |

After labeling, the next item is automatically assigned and displayed.

---

## Queue States

```
pending -> assigned -> labeled
                    \-> skipped
```

| State | Description |
|-------|-------------|
| pending | In queue, awaiting assignment |
| assigned | Assigned to a reviewer |
| labeled | Reviewed and rated |
| skipped | Skipped without rating |

Items are assigned using `FOR UPDATE SKIP LOCKED` to prevent conflicts when multiple reviewers work simultaneously.

---

## Integration with Feedback Loop

Labels flow into the feedback system:

1. **Annotation labels** are saved to `item_ratings` table
2. **Auto-threshold tuning** reads ratings with time decay
3. **Global thresholds** adjust based on net rating score
4. **Per-channel adjustments** apply reliability penalties

See [Content Quality - Feedback Loop](content-quality.md#feedback-loop-and-threshold-tuning) for tuning algorithm details.

---

## Data Export

Labeled annotations can be exported for offline evaluation:

```bash
go run ./cmd/tools/labels \
  -dsn "$POSTGRES_DSN" \
  -out docs/eval/golden.jsonl \
  -limit 500
```

Output format (JSONL):
```json
{"id": "uuid", "label": "good", "relevance_score": 0.72, "importance_score": 0.65}
```

Use with the evaluation tool to measure precision/recall:

```bash
go run ./cmd/tools/eval \
  -input docs/eval/golden.jsonl \
  -relevance-threshold 0.5 \
  -importance-threshold 0.3
```

---

## Database Schema

### annotation_queue

| Column | Type | Description |
|--------|------|-------------|
| id | UUID | Primary key |
| item_id | UUID | FK to items.id |
| status | TEXT | pending, assigned, labeled, skipped |
| assigned_to | BIGINT | Telegram user ID |
| assigned_at | TIMESTAMPTZ | Assignment time |
| label | TEXT | good, bad, irrelevant |
| comment | TEXT | Optional reviewer notes |
| created_at | TIMESTAMPTZ | Enqueue time |
| updated_at | TIMESTAMPTZ | Last status change |

---

## Implementation Files

| File | Purpose |
|------|---------|
| `internal/bot/annotations.go` | Bot commands and UI |
| `internal/storage/annotations.go` | Queue operations |
| `cmd/tools/labels` | Export utility |
| `cmd/tools/eval` | Evaluation utility |

---

## Best Practices

1. **Enqueue regularly**: Add items from each digest window for balanced coverage
2. **Be consistent**: Use the same criteria across all items
3. **Add comments**: Note edge cases for future reference
4. **Review skipped items**: Periodically check skipped items with fresh eyes
5. **Export and evaluate**: Run the eval tool after accumulating ~100+ labels

---

## Web-Based Annotation

In addition to bot commands, annotations can be submitted through the research dashboard web UI. This provides a faster workflow when reviewing multiple items.

### Access

Web annotation uses the same admin authentication as the research dashboard. Log in via `/research login` bot command, then access the research search at `/research/search`.

### Annotation Surfaces

**Research Search (List View)**
- Each search result row displays annotation buttons
- One tap saves a label immediately (no modal)
- After labeling, the row shows a status badge with timestamp
- Optional comments available via a collapsible note icon
- Supports batch selection with "Apply to selected" for multiple items
- Keyboard shortcuts: `g` (good), `b` (bad), `i` (irrelevant) when row focused

**Expanded View**
- Compact annotation strip available on item detail pages
- Same one-tap labeling workflow as list view

### API Endpoints

Web annotation is powered by JSON endpoints under `/research/`:

**Single Annotation**

```
POST /research/annotate
```

Request body:
```json
{
  "item_id": "<uuid>",
  "rating": "good|bad|irrelevant",
  "comment": "optional note",
  "source": "web-list|web-expanded"
}
```

Response:
```json
{
  "ok": true,
  "item_id": "...",
  "rating": "...",
  "created_at": "..."
}
```

**Batch Annotation**

```
POST /research/annotate/batch
```

Request body:
```json
{
  "item_ids": ["uuid1", "uuid2", ...],
  "rating": "good|bad|irrelevant",
  "comment": "optional note",
  "source": "web-list"
}
```

Response:
```json
{
  "ok": true,
  "count": 12
}
```

Maximum 100 items per batch request.

**Fetch Annotations**

```
GET /research/annotations?item_id=<uuid>
```

Returns recent annotations for an item, most recent first.

### Input Validation

| Field | Constraints |
|-------|-------------|
| `rating` | Required. Must be `good`, `bad`, or `irrelevant` |
| `item_id` | Required. Valid UUID, item must exist |
| `item_ids` | Required for batch. Non-empty, max 100 UUIDs |
| `source` | Required. Must be `web-list` or `web-expanded` |
| `comment` | Optional. Max 500 characters |

### Error Responses

| Status | Code | Description |
|--------|------|-------------|
| 400 | `invalid_payload` | Bad UUID, invalid rating, invalid source, empty batch |
| 401 | `unauthorized` | Not admin or no session |
| 404 | `not_found` | Item not found |
| 429 | `rate_limited` | Rate limit exceeded |
| 500 | `save_failed` | Server error |

Error response format:
```json
{
  "ok": false,
  "error": "<code>",
  "message": "<human message>"
}
```

### Rate Limiting

Rate limits are enforced per admin user ID:

| Endpoint | Limit | Burst |
|----------|-------|-------|
| Single (`/annotate`) | 60 requests/minute | 20 |
| Batch (`/annotate/batch`) | 6 requests/minute | 6 |

When rate limited, the response includes a `Retry-After` header with seconds to wait.

### Source Tracking

The `source` field distinguishes where annotations originate:

| Source | Meaning |
|--------|---------|
| `web-list` | Annotated from research search list view |
| `web-expanded` | Annotated from expanded item detail view |

This allows comparison of workflow efficiency and accuracy between the two interfaces. The source is stored in the `item_ratings.source` column.

### Integration with Research Dashboard

For full documentation on the research dashboard including search, item detail, and authentication, see [Research Dashboard](research-dashboard.md).
