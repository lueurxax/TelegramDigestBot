# Pipeline Optimization

This document covers heuristic-based optimizations that improve digest quality without additional LLM calls. These features work alongside the [Content Quality](content-quality.md) system to reduce noise, improve summary clarity, and optimize LLM usage through caching.

## Overview

The pipeline optimization features include:

1. **Pre-LLM Filters** - Language-aware length filtering and boilerplate detection
2. **Link Preview Enrichment** - Use Telegram preview data for short messages
3. **Domain Quality Signals** - Boost/penalize based on domain reputation
4. **Language Detection** - Stable language resolution with source tracking
5. **Same-Channel Dedup** - Stricter deduplication within channels
6. **Summary Post-Processing** - Heuristic cleanup of LLM summaries
7. **Lead Sentence Fallback** - Extract lead sentence when summary is weak
8. **Topic Cleanup** - Normalize and deduplicate cluster topics
9. **Explainability Line** - Show why items were included
10. **Time-to-Digest Tracking** - Monitor content freshness
11. **Summary Caching** - Reuse summaries for duplicate content
12. **Cluster Summary Caching** - Reuse cluster summaries across windows

---

## Pre-LLM Filters

Messages are filtered before LLM processing using language-aware rules.

### Length Filtering

Minimum text length varies by language to account for different word densities:

| Language | Minimum Length |
|----------|----------------|
| Russian (ru) | 20 characters |
| Ukrainian (uk) | 20 characters |
| English (en) | 15 characters |
| Other | 15 characters |

### Boilerplate Detection

The filter removes:

- **Emoji-only messages** - Messages containing only emoji characters
- **CTA prefixes** - Lines starting with promotional phrases:
  - English: `subscribe`, `share this`, `donate`, `support us`
  - Russian: `подписывайтесь`, `поддержите`, `поделитесь`
- **Footer blocks** - Last 2-3 lines containing multiple CTA keywords or URL lists

### Link-Only Handling

Messages with only URLs are **not dropped**. Instead, the system uses link preview data (title/description) as surrogate text for scoring.

### Configuration

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `FILTER_MIN_LENGTH_RU` | int | `20` | Minimum length for Russian text |
| `FILTER_MIN_LENGTH_UK` | int | `20` | Minimum length for Ukrainian text |
| `FILTER_MIN_LENGTH_EN` | int | `15` | Minimum length for English text |

### Implementation

- **File**: `internal/process/filters/heuristics.go`
- Functions: `IsEmojiOnly()`, `IsBoilerplateOnly()`, `StripFooterBoilerplate()`

---

## Link Preview Enrichment

For short messages, Telegram's link preview data supplements the text used for scoring.

### How It Works

1. Extract preview data from Telegram's `webpage` fields (title, description, site name)
2. If message text is short but preview exists, combine them for scoring
3. Store preview-derived snippet for display context

### Behavior

| Scenario | Action |
|----------|--------|
| Short text + preview available | Combine text + preview for scoring |
| Short text + no preview | Score on message text only |
| Long text | Use message text (preview ignored for scoring) |

### Database

The `preview_text` column in the items table stores extracted preview content.

### Implementation

- **File**: `internal/process/pipeline/text_prep.go`
- Functions: `previewTextFromMessage()`, `combinePreviewText()`

---

## Domain Quality Signals

Domain allowlists and denylists adjust item scores based on source reputation.

### Scoring Adjustment

| Domain Status | Score Adjustment |
|---------------|------------------|
| Allowlisted | +0.05 to importance |
| Denylisted | -0.05 to importance |
| Neutral | No adjustment |

### URL Normalization

Before matching, domains are normalized:
- Strip `www.` prefix
- Convert to lowercase
- Extract domain from full URL

### Configuration

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `DOMAIN_ALLOWLIST` | string | (empty) | Comma-separated trusted domains |
| `DOMAIN_DENYLIST` | string | (empty) | Comma-separated low-quality domains |

**Example:**
```bash
DOMAIN_ALLOWLIST=reuters.com,apnews.com,bbc.com
DOMAIN_DENYLIST=clickbait.example,spam.example
```

### Administration

- Lists are maintained by admins via environment or ConfigMap
- If both lists are empty, no domain bias is applied
- Review monthly or when noise patterns are detected

### Implementation

- **File**: `internal/process/pipeline/domain_bias.go`
- Function: `applyDomainBias()`

---

## Language Detection Normalization

Language detection uses a stable precedence to avoid false flips between similar languages (e.g., RU/UK).

