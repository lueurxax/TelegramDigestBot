package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/google/generative-ai-go/genai"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
	"google.golang.org/api/option"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
)

// Google model constants.
const (
	// ModelGeminiFlashLite is the cheapest/fastest Google model.
	ModelGeminiFlashLite = "gemini-2.0-flash-lite"

	// Default model for Google (use cheapest available).
	defaultGoogleModel = ModelGeminiFlashLite

	// Rate limiter settings for Google.
	googleRateLimiterBurst = 5

	// Relevance gate default confidence.
	googleDefaultConfidence = 0.5
)

// sanitizeUTF8 removes or replaces invalid UTF-8 sequences from a string.
// Google's protobuf API requires valid UTF-8, and crawled content may contain invalid bytes.
func sanitizeUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}

	// Build a new string with only valid UTF-8 runes
	var builder strings.Builder
	builder.Grow(len(s))

	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			// Invalid byte sequence - replace with replacement character
			builder.WriteRune(utf8.RuneError)

			i++
		} else {
			builder.WriteRune(r)

			i += size
		}
	}

	return builder.String()
}

// googleProvider implements the Provider interface for Google Gemini.
// It supports automatic switching from free tier to paid tier on rate limit errors.
type googleProvider struct {
	cfg           *config.Config
	clientFree    *genai.Client
	clientPaid    *genai.Client
	logger        *zerolog.Logger
	rateLimiter   *rate.Limiter
	promptStore   PromptStore
	usageRecorder UsageRecorder

	mu         sync.RWMutex
	usePaidKey bool
}

// NewGoogleProvider creates a new Google Gemini LLM provider.
// It supports automatic switching from free tier (GOOGLE_API_KEY) to paid tier (GOOGLE_API_KEY_PAID).
func NewGoogleProvider(ctx context.Context, cfg *config.Config, store PromptStore, recorder UsageRecorder, logger *zerolog.Logger) (*googleProvider, error) {
	// Create free tier client (required)
	clientFree, err := genai.NewClient(ctx, option.WithAPIKey(cfg.GoogleAPIKey))
	if err != nil {
		return nil, fmt.Errorf("creating google genai client (free): %w", err)
	}

	// Create paid tier client (optional)
	var clientPaid *genai.Client

	if cfg.GoogleAPIKeyPaid != "" {
		clientPaid, err = genai.NewClient(ctx, option.WithAPIKey(cfg.GoogleAPIKeyPaid))
		if err != nil {
			// Close free client before returning error
			_ = clientFree.Close()

			return nil, fmt.Errorf("creating google genai client (paid): %w", err)
		}

		logger.Info().Msg("Google LLM provider initialized with free and paid tier keys")
	} else {
		logger.Info().Msg("Google LLM provider initialized with free tier key only")
	}

	rateLimit := cfg.RateLimitRPS
	if rateLimit == 0 {
		rateLimit = 1
	}

	return &googleProvider{
		cfg:           cfg,
		clientFree:    clientFree,
		clientPaid:    clientPaid,
		logger:        logger,
		rateLimiter:   rate.NewLimiter(rate.Limit(float64(rateLimit)), googleRateLimiterBurst),
		promptStore:   store,
		usageRecorder: recorder,
		usePaidKey:    false,
	}, nil
}

// Close closes the Google clients.
func (p *googleProvider) Close() error {
	var errs []error

	if p.clientFree != nil {
		if err := p.clientFree.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing google genai client (free): %w", err))
		}
	}

	if p.clientPaid != nil {
		if err := p.clientPaid.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing google genai client (paid): %w", err))
		}
	}

	if len(errs) > 0 {
		return errs[0]
	}

	return nil
}

// getClient returns the appropriate client based on current tier.
func (p *googleProvider) getClient() *genai.Client {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.usePaidKey && p.clientPaid != nil {
		return p.clientPaid
	}

	return p.clientFree
}

