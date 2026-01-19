package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog"
	"github.com/sashabaranov/go-openai"
	"golang.org/x/time/rate"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
)

type openaiClient struct {
	cfg         *config.Config
	client      *openai.Client
	logger      *zerolog.Logger
	rateLimiter *rate.Limiter
	promptStore PromptStore

	// Circuit breaker state
	consecutiveFailures int
	circuitOpenUntil    time.Time
	mu                  sync.Mutex
}

// ErrCircuitBreakerOpen indicates the circuit breaker is open.
var ErrCircuitBreakerOpen = errors.New("circuit breaker is open")

// ErrNoResultsExtracted indicates no results could be extracted from LLM response.
var ErrNoResultsExtracted = errors.New("failed to extract any results from LLM response")

// ErrEmptyDALLEResponse indicates DALL-E returned no image data.
var ErrEmptyDALLEResponse = errors.New("empty response from DALL-E")

// ErrEmptyLLMResponse indicates the LLM returned no choices.
var ErrEmptyLLMResponse = errors.New("empty response from LLM")

// ErrUnexpectedStatusCode indicates an unexpected HTTP status code was received.
var ErrUnexpectedStatusCode = errors.New("unexpected status code")

const (
	circuitBreakerThreshold       = 5
	circuitBreakerTimeout         = 1 * time.Minute
	translatePromptTemplate       = "Translate the following text to %s. Preserve HTML tags and return only the translated text."
	coverPromptNarrativeMaxLength = 200
	compressSummariesTemperature  = 0.3

	// Format strings for LLM prompts
	narrativeItemFormat       = "[%d] Topic: %s - %s\n"
	summaryLangInstructionFmt = " IMPORTANT: Write the summary in %s language."
	percentageMultiplier      = 100

	compressSummariesSystemPrompt = `You are a news headline writer. Your task is to compress news summaries into very short English phrases (3-6 words each) suitable for image generation.

Rules:
- Output one phrase per line
- Each phrase should be 3-6 words maximum
- Translate non-English text to English
- Focus on the key subject/event (e.g., "Trump tariff announcement", "Panama Canal dispute", "Tech company merger")
- Remove any HTML tags or formatting
- Be concrete and specific, avoid generic terms like "news" or "update"
- Do not number the phrases`
)

func NewOpenAI(cfg *config.Config, store PromptStore, logger *zerolog.Logger) Client {
	return &openaiClient{
		cfg:         cfg,
		client:      openai.NewClient(cfg.LLMAPIKey),
		logger:      logger,
		rateLimiter: rate.NewLimiter(rate.Limit(float64(cfg.RateLimitRPS)), rateLimiterBurst), // User-defined RPS, burst 5
		promptStore: store,
	}
}

func (c *openaiClient) checkCircuit() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if time.Now().Before(c.circuitOpenUntil) {
		return fmt.Errorf("%w until %v", ErrCircuitBreakerOpen, c.circuitOpenUntil)
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
		return nil, fmt.Errorf(errRateLimiter, err)
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
	langInstruction := c.buildLangInstruction(targetLanguage, tone)
	model = c.resolveModel(model)

	promptTemplate, _ := c.loadPrompt(ctx, promptKeySummarize, defaultSummarizePrompt)
	promptText := applyPromptTokens(promptTemplate, langInstruction, len(messages))
	parts := c.buildMessageParts(messages, promptText)

	if err := c.checkCircuit(); err != nil {
		return nil, err
	}

	if err := c.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf(errRateLimiter, err)
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

		return nil, fmt.Errorf(errOpenAIChatCompletion, err)
	}

	c.recordSuccess()

	content := resp.Choices[0].Message.Content
	c.logger.Debug().Str("content", content).Msg("LLM response")

	results, err := c.parseResponseJSON(content)
	if err != nil {
		return nil, err
	}

	return c.alignBatchResults(results, messages)
}

func (c *openaiClient) TranslateText(ctx context.Context, text string, targetLanguage string, model string) (string, error) {
	if strings.TrimSpace(text) == "" || strings.TrimSpace(targetLanguage) == "" {
		return text, nil
	}

	if err := c.checkCircuit(); err != nil {
		return "", err
	}

	if err := c.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiter, err)
	}

	model = c.resolveModel(model)
	prompt := fmt.Sprintf(translatePromptTemplate, targetLanguage)

	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: prompt + "\n\nText:\n" + text,
			},
		},
	})
	if err != nil {
		c.recordFailure()

		return "", fmt.Errorf(errOpenAIChatCompletion, err)
	}

	c.recordSuccess()

	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}

