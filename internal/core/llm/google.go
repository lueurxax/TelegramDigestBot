package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
	ModelGeminiFlashLite = "gemini-2.5-flash-lite"

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
type googleProvider struct {
	cfg         *config.Config
	client      *genai.Client
	logger      *zerolog.Logger
	rateLimiter *rate.Limiter
	promptStore PromptStore
}

// NewGoogleProvider creates a new Google Gemini LLM provider.
func NewGoogleProvider(ctx context.Context, cfg *config.Config, store PromptStore, logger *zerolog.Logger) (*googleProvider, error) {
	client, err := genai.NewClient(ctx, option.WithAPIKey(cfg.GoogleAPIKey))
	if err != nil {
		return nil, fmt.Errorf("creating google genai client: %w", err)
	}

	rateLimit := cfg.RateLimitRPS
	if rateLimit == 0 {
		rateLimit = 1
	}

	return &googleProvider{
		cfg:         cfg,
		client:      client,
		logger:      logger,
		rateLimiter: rate.NewLimiter(rate.Limit(float64(rateLimit)), googleRateLimiterBurst),
		promptStore: store,
	}, nil
}

// Close closes the Google client.
func (p *googleProvider) Close() error {
	if p.client != nil {
		if err := p.client.Close(); err != nil {
			return fmt.Errorf("closing google genai client: %w", err)
		}
	}

	return nil
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
	return PrioritySecondFallback
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
	genModel := p.client.GenerativeModel(resolvedModel)

	resp, err := genModel.GenerateContent(ctx, genai.Text(sanitizeUTF8(content.String())))
	if err != nil {
		return nil, fmt.Errorf(errGoogleGenAICompletion, err)
	}

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

	prompt := fmt.Sprintf(translatePromptFmt, targetLanguage, text)
	resolvedModel := p.resolveModel(model)
	genModel := p.client.GenerativeModel(resolvedModel)

	resp, err := genModel.GenerateContent(ctx, genai.Text(sanitizeUTF8(prompt)))
	if err != nil {
		return "", fmt.Errorf("google genai translation: %w", err)
	}

	return strings.TrimSpace(extractGoogleResponseText(resp)), nil
}

// CompleteText implements Provider interface.
func (p *googleProvider) CompleteText(ctx context.Context, prompt, model string) (string, error) {
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	resolvedModel := p.resolveModel(model)
	genModel := p.client.GenerativeModel(resolvedModel)

	resp, err := genModel.GenerateContent(ctx, genai.Text(sanitizeUTF8(prompt)))
	if err != nil {
		return "", fmt.Errorf("google genai completion: %w", err)
	}

	return strings.TrimSpace(extractGoogleResponseText(resp)), nil
}

// GenerateNarrative implements Provider interface.
func (p *googleProvider) GenerateNarrative(ctx context.Context, items []domain.Item, targetLanguage, model, tone string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildNarrativePrompt(items, nil, targetLanguage, tone, defaultNarrativePrompt)
	resolvedModel := p.resolveModel(model)
	genModel := p.client.GenerativeModel(resolvedModel)

	resp, err := genModel.GenerateContent(ctx, genai.Text(sanitizeUTF8(prompt)))
	if err != nil {
		return "", fmt.Errorf("google genai narrative: %w", err)
	}

	return strings.TrimSpace(extractGoogleResponseText(resp)), nil
}

// GenerateNarrativeWithEvidence implements Provider interface.
func (p *googleProvider) GenerateNarrativeWithEvidence(ctx context.Context, items []domain.Item, evidence ItemEvidence, targetLanguage, model, tone string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildNarrativePrompt(items, evidence, targetLanguage, tone, defaultNarrativePrompt)
	resolvedModel := p.resolveModel(model)
	genModel := p.client.GenerativeModel(resolvedModel)

	resp, err := genModel.GenerateContent(ctx, genai.Text(sanitizeUTF8(prompt)))
	if err != nil {
		return "", fmt.Errorf("google genai narrative with evidence: %w", err)
	}

	return strings.TrimSpace(extractGoogleResponseText(resp)), nil
}

// SummarizeCluster implements Provider interface.
func (p *googleProvider) SummarizeCluster(ctx context.Context, items []domain.Item, targetLanguage, model, tone string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildClusterSummaryPrompt(items, nil, targetLanguage, tone, defaultClusterSummaryPrompt)
	resolvedModel := p.resolveModel(model)
	genModel := p.client.GenerativeModel(resolvedModel)

	resp, err := genModel.GenerateContent(ctx, genai.Text(sanitizeUTF8(prompt)))
	if err != nil {
		return "", fmt.Errorf("google genai cluster summary: %w", err)
	}

	return strings.TrimSpace(extractGoogleResponseText(resp)), nil
}

// SummarizeClusterWithEvidence implements Provider interface.
func (p *googleProvider) SummarizeClusterWithEvidence(ctx context.Context, items []domain.Item, evidence ItemEvidence, targetLanguage, model, tone string) (string, error) {
	if len(items) == 0 {
		return "", nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return "", fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildClusterSummaryPrompt(items, evidence, targetLanguage, tone, defaultClusterSummaryPrompt)
	resolvedModel := p.resolveModel(model)
	genModel := p.client.GenerativeModel(resolvedModel)

	resp, err := genModel.GenerateContent(ctx, genai.Text(sanitizeUTF8(prompt)))
	if err != nil {
		return "", fmt.Errorf("google genai cluster summary with evidence: %w", err)
	}

	return strings.TrimSpace(extractGoogleResponseText(resp)), nil
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
	genModel := p.client.GenerativeModel(resolvedModel)

	resp, err := genModel.GenerateContent(ctx, genai.Text(sanitizeUTF8(prompt)))
	if err != nil {
		return "", fmt.Errorf("google genai cluster topic: %w", err)
	}

	return strings.TrimSpace(extractGoogleResponseText(resp)), nil
}

// RelevanceGate implements Provider interface.
func (p *googleProvider) RelevanceGate(ctx context.Context, text, model, prompt string) (RelevanceGateResult, error) {
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return RelevanceGateResult{}, fmt.Errorf(errRateLimiterSimple, err)
	}

	fullPrompt := fmt.Sprintf(relevanceGateFormat, prompt, text)
	resolvedModel := p.resolveModel(model)
	genModel := p.client.GenerativeModel(resolvedModel)

	resp, err := genModel.GenerateContent(ctx, genai.Text(sanitizeUTF8(fullPrompt)))
	if err != nil {
		return RelevanceGateResult{}, fmt.Errorf("google genai relevance gate: %w", err)
	}

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
func (p *googleProvider) CompressSummariesForCover(ctx context.Context, summaries []string) ([]string, error) {
	if len(summaries) == 0 {
		return nil, nil
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf(errRateLimiterSimple, err)
	}

	prompt := buildCompressSummariesPrompt(summaries)
	genModel := p.client.GenerativeModel(ModelGeminiFlashLite)

	resp, err := genModel.GenerateContent(ctx, genai.Text(sanitizeUTF8(compressSummariesSystemPrompt+"\n\n"+prompt)))
	if err != nil {
		return nil, fmt.Errorf("google genai compress summaries: %w", err)
	}

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

// Ensure googleProvider implements Provider interface.
var _ Provider = (*googleProvider)(nil)
