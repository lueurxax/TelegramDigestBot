package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
)

// Anthropic model constants.
const (
	ModelClaudeHaiku = "claude-haiku-4.5"

	// Default model for Anthropic.
	defaultAnthropicModel = ModelClaudeHaiku

	// Rate limiter settings for Anthropic.
	anthropicRateLimiterBurst = 5

	// Max tokens defaults.
	anthropicMaxTokensDefault = 4096
	anthropicMaxTokensShort   = 2048
	anthropicMaxTokensTiny    = 1024
	anthropicMaxTokensMicro   = 512
	anthropicMaxTokensNano    = 256

	// Relevance gate default confidence.
	anthropicDefaultConfidence = 0.5
)

// anthropicProvider implements the Provider interface for Anthropic Claude.
type anthropicProvider struct {
	cfg         *config.Config
	client      anthropic.Client
	logger      *zerolog.Logger
	rateLimiter *rate.Limiter
	promptStore PromptStore
}

// NewAnthropicProvider creates a new Anthropic LLM provider.
func NewAnthropicProvider(cfg *config.Config, store PromptStore, logger *zerolog.Logger) *anthropicProvider {
	client := anthropic.NewClient(option.WithAPIKey(cfg.AnthropicAPIKey))

	rateLimit := cfg.RateLimitRPS
	if rateLimit == 0 {
		rateLimit = 1
	}

	return &anthropicProvider{
		cfg:         cfg,
		client:      client,
		logger:      logger,
		rateLimiter: rate.NewLimiter(rate.Limit(float64(rateLimit)), anthropicRateLimiterBurst),
		promptStore: store,
	}
}

// Name returns the provider identifier.
func (p *anthropicProvider) Name() ProviderName {
	return ProviderAnthropic
}

// IsAvailable returns true if the provider is configured and available.
func (p *anthropicProvider) IsAvailable() bool {
	return p.cfg.AnthropicAPIKey != ""
}

// Priority returns the provider priority.
func (p *anthropicProvider) Priority() int {
	return PriorityFallback
}

// SupportsImageGeneration returns false as Anthropic doesn't support image generation.
func (p *anthropicProvider) SupportsImageGeneration() bool {
	return false
}

// resolveModel returns the appropriate model name for Anthropic.
func (p *anthropicProvider) resolveModel(model string) string {
	if model == "" {
		return defaultAnthropicModel
	}

	// Map OpenAI model names to Anthropic equivalents
	// All models map to Haiku since it's the only configured Anthropic model
	if strings.HasPrefix(model, modelPrefixClaude) {
		return model
	}

	return ModelClaudeHaiku
}

// completeWithMetrics is a helper that executes an API call and records metrics.
// It handles the common pattern of calling the Anthropic API with metrics tracking.
func (p *anthropicProvider) completeWithMetrics(ctx context.Context, prompt, model, task string, maxTokens int64, errMsg string) (string, error) {
	resolvedModel := anthropic.Model(p.resolveModel(model))

	resp, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     resolvedModel,
		MaxTokens: maxTokens,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		RecordTokenUsage(string(ProviderAnthropic), string(resolvedModel), task, 0, 0, false) //nolint:contextcheck // fire-and-forget

		return "", fmt.Errorf("%s: %w", errMsg, err)
	}

	RecordTokenUsage(string(ProviderAnthropic), string(resolvedModel), task, int(resp.Usage.InputTokens), int(resp.Usage.OutputTokens), true) //nolint:contextcheck // fire-and-forget

	return strings.TrimSpace(extractTextFromResponse(resp)), nil
}

// ProcessBatch implements Provider interface.
func (p *anthropicProvider) ProcessBatch(ctx context.Context, messages []MessageInput, targetLanguage, model, tone string) ([]BatchResult, error) {
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf(errRateLimiterSimple, err)
	}

	langInstruction := buildLangInstructionSimple(targetLanguage, tone)
	promptTemplate := defaultSummarizePrompt
	promptText := applyPromptTokens(promptTemplate, langInstruction, len(messages))

	// Build message content
	var content strings.Builder
	content.WriteString(promptText)
	content.WriteString("\n\n")

	for i, m := range messages {
		content.WriteString(fmt.Sprintf(indexedPrefixFormat, i))

		if m.ChannelTitle != "" {
			content.WriteString(fmt.Sprintf(sourceChannelFormat, m.ChannelTitle))
		}

		content.WriteString(m.Text)
		content.WriteString("\n\n")
	}

	resolvedModel := anthropic.Model(p.resolveModel(model))

	resp, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     resolvedModel,
		MaxTokens: anthropicMaxTokensDefault,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(content.String())),
		},
	})
	if err != nil {
		RecordTokenUsage(string(ProviderAnthropic), string(resolvedModel), TaskSummarize, 0, 0, false) //nolint:contextcheck // fire-and-forget

		return nil, fmt.Errorf("anthropic chat completion: %w", err)
	}

	RecordTokenUsage(string(ProviderAnthropic), string(resolvedModel), TaskSummarize, int(resp.Usage.InputTokens), int(resp.Usage.OutputTokens), true) //nolint:contextcheck // fire-and-forget

	if len(resp.Content) == 0 {
		return nil, ErrEmptyLLMResponse
	}

	responseText := extractTextFromResponse(resp)

	return p.parseProcessBatchResponse(responseText, messages)
}

