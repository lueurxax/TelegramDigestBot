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

// Cohere API constants.
const (
	CohereAPIEndpoint = "https://api.cohere.ai/v2/chat"

	// Model constants (use dated versions as base models were deprecated Sept 2025).
	ModelCommandR     = "command-r-08-2024"
	ModelCommandRPlus = "command-r-plus-08-2024"

	// Default model for Cohere LLM.
	defaultCohereModel = ModelCommandR

	// Rate limiter settings.
	cohereRateLimiterBurst = 5

	// Default timeout for Cohere API requests.
	cohereDefaultTimeout = 60 * time.Second

	// Max tokens defaults.
	cohereMaxTokensDefault = 4096
	cohereMaxTokensShort   = 2048
	cohereMaxTokensTiny    = 1024
	cohereMaxTokensMicro   = 512
	cohereMaxTokensNano    = 256

	// Relevance gate default confidence.
	cohereDefaultConfidence = 0.5

	// Finish reason indicating truncation due to token limit.
	cohereFinishReasonMaxTokens = "MAX_TOKENS"
)

// Cohere errors.
var (
	ErrCohereEmptyResponse = errors.New("empty response from Cohere")
	ErrCohereAPIFailure    = errors.New("cohere API error")
)

// cohereProvider implements the Provider interface for Cohere.
type cohereProvider struct {
	cfg           *config.Config
	httpClient    *http.Client
	logger        *zerolog.Logger
	rateLimiter   *rate.Limiter
	promptStore   PromptStore
	usageRecorder UsageRecorder
}

// cohereChatRequest represents the Cohere Chat API request.
type cohereChatRequest struct {
	Model     string              `json:"model"`
	Messages  []cohereChatMessage `json:"messages"`
	MaxTokens int                 `json:"max_tokens,omitempty"`
}

// cohereChatMessage represents a message in the Cohere Chat API.
type cohereChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// cohereChatResponse represents the Cohere Chat API response.
type cohereChatResponse struct {
	ID      string `json:"id"`
	Message struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
	FinishReason string `json:"finish_reason"`
	Usage        struct {
		BilledUnits struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"billed_units"`
		Tokens struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"tokens"`
	} `json:"usage"`
}

// cohereErrorResponse represents the Cohere API error response.
type cohereErrorResponse struct {
	Message string `json:"message"`
}

// NewCohereProvider creates a new Cohere LLM provider.
func NewCohereProvider(cfg *config.Config, store PromptStore, recorder UsageRecorder, logger *zerolog.Logger) *cohereProvider {
	rateLimit := cfg.RateLimitRPS
	if rateLimit == 0 {
		rateLimit = 1
	}

	return &cohereProvider{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: cohereDefaultTimeout,
		},
		logger:        logger,
		rateLimiter:   rate.NewLimiter(rate.Limit(float64(rateLimit)), cohereRateLimiterBurst),
		promptStore:   store,
		usageRecorder: recorder,
	}
}

// Name returns the provider identifier.
func (p *cohereProvider) Name() ProviderName {
	return ProviderCohere
}

// IsAvailable returns true if the provider is configured and available.
func (p *cohereProvider) IsAvailable() bool {
	return p.cfg.CohereAPIKey != ""
}

// Priority returns the provider priority.
func (p *cohereProvider) Priority() int {
	return PriorityThirdFallback
}

// SupportsImageGeneration returns false as Cohere doesn't support image generation.
func (p *cohereProvider) SupportsImageGeneration() bool {
	return false
}

// resolveModel returns the appropriate model name for Cohere.
func (p *cohereProvider) resolveModel(model string) string {
	if model == "" {
		return defaultCohereModel
	}

	// If already a Cohere model, use it directly
	if strings.HasPrefix(model, "command") {
		return model
	}

	// Map other models to Cohere equivalents
	switch {
	case strings.Contains(strings.ToLower(model), modelPrefixGPT4) ||
		strings.Contains(strings.ToLower(model), modelPrefixGPT5):
		return ModelCommandRPlus
	default:
		return ModelCommandR
	}
}

