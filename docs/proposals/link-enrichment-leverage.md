# Link-Aware Processing Enhancements

> **Status:** Proposed (January 2026)

## Summary
Use resolved link content to improve existing pipeline steps without adding new UI. The goal is to make summaries, relevance, topics, dedup, and fact-checking more accurate when Telegram messages are short or link-only.

## Goals
- Increase relevance/topic accuracy for link-heavy posts.
- Improve semantic deduplication across channels with different message text.
- Generate stronger fact-check and enrichment queries using link context.
- Keep behavior unchanged when link enrichment is disabled or no links resolve.

## Non-Goals
- Replacing the LLM prompt structure or digest rendering.
- Adding new admin commands or UI in v1.
- Crawling external sites beyond the links in messages.

## Current State
Link enrichment fetches and resolves URL content and passes it to the LLM. This context is only used in summarization; other pipeline steps rely on message text.

## Proposal
### 1) Relevance scoring uses link context (when present)
- If a message has resolved link content, append a short extracted snippet to the relevance prompt.
- Use a strict max token budget (e.g., 400-600 tokens) to avoid overwhelming the prompt.
- Fallback to message-only behavior when no links resolve.

### 2) Topic assignment uses link context
- For topic detection, include link snippets only if the raw message text is < 120 characters or lacks named entities.
- This avoids diluting topics with long articles when the message itself is already descriptive.

### 3) Semantic deduplication uses link-aware embeddings
- If link content exists and the raw message is short (< 200 chars), compute embeddings over: `message + link snippet`.
- For longer messages, use the link title/domain as additional context but prioritize message text.
- Store these as the primary embeddings for dedup checks.
- This helps detect duplicates across channels that paste different summary lines but link to the same article.

### 4) Enrichment query generation uses link context
- Query generator should pull keywords/entities from resolved link content when the message is short or the LLM summary is vague.
- Enrichment worker will fetch cached links for the ItemID to enrich the query generation context.

### 5) Fact-check claim extraction uses link content
- If a message is short, extract the claim from link text (specifically the headline or lead sentence) instead of message text.
- Use a minimal sentence selection rule (top 1-2 factual sentences from `ResolvedLink.Content`).

## Technical Implementation Notes
- **Pipeline Reordering**: Link resolution must be moved from the end of `prepareCandidates` to the beginning, before `generateEmbeddingIfNeeded`.
- **Conditional Resolution**: To avoid excessive IO, only resolve links for messages that pass basic hash/filter checks and require link context for further processing (based on `LINK_ENRICHMENT_SCOPE`).
- **Integration Points**: 
  - For **relevance**, the `RelevanceGate` caller in `internal/process/pipeline/pipeline.go` must pass the augmented text.
  - For **topic generation**, the clustering logic must use augmented summaries when available.
  - The `buildMessageTextPart` in `internal/core/llm/openai.go` should be updated to respect `LINK_SNIPPET_MAX_CHARS` and explicitly instruct the LLM to use "Referenced Content" for relevance/topic if the main message is sparse.
- **Cache Dependency**: Downstream workers (enrichment, fact-check) will rely on the `LinkCache` to retrieve resolved link content. To facilitate this, `EnrichmentQueueItem` will be updated to include `RawMessageID` (fetched via join during `ClaimNextEnrichment`), allowing workers to call `GetLinksForMessage`.

## Guardrails
- Only use link content if:
  - Extracted content length >= `LINK_MIN_WORDS` (default: 80)
  - Domain is not in denylist
- LLM summary is considered **vague** if it is < 100 characters or fails to extract at least one named entity (Person, Org, or Loc).
- Use a single, trimmed snippet rather than full article text (target 400-600 tokens).

## Configuration
Add a single scope setting to reduce complexity:
- `LINK_ENRICHMENT_SCOPE=summary,relevance,topic,dedup,queries,factcheck`
  - Default: `summary` (existing behavior).
  - Precedence: This setting is ignored if `LINK_ENRICHMENT_ENABLED` is `false`. It defines which pipeline stages *attempt* to pull from `LinkCache`. Individual channel settings (`/config links`) still control whether links are resolved in the first place.

Optional helper settings:
- `LINK_MIN_WORDS=80`
- `LINK_SNIPPET_MAX_CHARS=1200`
- `LINK_EMBEDDING_MAX_MSG_LEN=200` (threshold for using link content in embeddings)

## Rollout Plan
Implement all scope items in a single release with the guardrails enabled by default.

## Success Metrics
- ≥15% reduction in “irrelevant” ratings for link-only posts.
- ≥10% increase in topic accuracy (manual sampling).
- ≥20% increase in dedup hits for multi-channel news links.

## Risks & Mitigations
- **Risk:** Link content dominates prompt → inaccurate relevance.
  - Mitigation: strict snippet limits and short-message gating.
- **Risk:** Noisy domains pollute dedup.
  - Mitigation: denylist + minimum word count.