// switchToPaid switches to the paid tier if available.
func (p *googleProvider) switchToPaid() bool {
	if p.clientPaid == nil {
		return false
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.usePaidKey {
		p.usePaidKey = true
		p.logger.Warn().Msg("Google LLM: switching to paid tier due to rate limit")

		return true
	}

	return false
}

// isRateLimitError checks if the error is a rate limit error.
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	return strings.Contains(errStr, "429") ||
		strings.Contains(errStr, "RESOURCE_EXHAUSTED") ||
		strings.Contains(errStr, "quota") ||
		strings.Contains(errStr, "rate limit")
}

// generateContent wraps genModel.GenerateContent with automatic retry on rate limit.
// If the free tier is rate limited, it switches to paid tier and retries.
func (p *googleProvider) generateContent(ctx context.Context, model string, parts ...genai.Part) (*genai.GenerateContentResponse, error) {
	client := p.getClient()
	genModel := client.GenerativeModel(p.resolveModel(model))

	resp, err := genModel.GenerateContent(ctx, parts...)
	if err != nil && isRateLimitError(err) {
		// Try switching to paid tier
		if p.switchToPaid() {
			// Retry with paid client
			client = p.getClient()
			genModel = client.GenerativeModel(p.resolveModel(model))

			retryResp, retryErr := genModel.GenerateContent(ctx, parts...)
			if retryErr != nil {
				return nil, fmt.Errorf(errGoogleGenAICompletion, retryErr)
			}

			return retryResp, nil
		}
	}

	if err != nil {
		return nil, fmt.Errorf(errGoogleGenAICompletion, err)
	}

	return resp, nil
}

// Name returns the provider identifier.
func (p *googleProvider) Name() ProviderName {
	return ProviderGoogle
}

// IsAvailable returns true if the provider is configured and available.
func (p *googleProvider) IsAvailable() bool {
	return p.cfg.GoogleAPIKey != ""
}

// Priority returns the provider priority.
func (p *googleProvider) Priority() int {
	return PriorityPrimary
}

// SupportsImageGeneration returns false as Google Gemini doesn't support image generation in this context.
func (p *googleProvider) SupportsImageGeneration() bool {
	return false
}

// resolveModel returns the appropriate model name for Google.
// Always uses the cheapest model (gemini-1.5-flash) regardless of input.
func (p *googleProvider) resolveModel(model string) string {
	// If it's already a Gemini model name, use it directly
	if strings.HasPrefix(model, modelPrefixGemini) {
		return model
	}

	// Always use cheapest model for fallback
	return defaultGoogleModel
}

// ProcessBatch implements Provider interface.
func (p *googleProvider) ProcessBatch(ctx context.Context, messages []MessageInput, targetLanguage, model, tone string) ([]BatchResult, error) {
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

	resolvedModel := p.resolveModel(model)

	resp, err := p.generateContent(ctx, model, genai.Text(sanitizeUTF8(content.String())))
	if err != nil {
		p.usageRecorder.RecordTokenUsage(string(ProviderGoogle), resolvedModel, TaskSummarize, 0, 0, false)

		return nil, fmt.Errorf(errGoogleGenAICompletion, err)
	}

	promptTokens, completionTokens := extractGoogleTokenUsage(resp)
	p.usageRecorder.RecordTokenUsage(string(ProviderGoogle), resolvedModel, TaskSummarize, promptTokens, completionTokens, true)

	responseText := extractGoogleResponseText(resp)
	if responseText == "" {
		return nil, ErrEmptyLLMResponse
	}

	return p.parseProcessBatchResponse(responseText, messages)
}