// callCohereAPI makes the HTTP request to Cohere Chat API.
func (p *cohereProvider) callCohereAPI(ctx context.Context, prompt, model string, maxTokens int) (cohereResult, error) {
	reqBody := cohereChatRequest{
		Model: p.resolveModel(model),
		Messages: []cohereChatMessage{
			{Role: "user", Content: prompt},
		},
		MaxTokens: maxTokens,
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(reqBody); err != nil {
		return cohereResult{}, fmt.Errorf(errFmtMarshalRequest, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, CohereAPIEndpoint, &buf)
	if err != nil {
		return cohereResult{}, fmt.Errorf(errFmtCreateRequest, err)
	}

	req.Header.Set(headerAuthorization, "Bearer "+p.cfg.CohereAPIKey)
	req.Header.Set(headerContentType, contentTypeJSON)
	req.Header.Set("Accept", contentTypeJSON)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return cohereResult{}, fmt.Errorf("cohere request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return cohereResult{}, fmt.Errorf(errFmtReadResponse, err)
	}

	if resp.StatusCode != http.StatusOK {
		return cohereResult{}, p.parseAPIError(body, resp.StatusCode)
	}

	return p.extractResponseText(body)
}

// parseAPIError extracts error details from the API response.
func (p *cohereProvider) parseAPIError(body []byte, statusCode int) error {
	var errResp cohereErrorResponse
	if jsonErr := json.Unmarshal(body, &errResp); jsonErr == nil && errResp.Message != "" {
		return fmt.Errorf(errFmtAPIWithMessage, ErrCohereAPIFailure, statusCode, errResp.Message)
	}

	return fmt.Errorf(errFmtAPIStatusOnly, ErrCohereAPIFailure, statusCode)
}

// cohereResult holds the API response with usage info.
type cohereResult struct {
	Text             string
	PromptTokens     int
	CompletionTokens int
	FinishReason     string
}

// extractResponseText extracts the text content and usage from Cohere response.
func (p *cohereProvider) extractResponseText(body []byte) (cohereResult, error) {
	var resp cohereChatResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return cohereResult{}, fmt.Errorf(errFmtDecodeResponse, err)
	}

	if len(resp.Message.Content) == 0 {
		return cohereResult{}, ErrCohereEmptyResponse
	}

	var result strings.Builder

	for _, content := range resp.Message.Content {
		if content.Type == contentTypeText {
			result.WriteString(content.Text)
		}
	}

	return cohereResult{
		Text:             result.String(),
		PromptTokens:     resp.Usage.Tokens.InputTokens,
		CompletionTokens: resp.Usage.Tokens.OutputTokens,
		FinishReason:     resp.FinishReason,
	}, nil
}

// logTruncationWarning logs a warning if the response was truncated due to max_tokens limit.
func (p *cohereProvider) logTruncationWarning(result cohereResult, task string, maxTokens int) {
	if result.FinishReason == cohereFinishReasonMaxTokens {
		p.logger.Warn().
			Str(logKeyTask, task).
			Int(logKeyMaxTokens, maxTokens).
			Int(logKeyOutputTokens, result.CompletionTokens).
			Msg(logMsgTruncated)
	}
}

// ProcessBatch implements Provider interface.
func (p *cohereProvider) ProcessBatch(ctx context.Context, messages []MessageInput, targetLanguage, model, tone string) ([]BatchResult, error) {
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf(errRateLimiterSimple, err)
	}

	promptContent := buildBatchPromptContent(p.cfg, messages, targetLanguage, tone)
	resolvedModel := p.resolveModel(model)

	result, err := p.callCohereAPI(ctx, promptContent, model, cohereMaxTokensDefault)
	if err != nil {
		p.usageRecorder.RecordTokenUsage(string(ProviderCohere), resolvedModel, TaskSummarize, 0, 0, false)

		return nil, err
	}

	p.usageRecorder.RecordTokenUsage(string(ProviderCohere), resolvedModel, TaskSummarize, result.PromptTokens, result.CompletionTokens, true)

	return p.parseProcessBatchResponse(result.Text, messages)
}

