package llm

import (
	"context"
	"strconv"
	"strings"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
)

const (
	promptKeySummarize      = "summarize"
	promptKeyNarrative      = "narrative"
	promptKeyClusterSummary = "cluster_summary"
	promptKeyClusterTopic   = "cluster_topic"
	promptDefaultVersion    = "v1"
	promptLangPlaceholder   = "{{LANG_INSTRUCTION}}"
	promptCountPlaceholder  = "{{MESSAGE_COUNT}}"

	// Context types for language instruction.
	contextTypeSummary   = "summary"
	contextTypeNarrative = "narrative"
)

const defaultSummarizePrompt = `You are a news summarizer. Return STRICT JSON ONLY.
Output must be a single JSON object with a "results" array of length {{MESSAGE_COUNT}} (one object per input message, same order).
Use double quotes. No trailing commas. No markdown. No extra keys.

Language requirement: {{LANG_INSTRUCTION}}

Each result object must include:
- index: integer (match the [ID] of the message)
- relevance_score: number (0.0–1.0) — relevance to the channel’s typical audience
  - 0.0–0.3 = off-topic / spam / personal / admin chatter
  - 0.4–0.6 = tangential or routine updates
  - 0.7–0.9 = directly relevant to the channel’s theme
  - 1.0 = breaking or highly significant
- importance_score: number (0.0–1.0) — newsworthiness / time-sensitivity
  - 0.0–0.3 = opinion / evergreen / commentary
  - 0.4–0.6 = notable but not urgent
  - 0.7–0.9 = significant development or announcement
  - 1.0 = breaking news
- topic: string — choose ONLY from: Technology, Finance, Politics, Sports, Entertainment, Science, Health, Business, World News, Local News, Culture, Education, Humor, General.
  - If channel description indicates satire/parody (e.g. "сатирический" or "parody"), ALWAYS use "Humor".
- summary: string — ONE sentence, ≤ 240 chars. State the key fact (who/what/where/when if relevant). Avoid meta-language ("The article discusses..."). 
  - Use minimal HTML only: <b> for up to 3 key entities/numbers, <i> for direct quotes. No other tags, no markdown.
  - If irrelevant/link-only/empty, use an empty string and keep scores ≤ 0.2.
- language: string — 2-letter code of the output language (must match target language).
- source_channel: string — Exactly the "Source Channel" name provided (verbatim).

Important: Each input has a ">>> MESSAGE TO SUMMARIZE <<<" section. Summarize ONLY that section. "BACKGROUND CONTEXT" is for tone only.

If link context is provided:
1. If [PRIMARY ARTICLE] exists, summarize that content as the main source. Use MESSAGE only for context.
2. If only [SUPPLEMENTAL LINK] exists, summarize MESSAGE as primary and use link only to clarify facts.
3. For Telegram links, consider source channel identity and view count for importance. For web links, use article title/content.
4. If you include facts from a link, implicitly credit the source in the summary.

Messages:
`

const defaultNarrativePrompt = `You are an editor-in-chief. Write a single cohesive and engaging news digest from the following summaries. Group related stories, highlight trends, and provide a concise high-level overview.

Language requirement: {{LANG_INSTRUCTION}}

Format (Telegram HTML):
- Begin each section with an emoji and a bolded title (translate the title into the target language).
- Use <b> for section headers and key names/numbers; use <i> for direct quotes.
- Use bullet points (•) for multiple developments.
- Separate sections with blank lines; keep each paragraph 2–3 sentences.
- Aim for ~150–250 words total (adjust slightly for the target language).

Required structure (titles should be translated):
🔥 <b>Main / Главное</b> — 2–3 sentences on the most significant story.
📌 <b>Important / Важно</b> — notable secondary developments (bullets if needed).
📖 <b>Context / Контекст</b> — background context from "Background Context" inputs (omit if none).
🔮 <b>Watch / Следим за</b> — 1–2 sentences on what to watch next.

Rules:
- Every fact must come from the input summaries or background context (no new facts, no speculation).
- Make the digest easy to scan and concise.
- Use active voice and strong verbs.
- Bold key names, organizations, and significant numbers (don’t overuse).
- Use only <b> and <i> tags; ensure all tags are properly closed.

Summaries:
`

const defaultClusterSummaryPrompt = `You are an expert news editor. Merge the following related summaries (same event/topic) into one concise, coherent summary. Eliminate duplicates so all key points appear once.
Prefer ONE sentence; use a second sentence only if absolutely necessary. No bullet points.
Preserve existing HTML formatting from inputs (e.g. keep <b> tags). Ensure at most 3 bolded key entities.{{LANG_INSTRUCTION}}

Related Summaries:
`

const defaultClusterTopicPrompt = `You are an expert news editor. Based on the following related summaries, generate a very short topic label (2–4 words) that captures the main theme. No ending punctuation, no quotes. If a standard topic fits (e.g., Politics, Technology, Finance, World News, Local News, Sports, Science, Health, Culture, Education, Humor, Business), use it; otherwise create a short label in the target language.{{LANG_INSTRUCTION}}

Summaries:
`

func (c *openaiClient) loadPrompt(ctx context.Context, baseKey string, fallback string) (string, string) {
	version := promptDefaultVersion

	if c.promptStore != nil {
		var active string
		if err := c.promptStore.GetSetting(ctx, promptActiveKey(baseKey), &active); err == nil {
			if strings.TrimSpace(active) != "" {
				version = active
			}
		}

		var override string
		if err := c.promptStore.GetSetting(ctx, promptVersionKey(baseKey, version), &override); err == nil {
			if strings.TrimSpace(override) != "" {
				return override, version
			}
		}
	}

	return fallback, version
}