// parseProcessBatchResponse parses the JSON response from batch processing.
func (p *anthropicProvider) parseProcessBatchResponse(responseText string, messages []MessageInput) ([]BatchResult, error) {
	// Try to extract JSON from the response
	responseText = extractJSON(responseText)

	var results []BatchResult

	// Try wrapper format first
	var wrapper struct {
		Results []BatchResult `json:"results"`
	}

	if err := json.Unmarshal([]byte(responseText), &wrapper); err == nil && len(wrapper.Results) > 0 {
		results = wrapper.Results
	} else {
		// Try array format
		if err := json.Unmarshal([]byte(responseText), &results); err != nil {
			return nil, fmt.Errorf(errParseResponse, err)
		}
	}

	// Fill in source channel from messages
	for i := range results {
		if i < len(messages) {
			results[i].SourceChannel = messages[i].ChannelTitle
		}
	}

	return results, nil
}

// TranslateText implements Provider interface.
func (p *anthropicProvider) TranslateText(ctx context.Context, text, targetLanguage, model string) (string, error) {
	if strings.TrimSpace(text) == "" || strings.TrimSpace(targetLanguage) == "" {
		return text, nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := fmt.Sprintf(translatePromptFmt, targetLanguage, text)
	resolvedModel := anthropic.Model(p.resolveModel(model))

	resp, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     resolvedModel,
		MaxTokens: anthropicMaxTokensShort,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		RecordTokenUsage(string(ProviderAnthropic), string(resolvedModel), TaskTranslate, 0, 0, false) //nolint:contextcheck // fire-and-forget

		return "", fmt.Errorf("anthropic translation: %w", err)
	}

	RecordTokenUsage(string(ProviderAnthropic), string(resolvedModel), TaskTranslate, int(resp.Usage.InputTokens), int(resp.Usage.OutputTokens), true) //nolint:contextcheck // fire-and-forget

	return strings.TrimSpace(extractTextFromResponse(resp)), nil
}

// CompleteText implements Provider interface.
func (p *anthropicProvider) CompleteText(ctx context.Context, prompt, model string) (string, error) {
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	return p.completeWithMetrics(ctx, prompt, model, TaskComplete, anthropicMaxTokensDefault, "anthropic completion")
}

// GenerateNarrative implements Provider interface.
func (p *anthropicProvider) GenerateNarrative(ctx context.Context, items []domain.Item, targetLanguage, model, tone string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildNarrativePrompt(items, nil, targetLanguage, tone, defaultNarrativePrompt)

	return p.completeWithMetrics(ctx, prompt, model, TaskNarrative, anthropicMaxTokensDefault, "anthropic narrative")
}

// GenerateNarrativeWithEvidence implements Provider interface.
func (p *anthropicProvider) GenerateNarrativeWithEvidence(ctx context.Context, items []domain.Item, evidence ItemEvidence, targetLanguage, model, tone string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildNarrativePrompt(items, evidence, targetLanguage, tone, defaultNarrativePrompt)

	return p.completeWithMetrics(ctx, prompt, model, TaskNarrative, anthropicMaxTokensDefault, "anthropic narrative with evidence")
}

// SummarizeCluster implements Provider interface.
func (p *anthropicProvider) SummarizeCluster(ctx context.Context, items []domain.Item, targetLanguage, model, tone string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildClusterSummaryPrompt(items, nil, targetLanguage, tone, defaultClusterSummaryPrompt)

	return p.completeWithMetrics(ctx, prompt, model, TaskCluster, anthropicMaxTokensTiny, "anthropic cluster summary")
}

// SummarizeClusterWithEvidence implements Provider interface.
func (p *anthropicProvider) SummarizeClusterWithEvidence(ctx context.Context, items []domain.Item, evidence ItemEvidence, targetLanguage, model, tone string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildClusterSummaryPrompt(items, evidence, targetLanguage, tone, defaultClusterSummaryPrompt)

	return p.completeWithMetrics(ctx, prompt, model, TaskCluster, anthropicMaxTokensTiny, "anthropic cluster summary with evidence")
}