func (c *openaiClient) CompleteText(ctx context.Context, prompt string, model string) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", nil
	}

	if err := c.checkCircuit(); err != nil {
		return "", err
	}

	if err := c.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiter, err)
	}

	model = c.resolveModel(model)

	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: prompt,
			},
		},
	})
	if err != nil {
		c.recordFailure()

		return "", fmt.Errorf(errOpenAIChatCompletion, err)
	}

	c.recordSuccess()

	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}

func (c *openaiClient) buildLangInstruction(targetLanguage, tone string) string {
	langInstruction := ""

	if targetLanguage != "" {
		langInstruction = fmt.Sprintf(" IMPORTANT: Write all outputs in %s. Translate content if needed and do not mix languages.", targetLanguage)
	}

	if tone != "" {
		langInstruction += fmt.Sprintf(toneFormatString, getToneInstruction(tone))
	}

	return langInstruction
}

func (c *openaiClient) resolveModel(model string) string {
	if model == "" {
		model = c.cfg.LLMModel
	}

	if model == "" {
		model = openai.GPT4oMini
	}

	return model
}

func (c *openaiClient) buildMessageParts(messages []MessageInput, promptText string) []openai.ChatMessagePart {
	parts := []openai.ChatMessagePart{
		{
			Type: openai.ChatMessagePartTypeText,
			Text: promptText,
		},
	}

	for i, m := range messages {
		textPart := c.buildMessageTextPart(i, m)

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

	return parts
}

func (c *openaiClient) buildMessageTextPart(index int, m MessageInput) string {
	textPart := fmt.Sprintf("[%d] ", index)

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
		textPart += fmt.Sprintf("[BACKGROUND CONTEXT - DO NOT SUMMARIZE: %s] ", truncate(strings.Join(m.Context, " | "), truncateLengthShort))
	}

	textPart += c.buildLinkContext(m)
	textPart += ">>> MESSAGE TO SUMMARIZE <<< " + m.Text + "\n"

	return textPart
}

func (c *openaiClient) buildLinkContext(m MessageInput) string {
	textPart := ""

	if len(m.ResolvedLinks) > 0 {
		textPart += c.buildResolvedLinksText(m.ResolvedLinks)
	}

	if len(m.ResolvedLinks) > 0 && len(m.Text) < 100 {
		textPart += "NOTE: The main message is short. Please use the [Referenced Content] above to determine relevance, topic, and summary.\n"
	}

	return textPart
}

func (c *openaiClient) buildResolvedLinksText(links []domain.ResolvedLink) string {
	text := "[Referenced Content: "

	for _, link := range links {
		if link.LinkType == LinkTypeTelegram {
			text += fmt.Sprintf("[Telegram] From %s: \"%s\" ", link.ChannelTitle, truncate(link.Content, truncateLengthShort))

			if link.Views > 0 {
				text += fmt.Sprintf("[%d views] ", link.Views)
			}
		} else {
			limit := c.cfg.LinkSnippetMaxChars
			if limit == 0 {
				limit = truncateLengthLong
			}

			text += fmt.Sprintf("[Web] %s Title: %s Content: %s ", link.Domain, link.Title, truncate(link.Content, limit))
		}
	}

	text += "] "

	return text
}

func (c *openaiClient) parseResponseJSON(content string) ([]BatchResult, error) {
	if results := c.tryParseWrapper(content); len(results) > 0 {
		return results, nil
	}

	if results := c.tryParseArray(content); len(results) > 0 {
		return results, nil
	}

	if results := c.tryFindArrayInJSON(content); len(results) > 0 {
		return results, nil
	}

	return nil, fmt.Errorf("%w: %s", ErrNoResultsExtracted, content)
}

func (c *openaiClient) tryParseWrapper(content string) []BatchResult {
	var wrapper struct {
		Results []BatchResult `json:"results"`
	}

	if err := json.Unmarshal([]byte(content), &wrapper); err == nil {
		return wrapper.Results
	}

	return nil
}

