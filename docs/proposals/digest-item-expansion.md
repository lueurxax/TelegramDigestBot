# On-Demand Item Expansion and Q&A

> **Status: IMPLEMENTED** (January 2026)
>
> Add a short link per digest item that opens an expanded view with evidence, context, and optional Q&A.

## Summary
Provide a read-only expansion page for each digest item. The expansion uses existing evidence, link previews, and cluster context to avoid extra LLM cost. Q&A is handled via a deep link into ChatGPT (user subscription) only.

## Goals
- Provide deeper context without increasing baseline LLM usage.
- Enable user-driven Q&A for specific items via ChatGPT.
- Control access via signed tokens with optional admin-only mode.

## Non-Goals
- Running a full chat UI inside the bot.
- Long-term storage of Q&A sessions.

## UX
### Digest Output
- Add a short link or button per item:
  - `More: https://digest.local/i/<token>`
- Narrative sections show numbered links to all underlying items (up to 5).

### Expanded Item View
- Raw message text, summary, scores, topic, and channel.
- Message image (if available).
- Evidence list with agreement scores and matched claims.
- Corroborating channels and cluster context.
- Button: `📱 Ask on iPhone/Mac` (Apple Shortcuts only).

## Design

### Delivery
- Served by the health/metrics HTTP server (port 8080 by default).
- HTML response (simple template) for direct viewing; no separate service.
- Uses existing ingress + TLS; `EXPANDED_VIEW_BASE_URL` must route to the health server's `/i/` path.

### Access Control
- Use signed, time-limited tokens to prevent guessing.
- Token payload includes `item_id`, `user_id`, and `exp`.
- Two access modes controlled by configuration:
  - **Admin-only mode** (`EXPANDED_VIEW_REQUIRE_ADMIN=true`): Only admins can view.
  - **System tokens** (`EXPANDED_VIEW_ALLOW_SYSTEM_TOKENS=true`): Digest-generated links (user_id=0) bypass admin check, allowing digest recipients to view expanded items.
- Links expire after `EXPANDED_VIEW_TTL_HOURS` (default 72).
- Mark pages as `noindex` and block public search engines.
- Token in URL path; avoid logging full URLs and set `Referrer-Policy: no-referrer`.

### Token Format
- `token = base64url(item_id|user_id|exp|sig)`
- `sig = HMAC_SHA256(secret, item_id|user_id|exp)`
- No server-side mapping table required.

### Context Assembly (No New LLM)
- Use existing evidence, link previews, and cluster summary.
- Show claim matches and agreement scores.
- If evidence is missing, show `No evidence available`.

### ChatGPT Prompt
- Build prompt with maximum context:
  - Topic + summary
  - Original Telegram link
  - Full raw message text + preview text
  - Links extracted from message entities and media (URLs referenced in the message)
  - Duplicate/related messages with their links and full text
  - Evidence sources with URLs and descriptions
  - Standard questions for exploration
- Prompt is passed directly to the Apple Shortcuts flow (no copy/paste UI).

### ChatGPT Integration via Apple Shortcuts

Use Apple Shortcuts to send the full prompt to ChatGPT in one tap for iOS/macOS users.

#### Why Apple Shortcuts Only
- **User-Agent detection is unreliable** - Telegram in-app browser, Safari vs WebView, and other edge cases make platform detection error-prone
- **ChatGPT `?q=` parameter limitations** - URL length limits (~2000 chars), doesn't work on mobile app, doesn't work with Custom GPTs
- **Apple Shortcuts provides reliable integration** - Native URL scheme, works across iOS/iPadOS/macOS, integrates with ChatGPT app

#### How It Works
Use the `shortcuts://` URL scheme to trigger a pre-installed shortcut:
```
shortcuts://run-shortcut?name=Ask%20ChatGPT&input=<url_encoded_prompt>
```

**User flow:**
1. **One-time setup:** User installs "Ask ChatGPT" shortcut via iCloud link
2. **Usage:** Click "Ask on iPhone/Mac" button → Shortcuts app opens → runs shortcut → ChatGPT responds