### Detection Order

1. **Original text** - Detect from message content
2. **Preview text** - Fall back to link preview if original is unknown
3. **Summary** - Use LLM summary language as last resort

### Source Tracking

The `language_source` column tracks where the language was detected:
- `original` - From message text
- `preview` - From link preview
- `summary` - From LLM-generated summary

### Implementation

- **File**: `internal/process/pipeline/language.go`
- Function: `resolveItemLanguage()` returns `(language, source)`

---

## Same-Channel Deduplication

Stricter deduplication rules apply within the same channel to catch repeated posts.

### Thresholds

| Scope | Similarity Threshold | Time Window |
|-------|---------------------|-------------|
| Same channel | 0.85 | 6 hours (configurable) |
| Cross channel | Standard threshold | 36 hours |

### How It Works

1. For each new item, check for similar items in the same channel within the window
2. Use higher similarity threshold (0.85) for same-channel matches
3. Cross-channel dedup uses standard clustering threshold

### Configuration

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `DEDUP_SAME_CHANNEL_WINDOW_HOURS` | int | `6` | Time window for same-channel dedup |

### Implementation

- **File**: `internal/storage/items.go`
- Method: `FindSimilarItemForChannel(channelID, since, threshold)`

---

## Summary Post-Processing

Heuristic cleanup improves LLM summary quality without additional API calls.

### Processing Steps

1. **Strip prefixes** - Remove boilerplate phrases from summary start
2. **Normalize whitespace** - Collapse multiple spaces, trim
3. **Enforce sentence limit** - Allow max 2 sentences (second must be <80 chars)
4. **Truncate** - Enforce maximum character limit

### Boilerplate Phrases

Configurable per language. Default phrases stripped from start:
- English: "Breaking:", "Update:", "JUST IN:"
- Russian: "Срочно:", "Новость:"

### Configuration

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `SUMMARY_MAX_CHARS` | int | `220` | Maximum summary length |
| `SUMMARY_STRIP_PHRASES_EN` | string | (list) | English phrases to strip |
| `SUMMARY_STRIP_PHRASES_RU` | string | (list) | Russian phrases to strip |

### Implementation

- **File**: `internal/process/pipeline/summary.go`
- Functions: `postProcessSummary()`, `stripSummaryPrefixes()`, `enforceSentenceLimit()`, `truncateSummary()`

---

## Lead Sentence Fallback

When an LLM summary is weak, the system extracts the best lead sentence from the original text.

### Weak Summary Detection

A summary is considered weak if:
- Empty or whitespace only
- Length < 60 characters
- Fewer than 6 tokens (words)

### Sentence Scoring

Sentences are scored by information density signals:

| Signal | Score |
|--------|-------|
| Contains number/date | +2 |
| Contains 2+ consecutive capitalized words | +2 |
| Contains @mention or #hashtag | +1 |
| Contains acronym (2-5 uppercase letters) | +1 |

**Tie-breaker:** Longest sentence under 200 characters wins.

### Implementation

- **File**: `internal/process/pipeline/summary.go`
- Functions: `isWeakSummary()`, `selectLeadSentence()`

---

## Topic Cleanup

Cluster topics are normalized and deduplicated for consistency.

### Normalization Rules

1. Convert to lowercase, trim whitespace
2. Apply synonym mapping (e.g., Ukraine/Украина/Україна → "Ukraine")
3. Convert to title case for display

### Topic Deduplication

Similar topics within a digest window are merged using Jaccard similarity:
- Tokenize topics into word sets
- Merge if Jaccard index ≥ 0.8

### Synonym Map

Common synonyms are configured for cross-language consistency:

```
Ukraine, Украина, Україна → Ukraine
Russia, Россия → Russia
USA, US, United States, США → USA
```

### Implementation

- **File**: `internal/output/digest/clustering.go`
- Functions: `normalizeClusterTopic()`, `canonicalizeTopic()`, `topicsSimilar()`

---

## Explainability Line

Optional metadata showing why each item was included in the digest.

### Format

```
why: rel 0.62 | imp 0.41 | corr 3ch | gate: pass
```

| Field | Description |
|-------|-------------|
| `rel` | Relevance score (0-1) |
| `imp` | Importance score (0-1) |
| `corr` | Number of channels reporting (corroboration) |
| `gate` | Relevance gate status |

### Configuration

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `EXPLAINABILITY_LINE_ENABLED` | bool | `false` | Show explainability line |

**Note:** Keep disabled for public channels to avoid exposing internal metrics.

