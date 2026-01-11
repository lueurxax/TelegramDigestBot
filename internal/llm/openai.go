package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog"
	"github.com/sashabaranov/go-openai"
	"golang.org/x/time/rate"

	"github.com/lueurxax/telegram-digest-bot/internal/config"
	"github.com/lueurxax/telegram-digest-bot/internal/db"
)

type openaiClient struct {
	cfg         *config.Config
	client      *openai.Client
	logger      *zerolog.Logger
	rateLimiter *rate.Limiter

	// Circuit breaker state
	consecutiveFailures int
	circuitOpenUntil    time.Time
	mu                  sync.Mutex
}

const (
	circuitBreakerThreshold = 5
	circuitBreakerTimeout   = 1 * time.Minute
)

func NewOpenAI(cfg *config.Config, logger *zerolog.Logger) Client {
	return &openaiClient{
		cfg:         cfg,
		client:      openai.NewClient(cfg.LLMAPIKey),
		logger:      logger,
		rateLimiter: rate.NewLimiter(rate.Limit(float64(cfg.RateLimitRPS)), 5), // User-defined RPS, burst 5
	}
}

func (c *openaiClient) checkCircuit() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if time.Now().Before(c.circuitOpenUntil) {
		return fmt.Errorf("circuit breaker is open until %v", c.circuitOpenUntil)
	}
	return nil
}

func (c *openaiClient) recordSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consecutiveFailures = 0
}

func (c *openaiClient) recordFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consecutiveFailures++
	if c.consecutiveFailures >= circuitBreakerThreshold {
		c.circuitOpenUntil = time.Now().Add(circuitBreakerTimeout)
		c.logger.Warn().
			Int("consecutive_failures", c.consecutiveFailures).
			Time("open_until", c.circuitOpenUntil).
			Msg("Circuit breaker opened")
	}
}

func (c *openaiClient) GetEmbedding(ctx context.Context, text string) ([]float32, error) {
	if err := c.checkCircuit(); err != nil {
		return nil, err
	}
	if err := c.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter error: %w", err)
	}
	resp, err := c.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Input: []string{text},
		Model: openai.SmallEmbedding3,
	})
	if err != nil {
		c.recordFailure()
		return nil, fmt.Errorf("failed to create embeddings: %w", err)
	}
	c.recordSuccess()
	return resp.Data[0].Embedding, nil
}