**Shortcut implementation options:**
- Use native ChatGPT app Siri Shortcuts actions (recommended, requires ChatGPT app)
- Call OpenAI API directly within shortcut (requires user's API key)
- Copy to clipboard + open ChatGPT app (simplest fallback)

#### Proposed UI
Show Apple Shortcuts only (iOS/macOS only):
```
┌─────────────────────────────────────────────────────────────┐
│  Deep Dive with ChatGPT                                     │
├─────────────────────────────────────────────────────────────┤
│  [📱 Ask on iPhone/Mac]     ← shortcuts:// URL             │
│     ℹ️ Requires one-time shortcut installation              │
│                                                             │
│  ▸ View prompt                                              │
│  ▸ Install shortcut (iCloud link)                           │
└─────────────────────────────────────────────────────────────┘
```

**Behavior:**
- "Ask on iPhone/Mac" button visible (iOS/macOS only)
- Collapsible "Install shortcut" link for first-time setup

#### Shortcut Resources
- [Share to ChatGPT Shortcut](https://github.com/reorx/Share-to-ChatGPT-Shortcut) - shares text with personalized prompts
- [S-GPT by MacStories](https://www.macstories.net/ios/introducing-s-gpt-a-shortcut-to-connect-openais-chatgpt-with-native-features-of-apples-operating-systems/) - deep native integration
- [RoutineHub Share to ChatGPT](https://routinehub.co/shortcut/14636/) - community shortcut repository
- [Apple Shortcuts URL Scheme Docs](https://support.apple.com/guide/shortcuts/run-a-shortcut-from-a-url-apd624386f42/ios)

#### Configuration
```bash
EXPANDED_CHATGPT_SHORTCUT_ENABLED=true
EXPANDED_CHATGPT_SHORTCUT_NAME=Ask%20ChatGPT
EXPANDED_CHATGPT_SHORTCUT_ICLOUD_URL=https://www.icloud.com/shortcuts/xxx
EXPANDED_SHORTCUT_URL_MAX_CHARS=2000  # URL-safe limit for shortcuts:// scheme
```

#### Prompt Size Strategy
- **Shortcuts URL:** Truncated to `EXPANDED_SHORTCUT_URL_MAX_CHARS` (default 2000) due to URL length limits
- When URL prompt is truncated, append note: "... [Prompt truncated for Shortcuts URL]"

#### Error Handling: Shortcut Not Installed
The `shortcuts://` URL scheme will fail if:
- User hasn't installed the shortcut
- Shortcut was deleted or renamed
- User is on non-Apple platform
- Shortcuts app is restricted (corporate devices, parental controls)

**Expected behavior when shortcut is missing:**
- **iOS/macOS:** System shows "Shortcut not found" alert or silently fails to open
- **Non-Apple platforms:** Link does nothing (no app to handle `shortcuts://` scheme)

**UX mitigation:**
1. Button text clearly indicates requirement: "Ask on iPhone/Mac (requires shortcut)"
2. Prominent "Install shortcut" link shown above or alongside the button
3. Help text: "First time? Install the shortcut, then try again"

**Proposed button behavior:**
```javascript
function askViaShortcut() {
  window.location.href = 'shortcuts://run-shortcut?name=...&input=...';
}
```

#### Implementation Steps
1. Create "Ask ChatGPT" shortcut that accepts text input and sends to ChatGPT app
2. Upload shortcut to iCloud and obtain share link
3. Update HTML template with new buttons and installation link
4. URL-encode prompt text for `shortcuts://` URL (handle length limits gracefully)

#### Future Considerations
- OpenAI acquired Workflow/Shortcuts founders (October 2025) - native `chatgpt://` deep linking may come
- Monitor for official ChatGPT mobile app URL scheme support
- Android users remain on manual copy/paste flow until similar integration available

### Prompt Size Limits
- **Shortcuts URL:** Truncated to `EXPANDED_SHORTCUT_URL_MAX_CHARS` (default 2000) due to URL length limits
- **View prompt:** Shows full prompt without truncation
- When URL truncated, add suffix: "... [Prompt truncated for URL length]"

### HTML Template
- Simple server-rendered template with inline CSS.
- Mobile-first layout with `meta viewport`.
- Use a compact, readable type scale; avoid heavy assets.

### API
- `GET /i/:token` → HTML page.
- Errors:
  - `401` invalid token, non-admin, or system token denied
  - `404` item not found
  - `410` token expired
  - `429` rate limited
- Error UX: minimal HTML page with a short explanation and a "Back to chat" link.
- "Back to chat" uses Telegram deep link (`tg://resolve?domain=<bot_username>`) with fallback to `https://t.me/<bot_username>`.
- Rate limit: IP-based throttling (10 req/min, burst 20) to deter brute-force token guessing.

## Schema Additions
None. Uses existing items, raw_messages, evidence, and cluster tables.

## Configuration
```bash
EXPANDED_VIEW_ENABLED=true
EXPANDED_VIEW_BASE_URL=https://digest.local
EXPANDED_VIEW_SIGNING_SECRET=changeme
EXPANDED_VIEW_TTL_HOURS=72
EXPANDED_VIEW_REQUIRE_ADMIN=true
EXPANDED_VIEW_ALLOW_SYSTEM_TOKENS=true  # Allow digest links to bypass admin check
TELEGRAM_BOT_USERNAME=MyBot  # For error page deep links
```

## Dependencies
- Postgres (items, raw_messages, clusters, evidence tables).
- Health server exposed via ingress/TLS.
- ChatGPT web for Q&A (external).

## Observability
- Counters: `expanded_view_hits_total{status}`, `expanded_view_denied_total{reason}`, `expanded_view_errors_total{type}`.
- Histogram: `expanded_view_latency_seconds`.
- Log token validation failures and missing item IDs (redact full token).

## Data Queries
- Item + raw message (text, preview_text, media_data, entities_json, media_json) by `item_id`.
- Evidence list by `item_id` (top N by agreement).
- Cluster context (cluster ID, topic, member items with full text).

## Rollout
- Ship behind `EXPANDED_VIEW_ENABLED=false`.
- Add K8s secret for `EXPANDED_VIEW_SIGNING_SECRET`.
- Configure ingress to route `/i/` and `/robots.txt` to health server.
- Enable and verify access control and templates.

## Success Criteria
- Users can open expanded context in 1 click.
- No increase in baseline LLM cost.
- Q&A cost handled by the user's ChatGPT subscription.

## Testing Strategy
- Unit: token signing/validation, expiry handling.
- HTTP: 200/401/404/410/429 responses.
- Render: HTML template includes evidence, cluster items, images.
- Prompt: BuildChatGPTPrompt includes all sections with proper truncation.

## Decisions
| Question | Answer |
| --- | --- |
| Should expanded views be indexed or always private? | Not indexed by search engines (noindex headers + robots.txt). Items stored in Postgres only. |
| Should prompts include raw message text or summary+evidence only? | Maximum context: raw text, preview text, extracted links from entities/media, corroboration with full text and links, evidence with URLs, capped by char limits. |
| Should digest links be admin-only? | Configurable. With `EXPANDED_VIEW_ALLOW_SYSTEM_TOKENS=true`, digest links (user_id=0) bypass admin check so recipients can view. |
| How are original links extracted? | Links are extracted from `entities_json` (TextURL entities) and `media_json` (webpage links) using the `linkextract.ExtractAllURLs` function. Up to 10 links are included in the prompt. |
| How to streamline ChatGPT integration? | Apple Shortcuts only (iOS/macOS). No User-Agent detection (unreliable in Telegram WebView). Always show both "Ask on iPhone/Mac" button and "Copy Prompt" fallback. Pre-copy to clipboard before opening shortcut URL as safety net. |
| Should prompts be truncated? | Shortcuts URL truncated to ~2000 chars due to URL limits. Truncated prompts note: "Prompt truncated for Shortcuts URL". |