// parseProcessBatchResponse parses the JSON response from batch processing.
func (p *cohereProvider) parseProcessBatchResponse(responseText string, messages []MessageInput) ([]BatchResult, error) {
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
func (p *cohereProvider) TranslateText(ctx context.Context, text, targetLanguage, model string) (string, error) {
	if strings.TrimSpace(text) == "" || strings.TrimSpace(targetLanguage) == "" {
		return text, nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := fmt.Sprintf(translatePromptFmt, targetLanguage, targetLanguage, text)
	resolvedModel := p.resolveModel(model)

	result, err := p.callCohereAPI(ctx, prompt, model, cohereMaxTokensShort)
	if err != nil {
		p.usageRecorder.RecordTokenUsage(string(ProviderCohere), resolvedModel, TaskTranslate, 0, 0, false)

		return "", err
	}

	p.usageRecorder.RecordTokenUsage(string(ProviderCohere), resolvedModel, TaskTranslate, result.PromptTokens, result.CompletionTokens, true)

	return strings.TrimSpace(result.Text), nil
}

// CompleteText implements Provider interface.
func (p *cohereProvider) CompleteText(ctx context.Context, prompt, model string) (string, error) {
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	resolvedModel := p.resolveModel(model)

	result, err := p.callCohereAPI(ctx, prompt, model, cohereMaxTokensDefault)
	if err != nil {
		p.usageRecorder.RecordTokenUsage(string(ProviderCohere), resolvedModel, TaskComplete, 0, 0, false)

		return "", err
	}

	p.usageRecorder.RecordTokenUsage(string(ProviderCohere), resolvedModel, TaskComplete, result.PromptTokens, result.CompletionTokens, true)

	return strings.TrimSpace(result.Text), nil
}

// GenerateNarrative implements Provider interface.
func (p *cohereProvider) GenerateNarrative(ctx context.Context, items []domain.Item, targetLanguage, model, tone string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildNarrativePrompt(items, nil, targetLanguage, tone, defaultNarrativePrompt)
	resolvedModel := p.resolveModel(model)

	result, err := p.callCohereAPI(ctx, prompt, model, cohereMaxTokensDefault)
	if err != nil {
		p.usageRecorder.RecordTokenUsage(string(ProviderCohere), resolvedModel, TaskNarrative, 0, 0, false)

		return "", err
	}

	p.usageRecorder.RecordTokenUsage(string(ProviderCohere), resolvedModel, TaskNarrative, result.PromptTokens, result.CompletionTokens, true)

	return strings.TrimSpace(result.Text), nil
}

// GenerateNarrativeWithEvidence implements Provider interface.
func (p *cohereProvider) GenerateNarrativeWithEvidence(ctx context.Context, items []domain.Item, evidence ItemEvidence, targetLanguage, model, tone string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildNarrativePrompt(items, evidence, targetLanguage, tone, defaultNarrativePrompt)
	resolvedModel := p.resolveModel(model)

	result, err := p.callCohereAPI(ctx, prompt, model, cohereMaxTokensDefault)
	if err != nil {
		p.usageRecorder.RecordTokenUsage(string(ProviderCohere), resolvedModel, TaskNarrative, 0, 0, false)

		return "", err
	}

	p.usageRecorder.RecordTokenUsage(string(ProviderCohere), resolvedModel, TaskNarrative, result.PromptTokens, result.CompletionTokens, true)

	return strings.TrimSpace(result.Text), nil
}

// SummarizeCluster implements Provider interface.
func (p *cohereProvider) SummarizeCluster(ctx context.Context, items []domain.Item, targetLanguage, model, tone string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildClusterSummaryPrompt(items, nil, targetLanguage, tone, defaultClusterSummaryPrompt)
	resolvedModel := p.resolveModel(model)

	result, err := p.callCohereAPI(ctx, prompt, model, cohereMaxTokensShort)
	if err != nil {
		p.usageRecorder.RecordTokenUsage(string(ProviderCohere), resolvedModel, TaskCluster, 0, 0, false)

		return "", err
	}

	p.usageRecorder.RecordTokenUsage(string(ProviderCohere), resolvedModel, TaskCluster, result.PromptTokens, result.CompletionTokens, true)
	p.logTruncationWarning(result, TaskCluster, cohereMaxTokensShort)

	return strings.TrimSpace(result.Text), nil
}

// SummarizeClusterWithEvidence implements Provider interface.
//
//nolint:dupl // mirrored implementation across providers
func (p *cohereProvider) SummarizeClusterWithEvidence(ctx context.Context, items []domain.Item, evidence ItemEvidence, targetLanguage, model, tone string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildClusterSummaryPrompt(items, evidence, targetLanguage, tone, defaultClusterSummaryPrompt)
	resolvedModel := p.resolveModel(model)

	result, err := p.callCohereAPI(ctx, prompt, model, cohereMaxTokensShort)
	if err != nil {
		p.usageRecorder.RecordTokenUsage(string(ProviderCohere), resolvedModel, TaskCluster, 0, 0, false)

		return "", err
	}

	p.usageRecorder.RecordTokenUsage(string(ProviderCohere), resolvedModel, TaskCluster, result.PromptTokens, result.CompletionTokens, true)
	p.logTruncationWarning(result, TaskCluster, cohereMaxTokensShort)

	return strings.TrimSpace(result.Text), nil
}

// GenerateClusterTopic implements Provider interface.
func (p *cohereProvider) GenerateClusterTopic(ctx context.Context, items []domain.Item, targetLanguage, model string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildClusterTopicPrompt(items, targetLanguage, defaultClusterTopicPrompt)
	resolvedModel := p.resolveModel(model)

	result, err := p.callCohereAPI(ctx, prompt, model, cohereMaxTokensNano)
	if err != nil {
		p.usageRecorder.RecordTokenUsage(string(ProviderCohere), resolvedModel, TaskTopic, 0, 0, false)

		return "", err
	}

	p.usageRecorder.RecordTokenUsage(string(ProviderCohere), resolvedModel, TaskTopic, result.PromptTokens, result.CompletionTokens, true)

	return strings.TrimSpace(result.Text), nil
}

// RelevanceGate implements Provider interface.
//

func (p *cohereProvider) RelevanceGate(ctx context.Context, text, model, prompt string) (RelevanceGateResult, error) {
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return RelevanceGateResult{}, fmt.Errorf(errRateLimiterSimple, err)
	}

	fullPrompt := fmt.Sprintf(relevanceGateFormat, prompt, text)
	resolvedModel := p.resolveModel(model)

	helper := &relevanceGateHelper{
		providerName:      ProviderCohere,
		usageRecorder:     p.usageRecorder,
		logger:            p.logger,
		defaultConfidence: cohereDefaultConfidence,
	}

	return helper.executeRelevanceGate(resolvedModel, func() (apiCallResult, error) {
		result, err := p.callCohereAPI(ctx, fullPrompt, model, cohereMaxTokensMicro)
		return apiCallResult(result), err
	})
}

// CompressSummariesForCover implements Provider interface.
func (p *cohereProvider) CompressSummariesForCover(ctx context.Context, summaries []string, model string) ([]string, error) {
	if len(summaries) == 0 {
		return nil, nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildCompressSummariesPrompt(summaries)
	resolvedModel := p.resolveModel(model)

	helper := &compressHelper{
		providerName:  ProviderCohere,
		usageRecorder: p.usageRecorder,
	}

	return helper.executeCompress(resolvedModel, func() (apiCallResult, error) {
		result, err := p.callCohereAPI(ctx, compressSummariesSystemPrompt+"\n\n"+prompt, model, cohereMaxTokensTiny)
		return apiCallResult(result), err
	})
}

// GenerateDigestCover returns an error as Cohere doesn't support image generation.
func (p *cohereProvider) GenerateDigestCover(_ context.Context, _ []string, _ string) ([]byte, error) {
	return nil, ErrNoImageProvider
}

// ExtractBullets extracts key bullet points from a message.
func (p *cohereProvider) ExtractBullets(ctx context.Context, input BulletExtractionInput, targetLanguage, model string) (BulletExtractionResult, error) {
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return BulletExtractionResult{}, fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildBulletExtractionPrompt(input, targetLanguage)
	resolvedModel := p.resolveModel(model)

	helper := &bulletExtractionHelper{
		providerName:  ProviderCohere,
		usageRecorder: p.usageRecorder,
		logger:        p.logger,
	}

	return helper.extractBullets(ctx, input, targetLanguage, resolvedModel, func() (apiCallResult, error) {
		result, err := p.callCohereAPI(ctx, prompt, model, cohereMaxTokensTiny)
		return apiCallResult(result), err
	})
}

// Ensure cohereProvider implements Provider interface.
var _ Provider = (*cohereProvider)(nil)