// GenerateClusterTopic implements Provider interface.
func (p *anthropicProvider) GenerateClusterTopic(ctx context.Context, items []domain.Item, targetLanguage, model string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildClusterTopicPrompt(items, targetLanguage, defaultClusterTopicPrompt)

	return p.completeWithMetrics(ctx, prompt, model, TaskTopic, anthropicMaxTokensNano, "anthropic cluster topic")
}

// RelevanceGate implements Provider interface.
func (p *anthropicProvider) RelevanceGate(ctx context.Context, text, model, prompt string) (RelevanceGateResult, error) {
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return RelevanceGateResult{}, fmt.Errorf(errRateLimiterSimple, err)
	}

	fullPrompt := fmt.Sprintf(relevanceGateFormat, prompt, text)
	resolvedModel := anthropic.Model(p.resolveModel(model))

	resp, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     resolvedModel,
		MaxTokens: anthropicMaxTokensMicro,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(fullPrompt)),
		},
	})
	if err != nil {
		RecordTokenUsage(string(ProviderAnthropic), string(resolvedModel), TaskRelevanceGate, 0, 0, false) //nolint:contextcheck // fire-and-forget

		return RelevanceGateResult{}, fmt.Errorf("anthropic relevance gate: %w", err)
	}

	RecordTokenUsage(string(ProviderAnthropic), string(resolvedModel), TaskRelevanceGate, int(resp.Usage.InputTokens), int(resp.Usage.OutputTokens), true) //nolint:contextcheck // fire-and-forget

	responseText := extractJSON(extractTextFromResponse(resp))

	var result RelevanceGateResult
	if unmarshalErr := json.Unmarshal([]byte(responseText), &result); unmarshalErr != nil {
		// Default to relevant if parsing fails - log and return fallback
		p.logger.Warn().Err(unmarshalErr).Str(logKeyResponse, responseText).Msg(logMsgParseRelevanceGateFail)

		return RelevanceGateResult{
			Decision:   "relevant",
			Confidence: anthropicDefaultConfidence,
			Reason:     "failed to parse response",
		}, nil
	}

	return result, nil
}

// CompressSummariesForCover implements Provider interface.
func (p *anthropicProvider) CompressSummariesForCover(ctx context.Context, summaries []string, model string) ([]string, error) {
	if len(summaries) == 0 {
		return nil, nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildCompressSummariesPrompt(summaries)

	resolvedModel := anthropic.Model(p.resolveModel(model))

	resp, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     resolvedModel,
		MaxTokens: anthropicMaxTokensTiny,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(compressSummariesSystemPrompt + "\n\n" + prompt)),
		},
	})
	if err != nil {
		RecordTokenUsage(string(ProviderAnthropic), string(resolvedModel), TaskCompress, 0, 0, false) //nolint:contextcheck // fire-and-forget

		return nil, fmt.Errorf("anthropic compress summaries: %w", err)
	}

	RecordTokenUsage(string(ProviderAnthropic), string(resolvedModel), TaskCompress, int(resp.Usage.InputTokens), int(resp.Usage.OutputTokens), true) //nolint:contextcheck // fire-and-forget

	responseText := extractTextFromResponse(resp)
	lines := strings.Split(strings.TrimSpace(responseText), "\n")

	var compressed []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			compressed = append(compressed, trimmed)
		}
	}

	return compressed, nil
}

// GenerateDigestCover returns an error as Anthropic doesn't support image generation.
func (p *anthropicProvider) GenerateDigestCover(_ context.Context, _ []string, _ string) ([]byte, error) {
	return nil, ErrNoImageProvider
}

// extractTextFromResponse extracts text content from Anthropic response.
func extractTextFromResponse(resp *anthropic.Message) string {
	var result strings.Builder

	for _, block := range resp.Content {
		if block.Type == contentTypeText {
			result.WriteString(block.Text)
		}
	}

	return result.String()
}

// extractJSON tries to extract JSON from a response that might have extra text.
func extractJSON(text string) string {
	// Look for JSON object
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")

	if start != -1 && end != -1 && end > start {
		return text[start : end+1]
	}

	// Look for JSON array
	start = strings.Index(text, "[")
	end = strings.LastIndex(text, "]")

	if start != -1 && end != -1 && end > start {
		return text[start : end+1]
	}

	return text
}

// buildLangInstructionSimple builds a simple language instruction.
func buildLangInstructionSimple(targetLanguage, tone string) string {
	if targetLanguage == "" && tone == "" {
		return ""
	}

	var sb strings.Builder

	if targetLanguage != "" {
		sb.WriteString(fmt.Sprintf(" IMPORTANT: Write all outputs in %s language.", targetLanguage))
	}

	if tone != "" {
		sb.WriteString(fmt.Sprintf(" Tone: %s.", tone))
	}

	return sb.String()
}

// Ensure anthropicProvider implements Provider interface.
var _ Provider = (*anthropicProvider)(nil)
