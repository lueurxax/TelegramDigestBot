package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/time/rate"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
)

// OpenRouter API constants.
const (
	OpenRouterAPIEndpoint = "https://openrouter.ai/api/v1/chat/completions"

	// Model constants.
	ModelMistral7BInstruct = "mistralai/mistral-7b-instruct"

	// Default model for OpenRouter LLM.
	defaultOpenRouterModel = ModelMistral7BInstruct

	// Rate limiter settings.
	openRouterRateLimiterBurst = 5

	// Default timeout for OpenRouter API requests.
	openRouterDefaultTimeout = 60 * time.Second

	// Max tokens defaults.
	openRouterMaxTokensDefault = 4096
	openRouterMaxTokensShort   = 2048
	openRouterMaxTokensTiny    = 1024
	openRouterMaxTokensMicro   = 512
	openRouterMaxTokensNano    = 256

	// Relevance gate default confidence.
	openRouterDefaultConfidence = 0.5

	// Priority constant for OpenRouter.
	PriorityFourthFallback = 5
)

// OpenRouter errors.
var (
	ErrOpenRouterEmptyResponse = errors.New("empty response from OpenRouter")
	ErrOpenRouterAPIFailure    = errors.New("openrouter API error")
)

// openRouterProvider implements the Provider interface for OpenRouter.
type openRouterProvider struct {
	cfg         *config.Config
	httpClient  *http.Client
	logger      *zerolog.Logger
	rateLimiter *rate.Limiter
	promptStore PromptStore
}

// openRouterChatRequest represents the OpenRouter Chat API request (OpenAI-compatible).
type openRouterChatRequest struct {
	Model     string                  `json:"model"`
	Messages  []openRouterChatMessage `json:"messages"`
	MaxTokens int                     `json:"max_tokens,omitempty"`
}

// openRouterChatMessage represents a message in the OpenRouter Chat API.
type openRouterChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openRouterChatResponse represents the OpenRouter Chat API response (OpenAI-compatible).
type openRouterChatResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// openRouterErrorResponse represents the OpenRouter API error response.
type openRouterErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// NewOpenRouterProvider creates a new OpenRouter LLM provider.
func NewOpenRouterProvider(cfg *config.Config, store PromptStore, logger *zerolog.Logger) *openRouterProvider {
	rateLimit := cfg.RateLimitRPS
	if rateLimit == 0 {
		rateLimit = 1
	}

	return &openRouterProvider{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: openRouterDefaultTimeout,
		},
		logger:      logger,
		rateLimiter: rate.NewLimiter(rate.Limit(float64(rateLimit)), openRouterRateLimiterBurst),
		promptStore: store,
	}
}

// Name returns the provider identifier.
func (p *openRouterProvider) Name() ProviderName {
	return ProviderOpenRouter
}

// IsAvailable returns true if the provider is configured and available.
func (p *openRouterProvider) IsAvailable() bool {
	return p.cfg.OpenRouterAPIKey != ""
}

// Priority returns the provider priority.
func (p *openRouterProvider) Priority() int {
	return PriorityFourthFallback
}

// SupportsImageGeneration returns false as OpenRouter doesn't support image generation.
func (p *openRouterProvider) SupportsImageGeneration() bool {
	return false
}

// resolveModel returns the appropriate model name for OpenRouter.
func (p *openRouterProvider) resolveModel(model string) string {
	if model == "" {
		return defaultOpenRouterModel
	}

	// If already an OpenRouter model path, use it directly
	if strings.Contains(model, "/") {
		return model
	}

	// Map other models to OpenRouter equivalents
	switch {
	case strings.Contains(strings.ToLower(model), "mistral"):
		return ModelMistral7BInstruct
	default:
		return ModelMistral7BInstruct
	}
}

