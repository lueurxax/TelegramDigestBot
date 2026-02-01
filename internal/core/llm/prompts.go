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

const defaultSummarizePrompt = `Summarize these {{MESSAGE_COUNT}} Telegram messages and return a JSON object with a 'results' array of length {{MESSAGE_COUNT}} (one object per input message, in the same order).

Each result object should include:
- index: integer (match the [ID] of the message)
- relevance_score: number (0.0–1.0) — How relevant is this message to the channel’s typical audience?
  - 0.0–0.3 = off-topic, spam, or personal content
  - 0.4–0.6 = tangentially related or routine updates
  - 0.7–0.9 = directly relevant to the channel’s theme
  - 1.0 = breaking news or highly significant content
- importance_score: number (0.0–1.0) — How newsworthy or time-sensitive is this message?
  - 0.0–0.3 = opinion, commentary, or evergreen content
  - 0.4–0.6 = notable but not urgent
  - 0.7–0.9 = significant development or announcement
  - 1.0 = breaking news requiring immediate attention
- topic: string — A topic label for the message. Use one from this list (or create a specific one if none fit): Technology, Finance, Politics, Sports, Entertainment, Science, Health, Business, World News, Local News, Culture, Education, Humor. (If the channel’s description indicates satire or parody — e.g. contains "сатирический" or "parody" — then use "Humor" as the topic regardless of the message content.)
- summary: string — One sentence stating the key fact or development (include who, what, and why if relevant). Avoid meta-language like "The article discusses...". Use minimal HTML: <b> for names, organizations, and important numbers (limit ~3 bolded terms per summary); <i> for direct quotes. Do not use other tags (no <code>, <u>, etc.). For satirical/parody channels, prefix the summary with [Сатира] or [Satire] to indicate fictional content.
- language: string — 2-letter code of the message’s language (e.g. "en", "ru").
- source_channel: string — Exactly the "Source Channel" name provided for this message (copy it verbatim).

Important: Each input will have a ">>> MESSAGE TO SUMMARIZE <<<" section. Only summarize that section. DO NOT summarize any "BACKGROUND CONTEXT" (which is provided only to help you understand the channel’s tone).

If link context is provided:
1. If a [PRIMARY ARTICLE] section exists, summarize that content as the main source. Use MESSAGE only for context.
2. If only a [SUPPLEMENTAL LINK] section exists, summarize MESSAGE as primary and use the link only to clarify or add facts.
3. For Telegram links, consider the source channel identity and view count when assessing importance. For web links, use article title/content.
4. If you include facts from a link, mention or credit the original source as appropriate.

Messages:
`

const defaultNarrativePrompt = `You are an editor-in-chief. Write a single cohesive and engaging news digest from the following summaries. Group related stories, highlight trends, and provide a concise high-level overview.

Format (Telegram HTML):
- Begin each section with an emoji and a bolded title.
- Use <b> for section headers and for key names or numbers; use <i> for direct quotes.
- Use bullet points (•) to list multiple developments in a section.
- Separate sections with blank lines; keep each paragraph 2–3 sentences.
- Aim for a total length of about 150–250 words.

Required structure and sections:

🔥 <b>Главное</b> – 2–3 sentences on the most significant story (what happened, who is involved, why it matters).

📌 <b>Важно</b> – notable secondary developments (use bullet points if more than one):
• First additional key update or story.
• Second additional key update.

📖 <b>Контекст</b> – background context or explanation (from provided "Background Context" inputs) to explain why events are happening or historical context. (Skip this section if no background info is provided.)

🔮 <b>Следим за</b> – 1–2 sentences about what to watch for next or upcoming developments.

Rules:
- Every fact must come from the input summaries or background context (no new information or speculation).
- Make the digest easy to scan; the reader should grasp key points quickly.
- Use active voice and strong verbs (avoid passive constructions).
- Bold all names, organizations, and significant numbers or percentages.
- Use only <b>, <i>, and <u> HTML tags, and ensure all tags are properly closed.{{LANG_INSTRUCTION}}

Summaries:
`

const defaultClusterSummaryPrompt = `You are an expert news editor. Merge the following related summaries (all about one event or topic) into one concise, coherent summary. Eliminate any duplicate information so that the final result covers all key points without repetition. Try to express this in a **single sentence** (use a second sentence only if absolutely necessary). Preserve any HTML formatting from the inputs (e.g. keep <b> tags on names). Ensure the final summary has at most 3 bolded key entities.{{LANG_INSTRUCTION}}

Related Summaries:
`

const defaultClusterTopicPrompt = `You are an expert news editor. Based on the following related summaries, generate a very short topic label (around 2–4 words) that encapsulates the main theme of these stories. It should read like a brief headline or category, with no ending punctuation.{{LANG_INSTRUCTION}}

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