// parseProcessBatchResponse parses the JSON response from batch processing.
func (p *googleProvider) parseProcessBatchResponse(responseText string, messages []MessageInput) ([]BatchResult, error) {
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
func (p *googleProvider) TranslateText(ctx context.Context, text, targetLanguage, model string) (string, error) {
	if strings.TrimSpace(text) == "" || strings.TrimSpace(targetLanguage) == "" {
		return text, nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := fmt.Sprintf(translatePromptFmt, targetLanguage, targetLanguage, text)
	resolvedModel := p.resolveModel(model)

	resp, err := p.generateContent(ctx, model, genai.Text(sanitizeUTF8(prompt)))
	if err != nil {
		p.usageRecorder.RecordTokenUsage(string(ProviderGoogle), resolvedModel, TaskTranslate, 0, 0, false)

		return "", fmt.Errorf("google genai translation: %w", err)
	}

	promptTokens, completionTokens := extractGoogleTokenUsage(resp)
	p.usageRecorder.RecordTokenUsage(string(ProviderGoogle), resolvedModel, TaskTranslate, promptTokens, completionTokens, true)

	return strings.TrimSpace(extractGoogleResponseText(resp)), nil
}

// CompleteText implements Provider interface.
func (p *googleProvider) CompleteText(ctx context.Context, prompt, model string) (string, error) {
	return p.generateText(ctx, model, TaskComplete, prompt, "google genai completion")
}

func (p *googleProvider) generateText(ctx context.Context, model, task, prompt, errContext string) (string, error) {
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	resolvedModel := p.resolveModel(model)

	resp, err := p.generateContent(ctx, model, genai.Text(sanitizeUTF8(prompt)))
	if err != nil {
		p.usageRecorder.RecordTokenUsage(string(ProviderGoogle), resolvedModel, task, 0, 0, false)

		return "", fmt.Errorf(errFmtContextWrap, errContext, err)
	}

	promptTokens, completionTokens := extractGoogleTokenUsage(resp)
	p.usageRecorder.RecordTokenUsage(string(ProviderGoogle), resolvedModel, task, promptTokens, completionTokens, true)

	return strings.TrimSpace(extractGoogleResponseText(resp)), nil
}

// GenerateNarrative implements Provider interface.
func (p *googleProvider) GenerateNarrative(ctx context.Context, items []domain.Item, targetLanguage, model, tone string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	prompt := buildNarrativePrompt(items, nil, targetLanguage, tone, defaultNarrativePrompt)

	return p.generateText(ctx, model, TaskNarrative, prompt, "google genai narrative")
}

// GenerateNarrativeWithEvidence implements Provider interface.
func (p *googleProvider) GenerateNarrativeWithEvidence(ctx context.Context, items []domain.Item, evidence ItemEvidence, targetLanguage, model, tone string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	prompt := buildNarrativePrompt(items, evidence, targetLanguage, tone, defaultNarrativePrompt)

	return p.generateText(ctx, model, TaskNarrative, prompt, "google genai narrative with evidence")
}

// SummarizeCluster implements Provider interface.
func (p *googleProvider) SummarizeCluster(ctx context.Context, items []domain.Item, targetLanguage, model, tone string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	prompt := buildClusterSummaryPrompt(items, nil, targetLanguage, tone, defaultClusterSummaryPrompt)

	return p.generateText(ctx, model, TaskCluster, prompt, "google genai cluster summary")
}

// SummarizeClusterWithEvidence implements Provider interface.
func (p *googleProvider) SummarizeClusterWithEvidence(ctx context.Context, items []domain.Item, evidence ItemEvidence, targetLanguage, model, tone string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	prompt := buildClusterSummaryPrompt(items, evidence, targetLanguage, tone, defaultClusterSummaryPrompt)

	return p.generateText(ctx, model, TaskCluster, prompt, "google genai cluster summary with evidence")
}

// GenerateClusterTopic implements Provider interface.
func (p *googleProvider) GenerateClusterTopic(ctx context.Context, items []domain.Item, targetLanguage, model string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildClusterTopicPrompt(items, targetLanguage, defaultClusterTopicPrompt)
	resolvedModel := p.resolveModel(model)

	resp, err := p.generateContent(ctx, model, genai.Text(sanitizeUTF8(prompt)))
	if err != nil {
		p.usageRecorder.RecordTokenUsage(string(ProviderGoogle), resolvedModel, TaskTopic, 0, 0, false)

		return "", fmt.Errorf("google genai cluster topic: %w", err)
	}

	promptTokens, completionTokens := extractGoogleTokenUsage(resp)
	p.usageRecorder.RecordTokenUsage(string(ProviderGoogle), resolvedModel, TaskTopic, promptTokens, completionTokens, true)

	return strings.TrimSpace(extractGoogleResponseText(resp)), nil
}

// RelevanceGate implements Provider interface.
func (p *googleProvider) RelevanceGate(ctx context.Context, text, model, prompt string) (RelevanceGateResult, error) {
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return RelevanceGateResult{}, fmt.Errorf(errRateLimiterSimple, err)
	}

	fullPrompt := fmt.Sprintf(relevanceGateFormat, prompt, text)
	resolvedModel := p.resolveModel(model)

	resp, err := p.generateContent(ctx, model, genai.Text(sanitizeUTF8(fullPrompt)))
	if err != nil {
		p.usageRecorder.RecordTokenUsage(string(ProviderGoogle), resolvedModel, TaskRelevanceGate, 0, 0, false)

		return RelevanceGateResult{}, fmt.Errorf("google genai relevance gate: %w", err)
	}

	promptTokens, completionTokens := extractGoogleTokenUsage(resp)
	p.usageRecorder.RecordTokenUsage(string(ProviderGoogle), resolvedModel, TaskRelevanceGate, promptTokens, completionTokens, true)

	responseText := extractJSON(extractGoogleResponseText(resp))

	var result RelevanceGateResult
	if unmarshalErr := json.Unmarshal([]byte(responseText), &result); unmarshalErr != nil {
		// Default to relevant if parsing fails - log and return fallback
		p.logger.Warn().Err(unmarshalErr).Str(logKeyResponse, responseText).Msg(logMsgParseRelevanceGateFail)

		return RelevanceGateResult{
			Decision:   "relevant",
			Confidence: googleDefaultConfidence,
			Reason:     "failed to parse response",
		}, nil
	}

	return result, nil
}

// CompressSummariesForCover implements Provider interface.
func (p *googleProvider) CompressSummariesForCover(ctx context.Context, summaries []string, model string) ([]string, error) {
	if len(summaries) == 0 {
		return nil, nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildCompressSummariesPrompt(summaries)
	resolvedModel := p.resolveModel(model)

	resp, err := p.generateContent(ctx, model, genai.Text(sanitizeUTF8(compressSummariesSystemPrompt+"\n\n"+prompt)))
	if err != nil {
		p.usageRecorder.RecordTokenUsage(string(ProviderGoogle), resolvedModel, TaskCompress, 0, 0, false)

		return nil, fmt.Errorf("google genai compress summaries: %w", err)
	}

	promptTokens, completionTokens := extractGoogleTokenUsage(resp)
	p.usageRecorder.RecordTokenUsage(string(ProviderGoogle), resolvedModel, TaskCompress, promptTokens, completionTokens, true)

	responseText := extractGoogleResponseText(resp)
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

// GenerateDigestCover returns an error as Google Gemini doesn't support image generation in this context.
func (p *googleProvider) GenerateDigestCover(_ context.Context, _ []string, _ string) ([]byte, error) {
	return nil, ErrNoImageProvider
}

// extractGoogleResponseText extracts text content from Google Gemini response.
func extractGoogleResponseText(resp *genai.GenerateContentResponse) string {
	if resp == nil {
		return ""
	}

	var result strings.Builder

	for _, candidate := range resp.Candidates {
		if candidate.Content != nil {
			for _, part := range candidate.Content.Parts {
				if text, ok := part.(genai.Text); ok {
					result.WriteString(string(text))
				}
			}
		}
	}

	return result.String()
}

// extractGoogleTokenUsage extracts token usage from Google Gemini response.
func extractGoogleTokenUsage(resp *genai.GenerateContentResponse) (promptTokens, completionTokens int) {
	if resp == nil || resp.UsageMetadata == nil {
		return 0, 0
	}

	return int(resp.UsageMetadata.PromptTokenCount), int(resp.UsageMetadata.CandidatesTokenCount)
}

// ExtractBullets extracts key bullet points from a message.
// This is a stub implementation - actual bullet extraction logic will be added later.
func (p *googleProvider) ExtractBullets(_ context.Context, input BulletExtractionInput, _, _ string) (BulletExtractionResult, error) {
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

// Ensure googleProvider implements Provider interface.
var _ Provider = (*googleProvider)(nil)