// callOpenRouterAPI makes the HTTP request to OpenRouter Chat API.
func (p *openRouterProvider) callOpenRouterAPI(ctx context.Context, prompt, model string, maxTokens int) (openRouterResult, error) {
	resolvedModel := p.resolveModel(model)
	reqBody := openRouterChatRequest{
		Model: resolvedModel,
		Messages: []openRouterChatMessage{
			{Role: "user", Content: prompt},
		},
		MaxTokens: maxTokens,
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(reqBody); err != nil {
		return openRouterResult{}, fmt.Errorf(errFmtMarshalRequest, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, OpenRouterAPIEndpoint, &buf)
	if err != nil {
		return openRouterResult{}, fmt.Errorf(errFmtCreateRequest, err)
	}

	req.Header.Set(headerAuthorization, "Bearer "+p.cfg.OpenRouterAPIKey)
	req.Header.Set(headerContentType, contentTypeJSON)
	req.Header.Set("Referer", "https://github.com/lueurxax/telegram-digest-bot")
	req.Header.Set("X-Title", "telegram-digest-bot")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return openRouterResult{}, fmt.Errorf("openrouter request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return openRouterResult{}, fmt.Errorf(errFmtReadResponse, err)
	}

	if resp.StatusCode != http.StatusOK {
		return openRouterResult{}, p.parseAPIError(body, resp.StatusCode)
	}

	return p.extractResponseText(body)
}

// parseAPIError extracts error details from the API response.
func (p *openRouterProvider) parseAPIError(body []byte, statusCode int) error {
	var errResp openRouterErrorResponse
	if jsonErr := json.Unmarshal(body, &errResp); jsonErr == nil && errResp.Error.Message != "" {
		return fmt.Errorf(errFmtAPIWithMessage, ErrOpenRouterAPIFailure, statusCode, errResp.Error.Message)
	}

	return fmt.Errorf(errFmtAPIStatusOnly, ErrOpenRouterAPIFailure, statusCode)
}

// openRouterResult holds the API response with usage info.
type openRouterResult struct {
	Text             string
	PromptTokens     int
	CompletionTokens int
}

// extractResponseText extracts the text content and usage from OpenRouter response.
func (p *openRouterProvider) extractResponseText(body []byte) (openRouterResult, error) {
	var resp openRouterChatResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return openRouterResult{}, fmt.Errorf(errFmtDecodeResponse, err)
	}

	if len(resp.Choices) == 0 {
		return openRouterResult{}, ErrOpenRouterEmptyResponse
	}

	return openRouterResult{
		Text:             resp.Choices[0].Message.Content,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
	}, nil
}

// ProcessBatch implements Provider interface.
func (p *openRouterProvider) ProcessBatch(ctx context.Context, messages []MessageInput, targetLanguage, model, tone string) ([]BatchResult, error) {
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf(errRateLimiterSimple, err)
	}

	promptContent := buildBatchPromptContent(messages, targetLanguage, tone)
	resolvedModel := p.resolveModel(model)

	result, err := p.callOpenRouterAPI(ctx, promptContent, model, openRouterMaxTokensDefault)
	if err != nil {
		RecordTokenUsage(string(ProviderOpenRouter), resolvedModel, TaskSummarize, 0, 0, false) //nolint:contextcheck // fire-and-forget
		return nil, err
	}

	RecordTokenUsage(string(ProviderOpenRouter), resolvedModel, TaskSummarize, result.PromptTokens, result.CompletionTokens, true) //nolint:contextcheck // fire-and-forget

	return p.parseProcessBatchResponse(result.Text, messages)
}