func (c *openaiClient) ProcessBatch(ctx context.Context, messages []MessageInput, targetLanguage string, model string, tone string) ([]BatchResult, error) {
	langInstruction := ""
	if targetLanguage != "" {
		langInstruction = fmt.Sprintf(" IMPORTANT: Return all topics and summaries in %s language.", targetLanguage)
	}
	if tone != "" {
		langInstruction += fmt.Sprintf(" Tone: %s.", getToneInstruction(tone))
	}

	if model == "" {
		model = c.cfg.LLMModel
	}
	if model == "" {
		model = openai.GPT4oMini
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Summarize and score these %d Telegram messages. Return a JSON object with a 'results' key containing an array of objects. It is CRITICAL that you return exactly %d objects in the 'results' array, one for each message provided, in the same order.%s\n\nEach result object MUST have:\n- index (integer, matching the [ID] below)\n- relevance_score (0-1): How relevant is this to the channel's typical audience?\n  - 0.0-0.3: Off-topic, spam, or personal messages\n  - 0.4-0.6: Tangentially related or routine updates\n  - 0.7-0.9: Directly relevant to channel theme\n  - 1.0: Breaking news or highly significant content\n- importance_score (0-1): How newsworthy or time-sensitive is this?\n  - 0.0-0.3: Opinion, commentary, or evergreen content\n  - 0.4-0.6: Notable but not urgent\n  - 0.7-0.9: Significant development or announcement\n  - 1.0: Breaking news requiring immediate attention\n- topic (string): Assign a topic from this list (or create a specific one if none fit): Technology, Finance, Politics, Sports, Entertainment, Science, Health, Business, World News, Local News, Culture, Education, Humor (for jokes, satire, memes, ironic content).\n  IMPORTANT: Check the \"Channel Description\" if provided. If it mentions satire, parody, fiction, humor, jokes, or similar terms (e.g., \"сатирический вымысел\", \"satirical fiction\", \"parody account\"), you MUST assign topic \"Humor\" regardless of the message content appearing serious.\n- summary (string): A single, self-contained sentence that states the key fact or development. Include WHO, WHAT, and WHY if applicable. Avoid meta-language like \"The article discusses...\". Format summaries using minimal HTML:\n  - Use <b>bold</b> for: company names, person names, products, specific numbers/percentages\n  - Use <i>italic</i> for: direct quotes only\n  - Keep formatting minimal—maximum 3-4 bold elements per summary\n  - Never use <code>, <u>, or other tags in summaries\n  For satirical/parody channels: Prefix summary with [Сатира] or [Satire] to warn readers the content is fictional.\n- language (2-letter code of the source message)\n- source_channel (string): Echo back EXACTLY the \"Source Channel\" name provided for this message. This is CRITICAL for alignment.\n\nExample of GOOD summary: \"<b>Apple</b> announced <b>M4 chip</b> with <b>40%%</b> faster neural engine.\"\nExample of BAD summary: \"The channel posted about technology.\"\n\nCRITICAL: Each message has a clearly marked \">>> MESSAGE TO SUMMARIZE <<<\" section. You MUST summarize ONLY that section, NOT the background context. The \"BACKGROUND CONTEXT\" is provided ONLY to help you understand the channel's tone and theme - DO NOT summarize it.\n\nSome messages may have images or referenced content (links). If an image is provided, analyze it to improve the summary of the MESSAGE section.\n\nWhen \"Referenced Content\" is provided for a message:\n1. Use the linked content as the PRIMARY source for summarization.\n2. The Telegram message may just be commentary - focus on facts from the referenced source.\n3. For Telegram links: consider the source channel and view count for importance.\n4. For web links: use the article title and content for accurate summarization.\n5. Increase importance_score if linked content contains breaking news.\n6. Attribute information to the original source when relevant.\n\nMessages:\n", len(messages), len(messages), langInstruction))

	parts := []openai.ChatMessagePart{
		{
			Type: openai.ChatMessagePartTypeText,
			Text: sb.String(),
		},
	}

	for i, m := range messages {
		textPart := fmt.Sprintf("[%d] ", i)
		if m.ChannelTitle != "" {
			textPart += fmt.Sprintf("(Source Channel: %s) ", m.ChannelTitle)
		}
		if m.ChannelContext != "" {
			textPart += fmt.Sprintf("(Channel Context: %s) ", m.ChannelContext)
		}
		if m.ChannelDescription != "" {
			textPart += fmt.Sprintf("(Channel Description: %s) ", m.ChannelDescription)
		}
		if m.ChannelCategory != "" {
			textPart += fmt.Sprintf("(Channel Category: %s) ", m.ChannelCategory)
		}
		if m.ChannelTone != "" {
			textPart += fmt.Sprintf("(Channel Tone: %s) ", m.ChannelTone)
		}
		if m.ChannelUpdateFreq != "" {
			textPart += fmt.Sprintf("(Channel Frequency: %s) ", m.ChannelUpdateFreq)
		}
		if len(m.Context) > 0 {
			textPart += fmt.Sprintf("[BACKGROUND CONTEXT - DO NOT SUMMARIZE: %s] ", truncate(strings.Join(m.Context, " | "), 500))
		}
		if len(m.ResolvedLinks) > 0 {
			textPart += "[Referenced Content: "
			for _, link := range m.ResolvedLinks {
				if link.LinkType == "telegram" {
					textPart += fmt.Sprintf("[Telegram] From %s: \"%s\" ", link.ChannelTitle, truncate(link.Content, 500))
					if link.Views > 0 {
						textPart += fmt.Sprintf("[%d views] ", link.Views)
					}
				} else {
					textPart += fmt.Sprintf("[Web] %s Title: %s Content: %s ", link.Domain, link.Title, truncate(link.Content, 1000))
				}
			}
			textPart += "] "
		}
		textPart += ">>> MESSAGE TO SUMMARIZE <<< " + m.Text + "\n"

		parts = append(parts, openai.ChatMessagePart{
			Type: openai.ChatMessagePartTypeText,
			Text: textPart,
		})

		if len(m.MediaData) > 0 {
			mimeType := http.DetectContentType(m.MediaData)
			encoded := base64.StdEncoding.EncodeToString(m.MediaData)
			parts = append(parts, openai.ChatMessagePart{
				Type: openai.ChatMessagePartTypeImageURL,
				ImageURL: &openai.ChatMessageImageURL{
					URL:    fmt.Sprintf("data:%s;base64,%s", mimeType, encoded),
					Detail: openai.ImageURLDetailLow,
				},
			})
		}
	}

	if err := c.checkCircuit(); err != nil {
		return nil, err
	}
	if err := c.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter error: %w", err)
	}
	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:         openai.ChatMessageRoleUser,
				MultiContent: parts,
			},
		},
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
	})
	if err != nil {
		c.recordFailure()
		return nil, fmt.Errorf("openai chat completion error: %w", err)
	}
	c.recordSuccess()

	content := resp.Choices[0].Message.Content
	c.logger.Debug().Str("content", content).Msg("LLM response")

	var results []BatchResult
	var wrapper struct {
		Results []BatchResult `json:"results"`
	}

	if err := json.Unmarshal([]byte(content), &wrapper); err == nil && len(wrapper.Results) > 0 {
		results = wrapper.Results
	} else {
		// Fallback 1: try unmarshaling as an array directly
		if err2 := json.Unmarshal([]byte(content), &results); err2 != nil || len(results) == 0 {
			// Fallback 2: try to find any array in the JSON
			var raw map[string]interface{}
			if err := json.Unmarshal([]byte(content), &raw); err == nil {
				for _, v := range raw {
					if arr, ok := v.([]interface{}); ok && len(arr) > 0 {
						arrBytes, _ := json.Marshal(v)
						if err := json.Unmarshal(arrBytes, &results); err == nil && len(results) > 0 {
							break
						}
					}
				}
			}
		}
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("failed to extract any results from LLM response: %s", content)
	}

	// Align results by index and ensure same length as input
	finalResults := make([]BatchResult, len(messages))
	foundIndices := make(map[int]bool)
	allZeroIndex := true

	for _, res := range results {
		if res.Index != 0 {
			allZeroIndex = false
		}
		if res.Index >= 0 && res.Index < len(messages) {
			if !foundIndices[res.Index] {
				finalResults[res.Index] = res
				foundIndices[res.Index] = true
			} else if res.Index != 0 || !allZeroIndex {
				c.logger.Warn().Int("index", res.Index).Msg("LLM returned duplicate index, ignoring")
			}
		}
	}

	// Fallback: if all indices were 0, try to align by source_channel
	if allZeroIndex && len(results) == len(messages) {
		c.logger.Debug().Msg("All LLM results had index 0, attempting source_channel alignment")

		// Build a map of channel title to message indices (there may be multiple messages from same channel)
		channelToIndices := make(map[string][]int)
		for i, m := range messages {
			if m.ChannelTitle != "" {
				channelToIndices[m.ChannelTitle] = append(channelToIndices[m.ChannelTitle], i)
			}
		}

		// Try to match results by source_channel
		aligned := make([]BatchResult, len(messages))
		usedResults := make(map[int]bool)
		matchedByChannel := 0

		for i, m := range messages {
			// Find a result with matching source_channel
			for j, res := range results {
				if usedResults[j] {
					continue
				}
				if res.SourceChannel != "" && res.SourceChannel == m.ChannelTitle {
					aligned[i] = res
					aligned[i].Index = i
					usedResults[j] = true
					matchedByChannel++
					break
				}
			}
		}

		// If we matched most by channel, use the aligned results
		if matchedByChannel > len(messages)/2 {
			// Fill any remaining unmatched slots with unmatched results in order
			unmatchedResultIdx := 0
			for i := range aligned {
				if aligned[i].Summary == "" {
					for unmatchedResultIdx < len(results) {
						if !usedResults[unmatchedResultIdx] {
							c.logger.Warn().
								Int("message_idx", i).
								Str("expected_channel", messages[i].ChannelTitle).
								Str("result_channel", results[unmatchedResultIdx].SourceChannel).
								Msg("Fallback: using unmatched result (potential misalignment)")
							aligned[i] = results[unmatchedResultIdx]
							aligned[i].Index = i
							usedResults[unmatchedResultIdx] = true
							unmatchedResultIdx++
							break
						}
						unmatchedResultIdx++
					}
				}
			}
			c.logger.Info().Int("matched_by_channel", matchedByChannel).Int("total", len(messages)).Msg("Aligned results by source_channel")
			return aligned, nil
		}

		// Source channel matching didn't work well, fall back to order
		c.logger.Warn().Int("matched", matchedByChannel).Int("total", len(messages)).Msg("Source channel matching insufficient, assuming results are in order (potential misalignment)")
		return results, nil
	}

	// For missing indices, log warnings
	for i := 0; i < len(messages); i++ {
		if !foundIndices[i] {
			c.logger.Warn().Int("index", i).Msg("LLM result missing for message index")
		}
	}

	return finalResults, nil
}

