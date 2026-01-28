# On-Demand Item Expansion

The item expansion feature provides a read-only HTML page for each digest item, showing detailed evidence, context, and cluster information. Users can explore topics further via Apple Shortcuts integration with ChatGPT.

## Overview

Each digest item includes a short link that opens an expanded view with:
- Full message text and summary
- Message images (if available)
- Evidence sources with agreement scores
- Related items from the same cluster
- ChatGPT Q&A via Apple Shortcuts

The feature reuses existing evidence and cluster data, avoiding additional LLM costs for the expansion itself.

---

## Configuration

### Enable Expanded Views

```env
EXPANDED_VIEW_ENABLED=true
EXPANDED_VIEW_BASE_URL=https://digest.example.com
EXPANDED_VIEW_SIGNING_SECRET=changeme
```

### Token Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `EXPANDED_VIEW_ENABLED` | `false` | Enable expanded view feature |
| `EXPANDED_VIEW_BASE_URL` | - | Public URL where expanded views are served |
| `EXPANDED_VIEW_SIGNING_SECRET` | - | HMAC signing secret for tokens (required) |
| `EXPANDED_VIEW_TTL_HOURS` | `72` | Token expiration time in hours |
| `EXPANDED_VIEW_REQUIRE_ADMIN` | `true` | Restrict access to admin users |
| `EXPANDED_VIEW_ALLOW_SYSTEM_TOKENS` | `false` | Allow digest links (user_id=0) to bypass admin check |

### Apple Shortcuts Integration

| Setting | Default | Description |
|---------|---------|-------------|
| `EXPANDED_CHATGPT_SHORTCUT_ENABLED` | `false` | Enable ChatGPT shortcut button |
| `EXPANDED_CHATGPT_SHORTCUT_NAME` | `Ask ChatGPT` | Name of the iOS shortcut |
| `EXPANDED_CHATGPT_SHORTCUT_ICLOUD_URL` | - | iCloud link to install the shortcut |
| `EXPANDED_SHORTCUT_URL_MAX_CHARS` | `2000` | Max prompt length in shortcuts URL |

---

## Access Control

The system uses HMAC-signed tokens to prevent URL guessing. Tokens encode:
- Item ID (UUID)
- User ID (Telegram user who generated the link, or 0 for system-generated)
- Expiration timestamp

### Access Modes

**Admin-only mode** (`EXPANDED_VIEW_REQUIRE_ADMIN=true`):
- Only users in `ADMIN_IDS` can view expanded items
- Non-admin requests receive 401 Unauthorized

**System tokens** (`EXPANDED_VIEW_ALLOW_SYSTEM_TOKENS=true`):
- Digest-generated links use user_id=0 (system token)
- When enabled, these bypass the admin check
- Allows digest recipients to view expanded items without being admins

**Public mode** (`EXPANDED_VIEW_REQUIRE_ADMIN=false`):
- Anyone with a valid, non-expired token can view

### Token Format

Tokens are compact URL-safe strings:
```
token = base64url(item_id | user_id | exp | sig)
sig = HMAC-SHA256(secret, item_id | user_id | exp)[:16]
```

No server-side token storage is required; validation uses cryptographic verification.

---

## Expanded View Content

### Header Section

- Topic/headline
- Channel name with link
- Timestamp
- Link to original Telegram message
- Relevance and importance scores
- Message image (displayed inline if available)

### Summary Section

- Item summary (HTML rendering enabled only in admin-only mode without system tokens)

### Original Message

- Full raw message text with preserved formatting
- Preview/link content if different from message text

### Evidence Sources

For each evidence source (up to 5):
- Source title and URL
- Domain name
- Agreement score (color-coded: green for >= 70%, orange otherwise)
- Contradiction indicator if source disagrees
- Matched claims showing item-to-evidence claim pairs

### Cluster Context

If the item belongs to a cluster with multiple sources:
- Related items with their summaries
- Channel attribution with links
- Up to 5 related items shown

### ChatGPT Integration

When Apple Shortcuts is enabled:
- "Ask on iPhone/Mac" button launches the shortcut
- Link to install the shortcut (one-time setup)
- Collapsible "View prompt" section shows the full context

---

## ChatGPT Prompt

The system builds a comprehensive prompt for ChatGPT containing:

1. **Topic and Summary** - The item's headline and summary
2. **Original Source** - Telegram link and channel info
3. **Original Text** - Full message content
4. **Preview Content** - Link preview text if different
5. **Links in Message** - URLs extracted from message entities and media (up to 10)
6. **Corroboration** - Related messages from other channels with full text
7. **Evidence Sources** - External sources with URLs, descriptions, and matched claims
8. **Standard Questions** - Prompts for background, key facts, nuances, and perspectives

### Prompt Size Handling