### Implementation

- **File**: `internal/output/digest/digest_render.go`
- Function: `appendExplainabilityLine()`

---

## Time-to-Digest Tracking

Tracks how long content takes from first observation to digest inclusion.

### Metrics

- **First seen timestamp** - When the message was first observed
- **Digest inclusion timestamp** - When the item appears in a digest
- **Lag** - Time between first seen and inclusion

### Alerting

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `TIME_TO_DIGEST_ALERT_THRESHOLD` | duration | `6h` | Alert if average lag exceeds this |

When average lag exceeds the threshold, a warning is logged with:
- Average lag in seconds
- Number of items in the window
- Configured threshold

### Quality Signals

Low-quality windows are flagged when:
- Fewer than 3 items selected
- Average importance below threshold

### Database

The `first_seen_at` column in the items table tracks initial observation time.

### Implementation

- **File**: `internal/output/digest/digest.go`
- Functions: `recordDigestQuality()`, `calculateDigestTotals()`, `checkLagAlert()`

---

## Summary Caching

LLM summaries are cached and reused for duplicate content to reduce API costs.

### Cache Key

Summaries are keyed by:
- Canonical hash of message content
- Digest language (for translated summaries)

### Cache Entry

| Field | Description |
|-------|-------------|
| `canonical_hash` | Hash of normalized message text |
| `digest_language` | Target language for summary |
| `summary` | Cached summary text |
| `topic` | Cached topic assignment |
| `relevance_score` | Cached relevance score |
| `importance_score` | Cached importance score |

### Invalidation

- TTL: 30 days (configurable)
- Invalidated when prompt version changes

### Implementation

- **Files**: `internal/storage/summary_cache.go`, `internal/process/pipeline/pipeline.go`
- Table: `summary_cache`

---

## Cluster Summary Caching

Cluster summaries are reused across digest windows when content overlap is high.

### Fingerprinting

Cluster fingerprint = SHA256 hash of sorted item IDs

### Reuse Criteria

| Condition | Action |
|-----------|--------|
| Item overlap ≥ 80% | Reuse cached summary |
| Item overlap < 80% | Re-summarize |
| Cache age > 7 days | Re-summarize |

### Cache Entry

| Field | Description |
|-------|-------------|
| `fingerprint` | SHA256 of sorted item IDs |
| `item_ids` | Array of item IDs in cluster |
| `summary` | Cached cluster summary |
| `updated_at` | Last update timestamp |

### Staleness Guard

Cached summaries older than 7 days are not reused, even with high overlap.

### Implementation

- **Files**: `internal/storage/cluster_summary_cache.go`, `internal/output/digest/digest_render.go`
- Table: `cluster_summary_cache`

---

## Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `digest_time_to_digest_seconds` | Histogram | Time from first seen to digest inclusion |
| `digest_average_importance` | Gauge | Average importance in digest window |
| `digest_average_relevance` | Gauge | Average relevance in digest window |
| `digest_ready_items` | Gauge | Items selected for digest |
| `digest_drops_total` | Counter | Dropped messages by reason |

---

## Configuration Summary

| Variable | Default | Feature |
|----------|---------|---------|
| `FILTER_MIN_LENGTH_RU` | `20` | Pre-LLM Filters |
| `FILTER_MIN_LENGTH_UK` | `20` | Pre-LLM Filters |
| `FILTER_MIN_LENGTH_EN` | `15` | Pre-LLM Filters |
| `DOMAIN_ALLOWLIST` | (empty) | Domain Quality |
| `DOMAIN_DENYLIST` | (empty) | Domain Quality |
| `DEDUP_SAME_CHANNEL_WINDOW_HOURS` | `6` | Same-Channel Dedup |
| `SUMMARY_MAX_CHARS` | `220` | Summary Post-Processing |
| `SUMMARY_STRIP_PHRASES_EN` | (list) | Summary Post-Processing |
| `SUMMARY_STRIP_PHRASES_RU` | (list) | Summary Post-Processing |
| `EXPLAINABILITY_LINE_ENABLED` | `false` | Explainability |
| `TIME_TO_DIGEST_ALERT_THRESHOLD` | `6h` | Time-to-Digest |

---

## See Also

- [Content Quality](content-quality.md) - Relevance gates, threshold tuning, clustering
- [Corroboration](corroboration.md) - Multi-source verification and fact-checks
- [Channel Importance](channel-importance-weight.md) - Per-channel weighting
- [Source Enrichment](source-enrichment.md) - External evidence retrieval