func (c *openaiClient) tryParseArray(content string) []BatchResult {
	var results []BatchResult

	if err := json.Unmarshal([]byte(content), &results); err == nil {
		return results
	}

	return nil
}

func (c *openaiClient) tryFindArrayInJSON(content string) []BatchResult {
	var raw map[string]interface{}

	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return nil
	}

	for _, v := range raw {
		arr, ok := v.([]interface{})
		if !ok || len(arr) == 0 {
			continue
		}

		arrBytes, _ := json.Marshal(v) //nolint:errchkjson // marshaling interface{} from parsed JSON, cannot fail

		var results []BatchResult
		if err := json.Unmarshal(arrBytes, &results); err == nil && len(results) > 0 {
			return results
		}
	}

	return nil
}

func (c *openaiClient) alignBatchResults(results []BatchResult, messages []MessageInput) ([]BatchResult, error) {
	finalResults, foundIndices, allZeroIndex := c.populateResultsByIndex(results, len(messages))

	if allZeroIndex && len(results) == len(messages) {
		return c.alignBySourceChannel(results, messages)
	}

	c.logMissingIndices(foundIndices, len(messages))

	return finalResults, nil
}

func (c *openaiClient) populateResultsByIndex(results []BatchResult, messageCount int) ([]BatchResult, map[int]bool, bool) {
	finalResults := make([]BatchResult, messageCount)
	foundIndices := make(map[int]bool)
	allZeroIndex := true

	for _, res := range results {
		if res.Index != 0 {
			allZeroIndex = false
		}

		if res.Index < 0 || res.Index >= messageCount {
			continue
		}

		if !foundIndices[res.Index] {
			finalResults[res.Index] = res
			foundIndices[res.Index] = true
		} else if res.Index != 0 || !allZeroIndex {
			c.logger.Warn().Int(logKeyIndex, res.Index).Msg("LLM returned duplicate index, ignoring")
		}
	}

	return finalResults, foundIndices, allZeroIndex
}

func (c *openaiClient) logMissingIndices(foundIndices map[int]bool, messageCount int) {
	for i := 0; i < messageCount; i++ {
		if !foundIndices[i] {
			c.logger.Warn().Int(logKeyIndex, i).Msg("LLM result missing for message index")
		}
	}
}

func (c *openaiClient) alignBySourceChannel(results []BatchResult, messages []MessageInput) ([]BatchResult, error) {
	c.logger.Debug().Msg("All LLM results had index 0, attempting source_channel alignment")

	aligned := make([]BatchResult, len(messages))
	usedResults := make(map[int]bool)
	matchedByChannel := 0

	for i, m := range messages {
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

	if matchedByChannel > len(messages)/2 {
		c.fillUnmatchedResults(aligned, results, messages, usedResults)
		c.logger.Info().Int("matched_by_channel", matchedByChannel).Int(logKeyTotal, len(messages)).Msg("Aligned results by source_channel")

		return aligned, nil
	}

	c.logger.Warn().Int("matched", matchedByChannel).Int(logKeyTotal, len(messages)).Msg("Source channel matching insufficient, assuming results are in order (potential misalignment)")

	return results, nil
}

func (c *openaiClient) fillUnmatchedResults(aligned, results []BatchResult, messages []MessageInput, usedResults map[int]bool) {
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
}

func (c *openaiClient) GenerateNarrative(ctx context.Context, items []domain.Item, targetLanguage string, model string, tone string) (string, error) {
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
		langInstruction += fmt.Sprintf(toneFormatString, getToneInstruction(tone))
	}

	var sb strings.Builder

	promptTemplate, _ := c.loadPrompt(ctx, promptKeyNarrative, defaultNarrativePrompt)
	sb.WriteString(applyPromptTokens(promptTemplate, langInstruction, len(items)))

	for i, item := range items {
		sb.WriteString(fmt.Sprintf(narrativeItemFormat, i+1, item.Topic, item.Summary))
	}

	if err := c.checkCircuit(); err != nil {
		return "", err
	}

	if err := c.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiter, err)
	}

	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: sb.String(),
			},
		},
	})
	if err != nil {
		c.recordFailure()

		return "", fmt.Errorf(errOpenAIChatCompletion, err)
	}

	c.recordSuccess()

	return resp.Choices[0].Message.Content, nil
}

