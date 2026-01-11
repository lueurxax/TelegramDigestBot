package llm

import (
	"context"
	"strconv"
	"strings"
)

const (
	promptKeySummarize      = "summarize"
	promptKeyNarrative      = "narrative"
	promptKeyClusterSummary = "cluster_summary"
	promptKeyClusterTopic   = "cluster_topic"
	promptDefaultVersion    = "v1"
	promptLangPlaceholder   = "{{LANG_INSTRUCTION}}"
	promptCountPlaceholder  = "{{MESSAGE_COUNT}}"
)

const defaultSummarizePrompt = `Summarize and score these {{MESSAGE_COUNT}} Telegram messages. Return a JSON object with a 'results' key containing an array of objects. It is CRITICAL that you return exactly {{MESSAGE_COUNT}} objects in the 'results' array, one for each message provided, in the same order.{{LANG_INSTRUCTION}}

Each result object MUST have:
- index (integer, matching the [ID] below)
- relevance_score (0-1): How relevant is this to the channel's typical audience?
  - 0.0-0.3: Off-topic, spam, or personal messages
  - 0.4-0.6: Tangentially related or routine updates
  - 0.7-0.9: Directly relevant to channel theme
  - 1.0: Breaking news or highly significant content
- importance_score (0-1): How newsworthy or time-sensitive is this?
  - 0.0-0.3: Opinion, commentary, or evergreen content
  - 0.4-0.6: Notable but not urgent
  - 0.7-0.9: Significant development or announcement
  - 1.0: Breaking news requiring immediate attention
- topic (string): Assign a topic from this list (or create a specific one if none fit): Technology, Finance, Politics, Sports, Entertainment, Science, Health, Business, World News, Local News, Culture, Education, Humor (for jokes, satire, memes, ironic content).
  IMPORTANT: Check the "Channel Description" if provided. If it mentions satire, parody, fiction, humor, jokes, or similar terms (e.g., "сатирический вымысел", "satirical fiction", "parody account"), you MUST assign topic "Humor" regardless of the message content appearing serious.
- summary (string): A single, self-contained sentence that states the key fact or development. Include WHO, WHAT, and WHY if applicable. Avoid meta-language like "The article discusses...". Format summaries using minimal HTML:
  - Use <b>bold</b> for: company names, person names, products, specific numbers/percentages
  - Use <i>italic</i> for: direct quotes only
  - Keep formatting minimal—maximum 3-4 bold elements per summary
  - Never use <code>, <u>, or other tags in summaries
  For satirical/parody channels: Prefix summary with [Сатира] or [Satire] to warn readers the content is fictional.
- language (2-letter code of the source message)
- source_channel (string): Echo back EXACTLY the "Source Channel" name provided for this message. This is CRITICAL for alignment.

Example of GOOD summary: "<b>Apple</b> announced <b>M4 chip</b> with <b>40%</b> faster neural engine."
Example of BAD summary: "The channel posted about technology."

CRITICAL: Each message has a clearly marked ">>> MESSAGE TO SUMMARIZE <<<" section. You MUST summarize ONLY that section, NOT the background context. The "BACKGROUND CONTEXT" is provided ONLY to help you understand the channel's tone and theme - DO NOT summarize it.

Some messages may have images or referenced content (links). If an image is provided, analyze it to improve the summary of the MESSAGE section.

When "Referenced Content" is provided for a message:
1. Use the linked content as the PRIMARY source for summarization.
2. The Telegram message may just be commentary - focus on facts from the referenced source.
3. For Telegram links: consider the source channel and view count for importance.
4. For web links: use the article title and content for accurate summarization.
5. Increase importance_score if linked content contains breaking news.
6. Attribute information to the original source when relevant.

Messages:
`

const defaultNarrativePrompt = `You are an expert editor-in-chief. Your task is to take the following summaries of news stories and write a single, cohesive, and engaging narrative. Group related stories together, identify broader trends, and provide a high-quality overview.

Format for Telegram using HTML with VISUAL APPEAL:
- Use emojis at the START of section headers for visual scanning
- Use <b>bold</b> for section headers and key entities (names, numbers)
- Use <i>italic</i> for direct quotes and context
- Use bullet points (•) within sections when listing multiple developments
- Separate sections with blank lines for readability
- Keep paragraphs SHORT (2-3 sentences max)
- Total length: 150-250 words

REQUIRED STRUCTURE with emojis:

🔥 <b>Главное</b>
[Most significant story - 2-3 sentences with <b>key entities</b> highlighted. What happened, who is involved, why it matters.]

📌 <b>Важно</b>
[Secondary developments - can use bullet points if multiple stories:]
• First notable story
• Second notable story

🔮 <b>Следим за</b>
[Brief outlook - what to expect next, 1-2 sentences]

RULES:
- Every fact MUST appear in source summaries - no speculation
- Make it scannable - reader should get key info in 10 seconds
- Use dynamic verbs, avoid passive voice
- Bold ALL names, numbers, percentages, money amounts
- Use ONLY <b>, <i>, <u> tags. Ensure tags are properly closed.{{LANG_INSTRUCTION}}

Summaries:
`

const defaultClusterSummaryPrompt = `You are an expert news editor. Your task is to take the following related summaries of a single news story or discussion and merge them into a single, concise, and high-quality summary (1-2 sentences). Eliminate duplicate information and ensure the final summary is coherent and representative of all the provided points. Merge these summaries into one sentence. Preserve HTML formatting from inputs. Ensure the final summary has <b>bold</b> on key entities (1-3 entities max).{{LANG_INSTRUCTION}}

Related Summaries:
`

const defaultClusterTopicPrompt = `You are an expert news editor. Given these related news summaries, generate a very concise (2-4 words) topic label that captures the main theme. Do not use punctuation at the end.{{LANG_INSTRUCTION}}

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