func (c *openaiClient) GenerateNarrative(ctx context.Context, items []db.Item, targetLanguage string, model string, tone string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	if model == "" {
		model = c.cfg.LLMModel
	}

	langInstruction := ""
	if targetLanguage != "" {
		langInstruction = fmt.Sprintf(" IMPORTANT: Write the narrative in %s language.", targetLanguage)
	}
	if tone != "" {
		langInstruction += fmt.Sprintf(" Tone: %s.", getToneInstruction(tone))
	}

	var sb strings.Builder
	sb.WriteString(`You are an expert editor-in-chief. Your task is to take the following summaries of news stories and write a single, cohesive, and engaging narrative. Group related stories together, identify broader trends, and provide a high-quality overview.

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
- Use ONLY <b>, <i>, <u> tags. Ensure tags are properly closed.
- Write in engaging journalistic style, not dry bullet points
%s

Summaries:
`)
	for i, item := range items {
		sb.WriteString(fmt.Sprintf("[%d] Topic: %s - %s\n", i+1, item.Topic, item.Summary))
	}

	if err := c.checkCircuit(); err != nil {
		return "", err
	}
	if err := c.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("rate limiter error: %w", err)
	}
	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: fmt.Sprintf(sb.String(), langInstruction),
			},
		},
	})
	if err != nil {
		c.recordFailure()
		return "", fmt.Errorf("openai chat completion error: %w", err)
	}
	c.recordSuccess()

	return resp.Choices[0].Message.Content, nil
}

