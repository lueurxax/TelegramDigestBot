# Link Enrichment

Link enrichment pulls context from URLs found in Telegram messages and feeds that content into summarization. It helps the model write a better summary when the message is short or only contains a headline + link.

## How it works
1. The pipeline extracts up to N URLs from each message.
2. Each URL is resolved and fetched.
3. The resolver extracts readable content using RSS/Atom (if the URL is a feed), JSON-LD/OpenGraph metadata, and readability as a fallback.
4. The extracted content is cached and passed into the LLM prompt alongside the original message.

If link enrichment is disabled or a URL fetch fails, the summary is generated from the message text only.

## Admin controls (bot commands)
Enable or disable link enrichment:
- `/config links on`
- `/config links off`

Limit the number of URLs per message:
- `/config maxlinks 3` (range 1-5)

Set cache TTL for fetched links:
- `/config link_cache 24h` (accepts `12h`, `24h`, `7d`)

## Environment defaults
Set these in your `.env` or Kubernetes config:
- `LINK_ENRICHMENT_ENABLED` (default: `false`)
- `MAX_LINKS_PER_MESSAGE` (default: `3`)
- `LINK_CACHE_TTL` (default: `24h`)
- `TG_LINK_CACHE_TTL` (default: `1h`) for Telegram-specific link resolution

## Notes for annotation
The annotation UI shows the raw message text plus the generated summary. It does not display the fetched link content, even when link enrichment is enabled.
