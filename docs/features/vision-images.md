# Vision & Image Features

The bot provides several image-related capabilities: vision routing for analyzing images in messages, cover image selection for digests, AI-generated covers using DALL-E, and inline images in digest output.

## Overview

| Feature | Setting | Default | Description |
|---------|---------|---------|-------------|
| Vision Routing | `vision_routing_enabled` | off | Route image messages to vision-capable models |
| Cover Image | `digest_cover_image` | on | Include a cover image with digests |
| AI Cover | `digest_ai_cover` | off | Generate covers with DALL-E |
| Inline Images | `digest_inline_images` | off | Show images per item in digest |

---

## Vision Routing

When enabled, messages containing images are automatically routed to a vision-capable model (like GPT-4o) instead of the standard model.

### How It Works

1. Pipeline groups messages by model requirements
2. Messages with `MediaData` are flagged as needing vision
3. Image messages use the primary model configured by `LLM_MODEL`
4. Text-only messages continue using the same model

### Configuration

```
/ai vision on
```

| Setting | Description |
|---------|-------------|
| `vision_routing_enabled` | Enable/disable vision routing |
| `LLM_MODEL` | Primary model for text and vision messages |

### Benefits

- Process image content for better summarization
- Extract text from screenshots and infographics
- Understand visual context in news posts

---

## Digest Cover Images

Digests can include a cover image to make them visually distinctive in the channel.

### Image Selection Hierarchy

1. **AI-generated cover** (if `digest_ai_cover` is on)
2. **Original image** from highest-importance item (if `digest_cover_image` is on)
3. **No image** if both are off

### Original Cover Selection

When AI covers are disabled, the system selects a cover from digest items:

- Picks the image with highest importance score
- Filters by the current time window and threshold
- Falls back to no image if none available

### Configuration

```
/cover_image on
/ai_cover off
```

---

## AI-Generated Covers

When enabled, the bot generates a unique cover image for each digest using DALL-E.

### How It Works

1. Extract topics from items and clusters
2. Compress summaries into short English phrases
3. Build an image prompt describing the digest themes
4. Generate image via DALL-E (model: `gpt-image-1.5`)
5. Fall back to original image selection on failure

### Prompt Construction

The cover prompt is built from:
- **Topics**: Deduplicated topic labels from items
- **Narrative**: Compressed summary phrases from top items

Example prompt style:
```
Create an editorial illustration for a news digest covering: AI, Security, Markets.
Style: conceptual magazine cover art with symbolic imagery.
```

### Configuration

```
/ai_cover on
```

| Setting | Description |
|---------|-------------|
| `digest_ai_cover` | Enable AI cover generation |

### Fallback Behavior

If AI cover generation fails (API error, timeout, etc.):
1. Log warning with error details
2. Fall back to original image selection
3. If no original image available, send digest without cover

---

## Inline Images

When enabled, each digest item is sent with its associated image as a separate message, creating a richer visual experience.

### How It Works

1. Fetch items with their `MediaData`
2. Build `RichDigestContent` with header, items, and footer
3. Send as multiple Telegram messages:
   - Header message (intro text)
   - Each item as photo+caption or text
   - Footer message (links, navigation)

### Message Format

For items with images:
- Photo with caption containing the summary
- Caption limited to Telegram's 1024 character limit

For items without images:
- Regular text message with summary

### Configuration

```
/inline_images on
```

| Setting | Description |
|---------|-------------|
| `digest_inline_images` | Enable inline images in digests |

### Considerations

- Sends multiple messages per digest (one per item with image)
- May hit Telegram rate limits for large digests
- Higher bandwidth usage
- More visually engaging but noisier in chat

---

## Implementation Files

| File | Purpose |
|------|---------|
| `internal/core/llm/openai.go` | `GenerateDigestCover`, `CompressSummariesForCover` |
| `internal/output/digest/digest.go` | `fetchCoverImage`, `postRichDigest` |
| `internal/process/pipeline/pipeline.go` | Vision routing logic |
| `internal/storage/digests.go` | `GetDigestCoverImage`, `GetItemsForWindowWithMedia` |
| `internal/bot/bot.go` | `SendDigestWithImage`, `SendRichDigest` |

---

## Quick Reference

| Command | Action |
|---------|--------|
| `/ai vision on` | Enable vision routing |
| `/ai vision off` | Disable vision routing |
| `/cover_image on` | Enable original cover images |
| `/ai_cover on` | Enable AI-generated covers |
| `/inline_images on` | Enable inline images per item |
| `/settings` | View all current settings |