func (c *openaiClient) GenerateNarrativeWithEvidence(ctx context.Context, items []domain.Item, evidence ItemEvidence, targetLanguage string, model string, tone string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	if model == "" {
		model = c.cfg.LLMModel
	}

	langInstruction := c.buildLangInstruction(targetLanguage, tone)

	var sb strings.Builder

	promptTemplate, _ := c.loadPrompt(ctx, promptKeyNarrative, defaultNarrativePrompt)
	sb.WriteString(applyPromptTokens(promptTemplate, langInstruction, len(items)))

	for i, item := range items {
		sb.WriteString(fmt.Sprintf(narrativeItemFormat, i+1, item.Topic, item.Summary))

		// Add evidence context if available
		if ev, ok := evidence[item.ID]; ok && len(ev) > 0 {
			sb.WriteString(formatEvidenceContext(ev))
		}
	}

	if err := c.checkCircuit(); err != nil {
		return "", err
	}

	if err := c.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiter, err)
	}

	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: sb.String(),
			},
		},
	})
	if err != nil {
		c.recordFailure()

		return "", fmt.Errorf(errOpenAIChatCompletion, err)
	}

	c.recordSuccess()

	return resp.Choices[0].Message.Content, nil
}

func (c *openaiClient) SummarizeCluster(ctx context.Context, items []domain.Item, targetLanguage string, model string, tone string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	if model == "" {
		model = c.cfg.LLMModel
	}

	langInstruction := ""

	if targetLanguage != "" {
		langInstruction = fmt.Sprintf(summaryLangInstructionFmt, targetLanguage)
	}

	if tone != "" {
		langInstruction += fmt.Sprintf(toneFormatString, getToneInstruction(tone))
	}

	var sb strings.Builder

	promptTemplate, _ := c.loadPrompt(ctx, promptKeyClusterSummary, defaultClusterSummaryPrompt)
	sb.WriteString(applyPromptTokens(promptTemplate, langInstruction, len(items)))

	for i, item := range items {
		sb.WriteString(fmt.Sprintf(indexedItemFormat, i+1, item.Summary))
	}

	if err := c.checkCircuit(); err != nil {
		return "", err
	}

	if err := c.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiter, err)
	}

	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: sb.String(),
			},
		},
	})
	if err != nil {
		c.recordFailure()

		return "", fmt.Errorf(errOpenAIChatCompletion, err)
	}

	c.recordSuccess()

	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}

func (c *openaiClient) SummarizeClusterWithEvidence(ctx context.Context, items []domain.Item, evidence ItemEvidence, targetLanguage string, model string, tone string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	model = c.resolveModel(model)
	langInstruction := c.buildSummaryLangInstruction(targetLanguage, tone)
	prompt := c.buildClusterPromptWithEvidence(ctx, items, evidence, langInstruction)

	return c.executeClusterSummary(ctx, model, prompt)
}

func (c *openaiClient) buildSummaryLangInstruction(targetLanguage, tone string) string {
	langInstruction := ""

	if targetLanguage != "" {
		langInstruction = fmt.Sprintf(summaryLangInstructionFmt, targetLanguage)
	}

	if tone != "" {
		langInstruction += fmt.Sprintf(toneFormatString, getToneInstruction(tone))
	}

	return langInstruction
}

func (c *openaiClient) buildClusterPromptWithEvidence(ctx context.Context, items []domain.Item, evidence ItemEvidence, langInstruction string) string {
	var sb strings.Builder

	promptTemplate, _ := c.loadPrompt(ctx, promptKeyClusterSummary, defaultClusterSummaryPrompt)
	sb.WriteString(applyPromptTokens(promptTemplate, langInstruction, len(items)))

	for i, item := range items {
		sb.WriteString(fmt.Sprintf(indexedItemFormat, i+1, item.Summary))

		if ev, ok := evidence[item.ID]; ok && len(ev) > 0 {
			sb.WriteString(formatEvidenceContext(ev))
		}
	}

	return sb.String()
}

