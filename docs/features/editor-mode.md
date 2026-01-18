# Editor Mode & Narrative Rendering

Editor mode transforms the digest from a simple list of summaries into a cohesive, professionally-written narrative. This feature uses an LLM to act as an "editor-in-chief" that groups related stories, identifies trends, and produces engaging prose.

## Overview

| Feature | Setting | Default | Description |
|---------|---------|---------|-------------|
| Editor Mode | `editor_enabled` | off | Generate narrative digest instead of list |
| Tiered Importance | `tiered_importance_enabled` | off | Categorize items by importance level |
| Detailed Items | `editor_detailed_items` | on | Show individual items under sections |
| Consolidated Clusters | `consolidated_clusters_enabled` | off | Merge related clusters |
| Others as Narrative | `others_as_narrative` | off | Summarize low-importance items as prose |

---

## Editor Mode

When enabled, the digest is transformed from a bullet list into a flowing editorial narrative.

### Standard Output (Editor Off)

```
📌 <b>Digest for Jan 18</b>

• <b>Apple</b> announced M4 chip with 40% faster neural engine.
• <b>Tesla</b> shares dropped 5% after earnings miss.
• New study shows coffee improves cognitive function.
```

### Editor Mode Output (Editor On)

```
🔥 <b>Главное</b>
<b>Apple</b> unveiled its next-generation <b>M4 chip</b>, promising
a <b>40%</b> boost to neural engine performance...

📌 <b>Важно</b>
• <b>Tesla</b> shares fell <b>5%</b> following disappointing earnings
• Research confirms cognitive benefits of regular coffee consumption

🔮 <b>Следим за</b>
Expect more details on M4 availability at next week's event.
```

### Configuration

```
/ai editor on
```

---

## Tiered Importance

When enabled, items are categorized into importance tiers and rendered in separate sections.

### Importance Tiers

| Tier | Emoji | Description |
|------|-------|-------------|
| Breaking | 🔥 | Highest importance (score > 0.8) |
| Notable | 📌 | Medium importance (score 0.5-0.8) |
| Also | 📊 | Lower importance (score < 0.5) |

### Section Rendering

Items are grouped by importance tier and rendered under appropriate headers:

- **Breaking news** gets the most prominent placement
- **Notable items** form the middle section
- **Also/Other items** appear at the end (optionally as narrative)

### Configuration

```
/ai tiered on
```

---

## Detailed Items

Controls whether individual items are shown within each tier section.

| Setting | Behavior |
|---------|----------|
| on (default) | Show bullet points for each item |
| off | Show only section-level summary |

### Configuration

```
/ai details on
/ai details off
```

---

## Consolidated Clusters

When enabled, related items that were clustered together are merged into single entries.

### How It Works

1. Items are clustered by semantic similarity during processing
2. When rendering, each cluster becomes a single entry
3. The cluster summary represents all related items
4. Source channels are listed for attribution

### Benefits

- Reduces redundancy when multiple channels cover the same story
- Highlights broader trends rather than individual posts
- Creates a cleaner, more readable digest

### Configuration

```
/ai consolidated on
```

---

## Others as Narrative

The "others" section (lowest importance tier) can be rendered as a prose summary instead of individual bullet points.

### Standard Others Section

```
📊 <b>Также</b>
• Story about local event
• Minor product update
• Industry commentary
```

### Others as Narrative

```
📊 <b>Также</b>
In other developments, a local community event drew attention
while several minor product updates were announced across the
tech industry...
```

### How It Works

1. Collect all items in the "others" tier
2. Send to LLM with cluster summary prompt
3. Generate cohesive 2-3 sentence summary
4. Fall back to bullet list if generation fails

### Configuration

```
/others_narrative on
```

---

## Digest Tone

Controls the writing style of the narrative.

| Tone | Style |
|------|-------|
| professional | Formal, news-style writing |
| casual | Conversational, approachable |
| brief | Minimal, just the facts |

### Configuration

```
/ai tone casual
```

---

## Prompt Customization

The narrative prompt can be customized via database settings.

### Prompt Keys

| Key | Description |
|-----|-------------|
| `prompt:narrative:active` | Active version (e.g., `v1`, `v2`) |
| `prompt:narrative:v1` | Prompt text for version v1 |
| `prompt:cluster_summary:active` | Active cluster summary version |
| `prompt:cluster_summary:v1` | Cluster summary prompt text |

### Default Narrative Prompt

The default prompt instructs the LLM to:
- Write as an expert editor-in-chief
- Group related stories together
- Use HTML formatting for Telegram
- Include emojis for visual scanning
- Keep length to 150-250 words

---

## Implementation Files

| File | Purpose |
|------|---------|
| `internal/core/llm/prompts.go` | Default prompt definitions |
| `internal/core/llm/openai.go` | Narrative generation, cluster summaries |
| `internal/output/digest/digest_render.go` | Tiered rendering, section grouping |
| `internal/output/digest/digest.go` | `renderDetailedItems`, importance categorization |

---

## Quick Reference

| Command | Action |
|---------|--------|
| `/ai editor on` | Enable editor mode |
| `/ai tiered on` | Enable tiered importance |
| `/ai details off` | Hide individual items |
| `/ai consolidated on` | Merge related clusters |
| `/others_narrative on` | Summarize others as prose |
| `/ai tone casual` | Set casual writing style |
| `/settings` | View all current settings |

---

## Best Practices

1. **Start with defaults**: Try editor mode alone before enabling other features
2. **Use tiered for high volume**: Helps organize when many items appear
3. **Consolidated for multi-source**: Works best when tracking many channels on similar topics
4. **Others as narrative**: Reduces noise for low-importance items
5. **Test with preview**: Use `/preview` to see changes before next digest
