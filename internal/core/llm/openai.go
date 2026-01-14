package llm

import (
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

const (
	circuitBreakerThreshold = 5
	circuitBreakerTimeout   = 1 * time.Minute
	translatePromptTemplate = "Translate the following text to %s. Preserve HTML tags and return only the translated text."
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

	if len(m.ResolvedLinks) > 0 {
		textPart += c.buildResolvedLinksText(m.ResolvedLinks)
	}

	textPart += ">>> MESSAGE TO SUMMARIZE <<< " + m.Text + "\n"

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
			text += fmt.Sprintf("[Web] %s Title: %s Content: %s ", link.Domain, link.Title, truncate(link.Content, truncateLengthLong))
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
		sb.WriteString(fmt.Sprintf("[%d] Topic: %s - %s\n", i+1, item.Topic, item.Summary))
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
		langInstruction = fmt.Sprintf(" IMPORTANT: Write the summary in %s language.", targetLanguage)
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