func (c *openaiClient) executeClusterSummary(ctx context.Context, model, prompt string) (string, error) {
	if err := c.checkCircuit(); err != nil {
		return "", err
	}

	if err := c.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiter, err)
	}

	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: prompt,
			},
		},
	})
	if err != nil {
		c.recordFailure()

		return "", fmt.Errorf(errOpenAIChatCompletion, err)
	}

	c.recordSuccess()

	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}

func (c *openaiClient) GenerateClusterTopic(ctx context.Context, items []domain.Item, targetLanguage string, model string) (string, error) {
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

	promptTemplate, _ := c.loadPrompt(ctx, promptKeyClusterTopic, defaultClusterTopicPrompt)
	sb.WriteString(applyPromptTokens(promptTemplate, langInstruction, len(items)))

	for i, item := range items {
		sb.WriteString(fmt.Sprintf(indexedItemFormat, i+1, item.Summary))
	}

	if err := c.checkCircuit(); err != nil {
		return "", err
	}

	if err := c.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiter, err)
	}

	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: sb.String(),
			},
		},
	})
	if err != nil {
		c.recordFailure()

		return "", fmt.Errorf(errOpenAIChatCompletion, err)
	}

	c.recordSuccess()

	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}

func (c *openaiClient) RelevanceGate(ctx context.Context, text string, model string, prompt string) (RelevanceGateResult, error) {
	if err := c.checkCircuit(); err != nil {
		return RelevanceGateResult{}, err
	}

	if err := c.rateLimiter.Wait(ctx); err != nil {
		return RelevanceGateResult{}, fmt.Errorf(errRateLimiter, err)
	}

	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: prompt,
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: text,
			},
		},
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
	})
	if err != nil {
		c.recordFailure()

		return RelevanceGateResult{}, fmt.Errorf(errOpenAIChatCompletion, err)
	}

	c.recordSuccess()

	content := resp.Choices[0].Message.Content

	var result RelevanceGateResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return RelevanceGateResult{}, fmt.Errorf("failed to parse relevance gate response: %w", err)
	}

	return result, nil
}

func getToneInstruction(tone string) string {
	switch strings.ToLower(tone) {
	case ToneProfessional:
		return "Write in a formal, journalistic tone."
	case ToneCasual:
		return "Write in a conversational, accessible tone."
	case ToneBrief:
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

// formatEvidenceContext formats evidence sources for inclusion in LLM prompts.
func formatEvidenceContext(evidence []EvidenceSource) string {
	if len(evidence) == 0 {
		return ""
	}

	var sb strings.Builder

	sb.WriteString("   [Supporting Evidence:")

	for _, ev := range evidence {
		if ev.IsContradiction {
			sb.WriteString(fmt.Sprintf(" ⚠️ CONTRADICTS: %s (%s)", ev.Title, ev.Domain))
		} else {
			sb.WriteString(fmt.Sprintf(" ✓ %s (%s, score: %.0f%%)", ev.Title, ev.Domain, ev.AgreementScore*percentageMultiplier))
		}
	}

	sb.WriteString("]\n")

	// Add Background context if available from sources
	var background []string

	for _, ev := range evidence {
		if ev.Description != "" && !ev.IsContradiction && ev.AgreementScore > 0.7 {
			background = append(background, fmt.Sprintf("- %s: %s", ev.Domain, ev.Description))
		}
	}

	if len(background) > 0 {
		sb.WriteString("   [Background Context:\n")

		for _, b := range background {
			sb.WriteString("    ")
			sb.WriteString(b)
			sb.WriteString("\n")
		}

		sb.WriteString("   ]\n")
	}

	return sb.String()
}

// GenerateDigestCover generates a cover image for the digest using DALL-E.
// CompressSummariesForCover takes raw summaries and compresses them into short English phrases
// suitable for DALL-E image generation prompts.
func (c *openaiClient) CompressSummariesForCover(ctx context.Context, summaries []string) ([]string, error) {
	if len(summaries) == 0 {
		return nil, nil
	}

	if err := c.checkCircuit(); err != nil {
		return nil, err
	}

	if err := c.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf(errRateLimiter, err)
	}

	prompt := buildCompressSummariesPrompt(summaries)

	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: openai.GPT4oMini,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: compressSummariesSystemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: prompt},
		},
		Temperature: compressSummariesTemperature,
	})
	if err != nil {
		c.recordFailure()

		return nil, fmt.Errorf("failed to compress summaries: %w", err)
	}

	c.recordSuccess()

	if len(resp.Choices) == 0 {
		return nil, ErrEmptyLLMResponse
	}

	// Parse the response - each line is a compressed phrase
	result := strings.Split(strings.TrimSpace(resp.Choices[0].Message.Content), "\n")

	// Clean up empty lines
	phrases := make([]string, 0, len(result))

	for _, line := range result {
		line = strings.TrimSpace(line)
		if line != "" {
			phrases = append(phrases, line)
		}
	}

	c.logger.Debug().Strs("phrases", phrases).Msg("Compressed summaries for cover")

	return phrases, nil
}