// parseProcessBatchResponse parses the JSON response from batch processing.
func (p *openRouterProvider) parseProcessBatchResponse(responseText string, messages []MessageInput) ([]BatchResult, error) {
	responseText = extractJSON(responseText)

	var results []BatchResult

	// Try wrapper format first
	var wrapper struct {
		Results []BatchResult `json:"results"`
	}

	if err := json.Unmarshal([]byte(responseText), &wrapper); err == nil && len(wrapper.Results) > 0 {
		results = wrapper.Results
	} else {
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
func (p *openRouterProvider) TranslateText(ctx context.Context, text, targetLanguage, model string) (string, error) {
	if strings.TrimSpace(text) == "" || strings.TrimSpace(targetLanguage) == "" {
		return text, nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := fmt.Sprintf(translatePromptFmt, targetLanguage, text)
	resolvedModel := p.resolveModel(model)

	result, err := p.callOpenRouterAPI(ctx, prompt, model, openRouterMaxTokensShort)
	if err != nil {
		RecordTokenUsage(string(ProviderOpenRouter), resolvedModel, TaskTranslate, 0, 0, false) //nolint:contextcheck // fire-and-forget
		return "", err
	}

	RecordTokenUsage(string(ProviderOpenRouter), resolvedModel, TaskTranslate, result.PromptTokens, result.CompletionTokens, true) //nolint:contextcheck // fire-and-forget

	return strings.TrimSpace(result.Text), nil
}

// CompleteText implements Provider interface.
func (p *openRouterProvider) CompleteText(ctx context.Context, prompt, model string) (string, error) {
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	resolvedModel := p.resolveModel(model)

	result, err := p.callOpenRouterAPI(ctx, prompt, model, openRouterMaxTokensDefault)
	if err != nil {
		RecordTokenUsage(string(ProviderOpenRouter), resolvedModel, TaskComplete, 0, 0, false) //nolint:contextcheck // fire-and-forget
		return "", err
	}

	RecordTokenUsage(string(ProviderOpenRouter), resolvedModel, TaskComplete, result.PromptTokens, result.CompletionTokens, true) //nolint:contextcheck // fire-and-forget

	return strings.TrimSpace(result.Text), nil
}

// GenerateNarrative implements Provider interface.
func (p *openRouterProvider) GenerateNarrative(ctx context.Context, items []domain.Item, targetLanguage, model, tone string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildNarrativePrompt(items, nil, targetLanguage, tone, defaultNarrativePrompt)
	resolvedModel := p.resolveModel(model)

	result, err := p.callOpenRouterAPI(ctx, prompt, model, openRouterMaxTokensDefault)
	if err != nil {
		RecordTokenUsage(string(ProviderOpenRouter), resolvedModel, TaskNarrative, 0, 0, false) //nolint:contextcheck // fire-and-forget
		return "", err
	}

	RecordTokenUsage(string(ProviderOpenRouter), resolvedModel, TaskNarrative, result.PromptTokens, result.CompletionTokens, true) //nolint:contextcheck // fire-and-forget

	return strings.TrimSpace(result.Text), nil
}

// GenerateNarrativeWithEvidence implements Provider interface.
func (p *openRouterProvider) GenerateNarrativeWithEvidence(ctx context.Context, items []domain.Item, evidence ItemEvidence, targetLanguage, model, tone string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildNarrativePrompt(items, evidence, targetLanguage, tone, defaultNarrativePrompt)
	resolvedModel := p.resolveModel(model)

	result, err := p.callOpenRouterAPI(ctx, prompt, model, openRouterMaxTokensDefault)
	if err != nil {
		RecordTokenUsage(string(ProviderOpenRouter), resolvedModel, TaskNarrative, 0, 0, false) //nolint:contextcheck // fire-and-forget
		return "", err
	}

	RecordTokenUsage(string(ProviderOpenRouter), resolvedModel, TaskNarrative, result.PromptTokens, result.CompletionTokens, true) //nolint:contextcheck // fire-and-forget

	return strings.TrimSpace(result.Text), nil
}

// SummarizeCluster implements Provider interface.
func (p *openRouterProvider) SummarizeCluster(ctx context.Context, items []domain.Item, targetLanguage, model, tone string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildClusterSummaryPrompt(items, nil, targetLanguage, tone, defaultClusterSummaryPrompt)
	resolvedModel := p.resolveModel(model)

	result, err := p.callOpenRouterAPI(ctx, prompt, model, openRouterMaxTokensTiny)
	if err != nil {
		RecordTokenUsage(string(ProviderOpenRouter), resolvedModel, TaskCluster, 0, 0, false) //nolint:contextcheck // fire-and-forget
		return "", err
	}

	RecordTokenUsage(string(ProviderOpenRouter), resolvedModel, TaskCluster, result.PromptTokens, result.CompletionTokens, true) //nolint:contextcheck // fire-and-forget

	return strings.TrimSpace(result.Text), nil
}

// SummarizeClusterWithEvidence implements Provider interface.
func (p *openRouterProvider) SummarizeClusterWithEvidence(ctx context.Context, items []domain.Item, evidence ItemEvidence, targetLanguage, model, tone string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildClusterSummaryPrompt(items, evidence, targetLanguage, tone, defaultClusterSummaryPrompt)
	resolvedModel := p.resolveModel(model)

	result, err := p.callOpenRouterAPI(ctx, prompt, model, openRouterMaxTokensTiny)
	if err != nil {
		RecordTokenUsage(string(ProviderOpenRouter), resolvedModel, TaskCluster, 0, 0, false) //nolint:contextcheck // fire-and-forget
		return "", err
	}

	RecordTokenUsage(string(ProviderOpenRouter), resolvedModel, TaskCluster, result.PromptTokens, result.CompletionTokens, true) //nolint:contextcheck // fire-and-forget

	return strings.TrimSpace(result.Text), nil
}

// GenerateClusterTopic implements Provider interface.
func (p *openRouterProvider) GenerateClusterTopic(ctx context.Context, items []domain.Item, targetLanguage, model string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildClusterTopicPrompt(items, targetLanguage, defaultClusterTopicPrompt)
	resolvedModel := p.resolveModel(model)

	result, err := p.callOpenRouterAPI(ctx, prompt, model, openRouterMaxTokensNano)
	if err != nil {
		RecordTokenUsage(string(ProviderOpenRouter), resolvedModel, TaskTopic, 0, 0, false) //nolint:contextcheck // fire-and-forget
		return "", err
	}

	RecordTokenUsage(string(ProviderOpenRouter), resolvedModel, TaskTopic, result.PromptTokens, result.CompletionTokens, true) //nolint:contextcheck // fire-and-forget

	return strings.TrimSpace(result.Text), nil
}

// RelevanceGate implements Provider interface.
//
//nolint:dupl // Provider implementations share similar structure
func (p *openRouterProvider) RelevanceGate(ctx context.Context, text, model, prompt string) (RelevanceGateResult, error) {
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return RelevanceGateResult{}, fmt.Errorf(errRateLimiterSimple, err)
	}

	fullPrompt := fmt.Sprintf(relevanceGateFormat, prompt, text)
	resolvedModel := p.resolveModel(model)

	apiResult, err := p.callOpenRouterAPI(ctx, fullPrompt, model, openRouterMaxTokensMicro)
	if err != nil {
		RecordTokenUsage(string(ProviderOpenRouter), resolvedModel, TaskRelevanceGate, 0, 0, false) //nolint:contextcheck // fire-and-forget
		return RelevanceGateResult{}, err
	}

	RecordTokenUsage(string(ProviderOpenRouter), resolvedModel, TaskRelevanceGate, apiResult.PromptTokens, apiResult.CompletionTokens, true) //nolint:contextcheck // fire-and-forget

	responseText := extractJSON(apiResult.Text)

	var result RelevanceGateResult
	if unmarshalErr := json.Unmarshal([]byte(responseText), &result); unmarshalErr != nil {
		p.logger.Warn().Err(unmarshalErr).Str(logKeyResponse, responseText).Msg(logMsgParseRelevanceGateFail)

		return RelevanceGateResult{
			Decision:   "relevant",
			Confidence: openRouterDefaultConfidence,
			Reason:     "failed to parse response",
		}, nil
	}

	return result, nil
}

// CompressSummariesForCover implements Provider interface.
func (p *openRouterProvider) CompressSummariesForCover(ctx context.Context, summaries []string, model string) ([]string, error) {
	if len(summaries) == 0 {
		return nil, nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildCompressSummariesPrompt(summaries)
	resolvedModel := p.resolveModel(model)

	result, err := p.callOpenRouterAPI(ctx, compressSummariesSystemPrompt+"\n\n"+prompt, model, openRouterMaxTokensTiny)
	if err != nil {
		RecordTokenUsage(string(ProviderOpenRouter), resolvedModel, TaskCompress, 0, 0, false) //nolint:contextcheck // fire-and-forget
		return nil, err
	}

	RecordTokenUsage(string(ProviderOpenRouter), resolvedModel, TaskCompress, result.PromptTokens, result.CompletionTokens, true) //nolint:contextcheck // fire-and-forget

	lines := strings.Split(strings.TrimSpace(result.Text), "\n")

	var compressed []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			compressed = append(compressed, trimmed)
		}
	}

	return compressed, nil
}

// GenerateDigestCover returns an error as OpenRouter doesn't support image generation.
func (p *openRouterProvider) GenerateDigestCover(_ context.Context, _ []string, _ string) ([]byte, error) {
	return nil, ErrNoImageProvider
}

// ExtractBullets extracts key bullet points from a message.
// This is a stub implementation - actual bullet extraction logic will be added later.
func (p *openRouterProvider) ExtractBullets(_ context.Context, input BulletExtractionInput, _, _ string) (BulletExtractionResult, error) {
	// Stub: return the input text as a single bullet with default scores
	return BulletExtractionResult{
		Bullets: []ExtractedBullet{
			{
				Text:            input.Summary,
				RelevanceScore:  fallbackBulletScore,
				ImportanceScore: fallbackBulletScore,
				Topic:           "",
			},
		},
	}, nil
}

// Ensure openRouterProvider implements Provider interface.
var _ Provider = (*openRouterProvider)(nil)