func (c *openaiClient) SummarizeCluster(ctx context.Context, items []db.Item, targetLanguage string, model string, tone string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	if model == "" {
		model = c.cfg.LLMModel
	}

	langInstruction := ""
	if targetLanguage != "" {
		langInstruction = fmt.Sprintf(" IMPORTANT: Write the summary in %s language.", targetLanguage)
	}
	if tone != "" {
		langInstruction += fmt.Sprintf(" Tone: %s.", getToneInstruction(tone))
	}

	var sb strings.Builder
	sb.WriteString("You are an expert news editor. Your task is to take the following related summaries of a single news story or discussion and merge them into a single, concise, and high-quality summary (1-2 sentences). Eliminate duplicate information and ensure the final summary is coherent and representative of all the provided points. Merge these summaries into one sentence. Preserve HTML formatting from inputs. Ensure the final summary has <b>bold</b> on key entities (1-3 entities max).%s\n\nRelated Summaries:\n")
	for i, item := range items {
		sb.WriteString(fmt.Sprintf("[%d] %s\n", i+1, item.Summary))
	}

	if err := c.checkCircuit(); err != nil {
		return "", err
	}
	if err := c.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("rate limiter error: %w", err)
	}
	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: fmt.Sprintf(sb.String(), langInstruction),
			},
		},
	})
	if err != nil {
		c.recordFailure()
		return "", fmt.Errorf("openai chat completion error: %w", err)
	}
	c.recordSuccess()

	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}

func (c *openaiClient) GenerateClusterTopic(ctx context.Context, items []db.Item, targetLanguage string, model string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	if model == "" {
		model = c.cfg.LLMModel
	}

	langInstruction := ""
	if targetLanguage != "" {
		langInstruction = fmt.Sprintf(" IMPORTANT: Write the topic in %s language.", targetLanguage)
	}

	var sb strings.Builder
	sb.WriteString("You are an expert news editor. Given these related news summaries, generate a very concise (2-4 words) topic label that captures the main theme. Do not use punctuation at the end.%s\n\nSummaries:\n")
	for i, item := range items {
		sb.WriteString(fmt.Sprintf("[%d] %s\n", i+1, item.Summary))
	}

	if err := c.checkCircuit(); err != nil {
		return "", err
	}
	if err := c.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("rate limiter error: %w", err)
	}
	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: fmt.Sprintf(sb.String(), langInstruction),
			},
		},
	})
	if err != nil {
		c.recordFailure()
		return "", fmt.Errorf("openai chat completion error: %w", err)
	}
	c.recordSuccess()

	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}

func getToneInstruction(tone string) string {
	switch strings.ToLower(tone) {
	case "professional":
		return "Write in a formal, journalistic tone."
	case "casual":
		return "Write in a conversational, accessible tone."
	case "brief":
		return "Be extremely concise, telegram-style."
	default:
		return ""
	}
}

func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "..."
}