func (c *openaiClient) GenerateDigestCover(ctx context.Context, topics []string, narrative string) ([]byte, error) {
	if err := c.checkCircuit(); err != nil {
		return nil, err
	}

	if err := c.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf(errRateLimiter, err)
	}

	prompt := buildCoverPrompt(topics, narrative)
	c.logger.Debug().Str("prompt", prompt).Msg("Generating digest cover image")

	resp, err := c.client.CreateImage(ctx, openai.ImageRequest{
		Model:   "gpt-image-1.5",
		Prompt:  prompt,
		Size:    openai.CreateImageSize1024x1024,
		Quality: openai.CreateImageQualityMedium,
		N:       1,
	})
	if err != nil {
		c.recordFailure()

		return nil, fmt.Errorf("failed to generate cover image: %w", err)
	}

	c.recordSuccess()

	if len(resp.Data) == 0 {
		return nil, ErrEmptyDALLEResponse
	}

	// gpt-image-1 returns base64 data directly
	imageData, err := base64.StdEncoding.DecodeString(resp.Data[0].B64JSON)
	if err != nil {
		// If base64 decoding fails, try fetching from URL
		if resp.Data[0].URL == "" {
			return nil, ErrEmptyDALLEResponse
		}

		imageData, err = c.fetchImageFromURL(ctx, resp.Data[0].URL)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch cover image from URL: %w", err)
		}
	}

	c.logger.Info().Int("image_size", len(imageData)).Msg("Generated digest cover image")

	return imageData, nil
}

// fetchImageFromURL downloads an image from the given URL.
func (c *openaiClient) fetchImageFromURL(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %d", ErrUnexpectedStatusCode, resp.StatusCode)
	}

	var buf bytes.Buffer

	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil, fmt.Errorf("failed to read image body: %w", err)
	}

	return buf.Bytes(), nil
}

// buildCoverPrompt creates a DALL-E prompt from digest topics and narrative.
func buildCoverPrompt(topics []string, narrative string) string {
	var sb strings.Builder

	// If we have narrative (actual content summaries), create a content-specific prompt
	if narrative != "" {
		sb.WriteString("Create a symbolic illustration representing these current events: ")
		sb.WriteString(truncate(narrative, coverPromptNarrativeMaxLength))
		sb.WriteString(". ")
		sb.WriteString("Style: editorial illustration, conceptual art, metaphorical imagery. ")
		sb.WriteString("Use symbolic visual elements that represent the subjects (not literal depictions). ")
	} else if len(topics) > 0 {
		// Fallback to topic-based prompt
		sb.WriteString("Create an editorial illustration for a news digest covering: ")
		sb.WriteString(strings.Join(topics, ", "))
		sb.WriteString(". ")
		sb.WriteString("Style: conceptual magazine cover art with symbolic imagery. ")
	} else {
		sb.WriteString("Create an abstract editorial illustration for a news digest. ")
		sb.WriteString("Style: modern conceptual art, magazine cover aesthetic. ")
	}

	sb.WriteString("IMPORTANT: Absolutely no text, letters, words, numbers, or writing of any kind. ")
	sb.WriteString("Clean, professional, visually striking.")

	return sb.String()
}

// buildCompressSummariesPrompt creates a prompt to compress summaries into short English phrases.
func buildCompressSummariesPrompt(summaries []string) string {
	var sb strings.Builder

	sb.WriteString("Compress each of these news summaries into a short English phrase (3-6 words):\n\n")

	for i, summary := range summaries {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, summary))
	}

	return sb.String()
}