func promptActiveKey(baseKey string) string {
	return "prompt:" + baseKey + ":active"
}

func promptVersionKey(baseKey, version string) string {
	return "prompt:" + baseKey + ":" + version
}

func applyPromptTokens(prompt string, langInstruction string, count int) string {
	withCount := strings.ReplaceAll(prompt, promptCountPlaceholder, strconv.Itoa(count))
	if strings.Contains(withCount, promptLangPlaceholder) {
		return strings.ReplaceAll(withCount, promptLangPlaceholder, langInstruction)
	}

	if langInstruction != "" {
		return strings.TrimSpace(withCount + " " + strings.TrimSpace(langInstruction))
	}

	return withCount
}

// buildNarrativePrompt builds a prompt for narrative generation.
func buildNarrativePrompt(items []domain.Item, evidence ItemEvidence, targetLanguage, tone, promptTemplate string) string {
	langInstruction := buildPromptLangInstruction(targetLanguage, tone, contextTypeNarrative)

	var sb strings.Builder

	sb.WriteString(applyPromptTokens(promptTemplate, langInstruction, len(items)))

	for i, item := range items {
		sb.WriteString("[")
		sb.WriteString(strconv.Itoa(i + 1))
		sb.WriteString("] Topic: ")
		sb.WriteString(item.Topic)
		sb.WriteString(" - ")
		sb.WriteString(item.Summary)
		sb.WriteString("\n")

		// Add evidence context if available
		if evidence != nil {
			if ev, ok := evidence[item.ID]; ok && len(ev) > 0 {
				sb.WriteString(formatEvidenceForPrompt(ev))
			}
		}
	}

	return sb.String()
}

// buildClusterSummaryPrompt builds a prompt for cluster summarization.
func buildClusterSummaryPrompt(items []domain.Item, evidence ItemEvidence, targetLanguage, tone, promptTemplate string) string {
	langInstruction := buildPromptLangInstruction(targetLanguage, tone, contextTypeSummary)

	var sb strings.Builder

	sb.WriteString(applyPromptTokens(promptTemplate, langInstruction, len(items)))

	for i, item := range items {
		sb.WriteString("[")
		sb.WriteString(strconv.Itoa(i + 1))
		sb.WriteString("] ")
		sb.WriteString(item.Summary)
		sb.WriteString("\n")

		// Add evidence context if available
		if evidence != nil {
			if ev, ok := evidence[item.ID]; ok && len(ev) > 0 {
				sb.WriteString(formatEvidenceForPrompt(ev))
			}
		}
	}

	return sb.String()
}

// buildClusterTopicPrompt builds a prompt for cluster topic generation.
func buildClusterTopicPrompt(items []domain.Item, targetLanguage, promptTemplate string) string {
	langInstruction := ""
	if targetLanguage != "" {
		langInstruction = " IMPORTANT: Write the topic in " + targetLanguage + " language."
	}

	var sb strings.Builder

	sb.WriteString(applyPromptTokens(promptTemplate, langInstruction, len(items)))

	for i, item := range items {
		sb.WriteString("[")
		sb.WriteString(strconv.Itoa(i + 1))
		sb.WriteString("] ")
		sb.WriteString(item.Summary)
		sb.WriteString("\n")
	}

	return sb.String()
}

// buildPromptLangInstruction builds a language instruction for prompts.
func buildPromptLangInstruction(targetLanguage, tone, context string) string {
	var sb strings.Builder

	if targetLanguage != "" {
		sb.WriteString(" IMPORTANT: Write the ")
		sb.WriteString(context)
		sb.WriteString(" in ")
		sb.WriteString(targetLanguage)
		sb.WriteString(" language.")
	}

	if tone != "" {
		sb.WriteString(" Tone: ")
		sb.WriteString(getToneInstruction(tone))
	}

	return sb.String()
}

// buildBatchPromptContent builds the prompt content for ProcessBatch operations.
func buildBatchPromptContent(cfg *config.Config, messages []MessageInput, targetLanguage, tone string) string {
	langInstruction := buildLangInstructionSimple(targetLanguage, tone)
	promptTemplate := defaultSummarizePrompt
	promptText := applyPromptTokens(promptTemplate, langInstruction, len(messages))

	var content strings.Builder

	content.WriteString(promptText)
	content.WriteString("\n\n")

	for i, m := range messages {
		content.WriteString("[")
		content.WriteString(strconv.Itoa(i))
		content.WriteString("] ")

		if m.ChannelTitle != "" {
			content.WriteString("(Source: ")
			content.WriteString(m.ChannelTitle)
			content.WriteString(") ")
		}

		if cfg != nil {
			content.WriteString(buildLinkContextString(cfg, m))
		}

		content.WriteString(">>> MESSAGE TO SUMMARIZE <<< ")
		content.WriteString(m.Text)
		content.WriteString("\n\n")
	}

	return content.String()
}

// formatEvidenceForPrompt formats evidence for inclusion in prompts.
func formatEvidenceForPrompt(evidence []EvidenceSource) string {
	if len(evidence) == 0 {
		return ""
	}

	var sb strings.Builder

	sb.WriteString("   [Supporting Evidence:")

	for _, ev := range evidence {
		if ev.IsContradiction {
			sb.WriteString(" ⚠️ CONTRADICTS: ")
			sb.WriteString(ev.Title)
			sb.WriteString(" (")
			sb.WriteString(ev.Domain)
			sb.WriteString(")")
		} else {
			sb.WriteString(" ✓ ")
			sb.WriteString(ev.Title)
			sb.WriteString(" (")
			sb.WriteString(ev.Domain)
			sb.WriteString(")")
		}
	}

	sb.WriteString("]\n")

	return sb.String()
}