- **Shortcuts URL**: Truncated to `EXPANDED_SHORTCUT_URL_MAX_CHARS` (default 2000) due to URL length limits
- **View prompt section**: Shows full prompt without truncation
- Truncated prompts include suffix: "... [Prompt truncated for URL length]"

---

## Apple Shortcuts Integration

The feature uses Apple Shortcuts to send prompts to ChatGPT, providing a native experience for iOS/iPadOS/macOS users.

### Why Apple Shortcuts

- User-Agent detection is unreliable in Telegram's in-app browser
- ChatGPT's `?q=` URL parameter has length limits and doesn't work on mobile
- Apple Shortcuts provides reliable integration via `shortcuts://` URL scheme

### How It Works

1. **One-time setup**: User installs "Ask ChatGPT" shortcut via iCloud link
2. **Usage**: Click button -> Shortcuts app opens -> runs shortcut -> ChatGPT responds

The button triggers:
```
shortcuts://run-shortcut?name=Ask%20ChatGPT&input=<url_encoded_prompt>
```

### Error Handling

If the shortcut is not installed:
- iOS/macOS shows "Shortcut not found" alert
- Non-Apple platforms: link does nothing

The UI mitigates this with:
- Prominent "Install shortcut" link
- Clear button label indicating the requirement

### Shortcut Implementation Options

- Native ChatGPT app Siri Shortcuts actions (recommended)
- Call OpenAI API directly within shortcut (requires API key)
- Copy to clipboard + open ChatGPT app (fallback)

---

## API Endpoint

### GET /i/:token

Returns the expanded view HTML page.

**Responses:**

| Code | Condition |
|------|-----------|
| 200 | Valid token, authorized access |
| 400 | Missing token |
| 401 | Invalid token, non-admin user, or system token denied |
| 404 | Item not found |
| 410 | Token expired |
| 429 | Rate limited |

**Error pages** include:
- Human-readable error message
- "Back to bot" link using `tg://resolve?domain=<bot_username>`

### Rate Limiting

IP-based throttling protects against brute-force token guessing:
- 10 requests per minute per IP
- Burst capacity of 20 requests

### Security Headers

All responses include:
- `X-Robots-Tag: noindex, nofollow` - Prevent search indexing
- `Referrer-Policy: no-referrer` - Prevent token leakage in referrer
- `Cache-Control: private, no-store` - Prevent caching

---

## Deployment

### Routing

The expanded view handler serves on `/i/` path via the health/metrics HTTP server (port 8080 by default). Configure ingress to route:
- `/i/*` - Expanded view handler
- `/robots.txt` - Optional, for additional crawler blocking

### Kubernetes Setup

1. Add `EXPANDED_VIEW_SIGNING_SECRET` to your secrets
2. Configure ingress to route `/i/` to the health server
3. Set `EXPANDED_VIEW_BASE_URL` to your public URL

Example ingress rule:
```yaml
- path: /i/
  pathType: Prefix
  backend:
    service:
      name: digest-bot
      port:
        number: 8080
```

---

## Observability

### Prometheus Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `expanded_view_hits_total` | Counter | `status` | Total requests by HTTP status |
| `expanded_view_denied_total` | Counter | `reason` | Denied requests by reason |
| `expanded_view_errors_total` | Counter | `type` | Errors by type (db_error, render_error) |
| `expanded_view_latency_seconds` | Histogram | - | Request latency |

**Status labels**: 200, 401, 404, 410, 429, 500
**Reason labels**: invalid_token, expired, not_admin, rate_limited
**Type labels**: db_error, render_error

### Logging

Token validation failures and missing items are logged with:
- Redacted token (full token not logged for security)
- Item ID
- User ID (for admin check failures)

---

## Dependencies

- **Database**: items, raw_messages, clusters, evidence tables
- **Health server**: Serves expanded views on existing HTTP port
- **Ingress/TLS**: Routes `/i/` path to health server
- **ChatGPT**: External service for Q&A (user subscription)

No additional database schema is required; the feature uses existing tables.

---

## Implementation Files

| File | Purpose |
|------|---------|
| `internal/expandedview/token.go` | HMAC token generation and verification |
| `internal/expandedview/handler.go` | HTTP handler with rate limiting and access control |
| `internal/expandedview/render.go` | HTML rendering and ChatGPT prompt building |
| `internal/expandedview/metrics.go` | Prometheus metrics |
| `internal/expandedview/templates/expanded.html` | Main HTML template |
| `internal/expandedview/templates/error.html` | Error page template |

---

## See Also

- [Source Enrichment](source-enrichment.md) - Evidence retrieval shown in expanded views
- [Corroboration](corroboration.md) - Cluster context shown in expanded views
- [Editor Mode](editor-mode.md) - Digest rendering that includes expansion links
