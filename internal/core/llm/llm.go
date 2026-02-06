// Package llm provides LLM client interfaces and multi-provider support.
//
// The package supports multiple LLM providers with automatic fallback:
//   - OpenAI (primary)
//   - Anthropic Claude
//   - Google Gemini
//   - Cohere
//   - OpenRouter
//
// Features include:
//   - Circuit breaker pattern for provider resilience
//   - Token usage tracking and budget management
//   - Per-task model configuration
//   - Prompt caching and customization
package llm

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/core/embeddings"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
)

// BatchResult contains the LLM processing result for a single message.
type BatchResult struct {
	Index           int       `json:"index"`
	RelevanceScore  float32   `json:"relevance_score"`
	ImportanceScore float32   `json:"importance_score"`
	Topic           string    `json:"topic"`
	Summary         string    `json:"summary"`
	Language        string    `json:"language"`
	SourceChannel   string    `json:"source_channel"` // Echo back the source channel name for verification
	Embedding       []float32 `json:"-"`
}

// MessageInput wraps a RawMessage with additional context for LLM processing.
type MessageInput struct {
	domain.RawMessage
	Context       []string              // Recent messages from the same channel
	ResolvedLinks []domain.ResolvedLink // Resolved URLs found in the message
}

// EvidenceSource represents evidence from external sources for context injection.
type EvidenceSource struct {
	URL             string
	Domain          string
	Title           string
	Description     string // Added for "Background" context
	AgreementScore  float32
	IsContradiction bool
}

// ItemEvidence maps item IDs to their associated evidence sources.
type ItemEvidence map[string][]EvidenceSource

// BulletExtractionInput contains the input for bullet extraction.
type BulletExtractionInput struct {
	Text            string // Raw message text
	PreviewText     string // Preview/link content if available
	Summary         string // Existing summary for context
	MaxBullets      int    // Maximum number of bullets to extract (0 = default)
	LinkContext     string // Resolved link content (if available)
	LinkContextRole string // "primary" or "supplemental" - how to use LinkContext
}

// ExtractedBullet represents a single bullet point extracted from a message.
type ExtractedBullet struct {
	Text            string  `json:"text"`             // The bullet text content
	RelevanceScore  float32 `json:"relevance_score"`  // Relevance score (0-1)
	ImportanceScore float32 `json:"importance_score"` // Importance score (0-1)
	Topic           string  `json:"topic"`            // Topic classification
}

// BulletExtractionResult contains the result of bullet extraction.
type BulletExtractionResult struct {
	Bullets []ExtractedBullet `json:"bullets"`
}

// Client defines the interface for LLM operations.
// Implemented by Registry which provides multi-provider support.
type Client interface {
	ProcessBatch(ctx context.Context, messages []MessageInput, targetLanguage string, model string, tone string) ([]BatchResult, error)
	TranslateText(ctx context.Context, text string, targetLanguage string, model string) (string, error)
	CompleteText(ctx context.Context, prompt string, model string) (string, error)
	GenerateNarrative(ctx context.Context, items []domain.Item, targetLanguage string, model string, tone string) (string, error)
	GenerateNarrativeWithEvidence(ctx context.Context, items []domain.Item, evidence ItemEvidence, targetLanguage string, model string, tone string) (string, error)
	SummarizeCluster(ctx context.Context, items []domain.Item, targetLanguage string, model string, tone string) (string, error)
	SummarizeClusterWithEvidence(ctx context.Context, items []domain.Item, evidence ItemEvidence, targetLanguage string, model string, tone string) (string, error)
	GenerateClusterTopic(ctx context.Context, items []domain.Item, targetLanguage string, model string) (string, error)
	RelevanceGate(ctx context.Context, text string, model string, prompt string) (RelevanceGateResult, error)
	CompressSummariesForCover(ctx context.Context, summaries []string) ([]string, error)
	GenerateDigestCover(ctx context.Context, topics []string, narrative string) ([]byte, error)
	GetProviderStatuses() []ProviderStatus
	// Bullet extraction for bulletized digest output
	ExtractBullets(ctx context.Context, input BulletExtractionInput, targetLanguage string, model string) (BulletExtractionResult, error)
	// Budget tracking methods
	SetBudgetLimit(limit int64)
	GetBudgetStatus() (dailyTokens, dailyLimit int64, percentage float64)
	SetBudgetAlertCallback(callback func(alert BudgetAlert))
	RecordTokensForBudget(tokens int)
	// Runtime override methods
	RefreshOverride(ctx context.Context, reader SettingsReader, settingKey string)
}

// RelevanceGateResult contains the outcome of relevance gate evaluation.
type RelevanceGateResult struct {
	Decision   string  `json:"decision"`
	Confidence float32 `json:"confidence"`
	Reason     string  `json:"reason"`
}

// PromptStore provides access to customizable prompts from settings.
type PromptStore interface {
	GetSetting(ctx context.Context, key string, target interface{}) error
}

// buildCircuitConfig creates a CircuitBreakerConfig with defaults applied.
func buildCircuitConfig(cfg *config.Config) embeddings.CircuitBreakerConfig {
	circuitCfg := embeddings.CircuitBreakerConfig{
		Threshold:  cfg.LLMCircuitThreshold,
		ResetAfter: cfg.LLMCircuitTimeout,
	}

	if circuitCfg.Threshold == 0 {
		circuitCfg.Threshold = defaultCircuitThreshold
	}

	if circuitCfg.ResetAfter == 0 {
		circuitCfg.ResetAfter = defaultCircuitTimeout
	}

	return circuitCfg
}

// registerProviders registers all available LLM providers with the registry.
func registerProviders(ctx context.Context, registry *Registry, cfg *config.Config, store PromptStore, logger *zerolog.Logger, circuitCfg embeddings.CircuitBreakerConfig) {
	recorder := registry.UsageRecorder()

	// Register OpenAI as primary provider
	if cfg.LLMAPIKey != "" && cfg.LLMAPIKey != llmAPIKeyMock {
		registry.Register(NewOpenAIProvider(cfg, store, recorder, logger), circuitCfg)
	}

	// Register Anthropic as first fallback
	if cfg.AnthropicAPIKey != "" {
		registry.Register(NewAnthropicProvider(cfg, store, recorder, logger), circuitCfg)
	}

	// Register Google as second fallback
	if cfg.GoogleAPIKey != "" {
		googleProvider, err := NewGoogleProvider(ctx, cfg, store, recorder, logger)
		if err != nil {
			logger.Warn().Err(err).Msg("failed to create Google LLM provider")
		} else {
			registry.Register(googleProvider, circuitCfg)
		}
	}

	// Register Cohere as third fallback
	if cfg.CohereAPIKey != "" {
		registry.Register(NewCohereProvider(cfg, store, recorder, logger), circuitCfg)
	}

	// Register OpenRouter as fourth fallback
	if cfg.OpenRouterAPIKey != "" {
		registry.Register(NewOpenRouterProvider(cfg, store, recorder, logger), circuitCfg)
	}

	// If no providers configured, use mock
	if registry.ProviderCount() == 0 {
		registry.Register(NewMockProvider(cfg), circuitCfg)
	}
}

// New creates a new LLM client with multi-provider fallback support.
// It registers providers in priority order: OpenAI (primary), Anthropic (fallback), Google (second fallback).
// If no providers are configured, it returns a mock client.
// The usageStore parameter is optional and allows persisting token usage to the database.
func New(ctx context.Context, cfg *config.Config, store PromptStore, usageStore UsageStore, logger *zerolog.Logger) Client {
	if logger == nil {
		nopLogger := zerolog.Nop()
		logger = &nopLogger
	}

	registry := NewRegistry(logger)

	// Set the usage store if provided
	if usageStore != nil {
		registry.SetUsageStore(usageStore)
	}

	circuitCfg := buildCircuitConfig(cfg)
	registerProviders(ctx, registry, cfg, store, logger, circuitCfg)

	// Apply env-based model overrides first
	applyModelOverrides(registry, cfg)

	// Then load DB overrides (runtime config takes precedence)
	if store != nil {
		registry.LoadOverridesFromDB(ctx, store)
	}

	return registry
}

// applyModelOverrides applies per-task model overrides from config.
func applyModelOverrides(registry *Registry, cfg *config.Config) {
	// Apply model overrides for each task type
	registry.SetTaskModelOverride(TaskTypeSummarize, cfg.LLMSummarizeModel)
	registry.SetTaskModelOverride(TaskTypeClusterSummary, cfg.LLMClusterModel)
	registry.SetTaskModelOverride(TaskTypeClusterTopic, cfg.LLMClusterModel)
	registry.SetTaskModelOverride(TaskTypeNarrative, cfg.LLMNarrativeModel)
	registry.SetTaskModelOverride(TaskTypeTranslate, cfg.LLMTranslateModel)
	registry.SetTaskModelOverride(TaskTypeComplete, cfg.LLMCompleteModel)
	registry.SetTaskModelOverride(TaskTypeRelevanceGate, cfg.LLMRelevanceGateModel)
	registry.SetTaskModelOverride(TaskTypeCompress, cfg.LLMCompressModel)
}

// apiCallResult holds the common fields from LLM API responses.
type apiCallResult struct {
	Text             string
	PromptTokens     int
	CompletionTokens int
	FinishReason     string
}

// bulletExtractionHelper handles common bullet extraction logic across providers.
type bulletExtractionHelper struct {
	providerName  ProviderName
	usageRecorder UsageRecorder
	logger        *zerolog.Logger
}

// extractBullets performs bullet extraction using the provided API call function.
func (h *bulletExtractionHelper) extractBullets(
	_ context.Context,
	input BulletExtractionInput,
	_ string,
	resolvedModel string,
	apiCall func() (apiCallResult, error),
) (BulletExtractionResult, error) {
	result, err := apiCall()
	if err != nil {
		h.usageRecorder.RecordTokenUsage(string(h.providerName), resolvedModel, TaskBulletExtract, 0, 0, false)
		return BulletExtractionResult{}, err
	}

	h.usageRecorder.RecordTokenUsage(string(h.providerName), resolvedModel, TaskBulletExtract, result.PromptTokens, result.CompletionTokens, true)

	bullets, err := parseBulletResponse(result.Text)
	if err != nil {
		h.logger.Warn().Err(err).Str(logKeyResponse, result.Text).Msg(logMsgBulletParseError)
		return makeBulletFallback(input), nil
	}

	return BulletExtractionResult{Bullets: bullets}, nil
}

// relevanceGateHelper handles common relevance gate logic across providers.
type relevanceGateHelper struct {
	providerName      ProviderName
	usageRecorder     UsageRecorder
	logger            *zerolog.Logger
	defaultConfidence float32
}

// executeRelevanceGate performs relevance gate using the provided API call function.
func (h *relevanceGateHelper) executeRelevanceGate(
	resolvedModel string,
	apiCall func() (apiCallResult, error),
) (RelevanceGateResult, error) {
	apiResult, err := apiCall()
	if err != nil {
		h.usageRecorder.RecordTokenUsage(string(h.providerName), resolvedModel, TaskRelevanceGate, 0, 0, false)
		return RelevanceGateResult{}, err
	}

	h.usageRecorder.RecordTokenUsage(string(h.providerName), resolvedModel, TaskRelevanceGate, apiResult.PromptTokens, apiResult.CompletionTokens, true)
	responseText := extractJSON(apiResult.Text)

	var result RelevanceGateResult
	if unmarshalErr := json.Unmarshal([]byte(responseText), &result); unmarshalErr != nil {
		h.logger.Warn().Err(unmarshalErr).Str(logKeyResponse, responseText).Msg(logMsgParseRelevanceGateFail)

		return RelevanceGateResult{
			Decision:   "relevant",
			Confidence: h.defaultConfidence,
			Reason:     "failed to parse response",
		}, nil
	}

	return result, nil
}

// compressHelper handles common compress summaries logic across providers.
type compressHelper struct {
	providerName  ProviderName
	usageRecorder UsageRecorder
}

// executeCompress performs summary compression using the provided API call function.
func (h *compressHelper) executeCompress(
	resolvedModel string,
	apiCall func() (apiCallResult, error),
) ([]string, error) {
	result, err := apiCall()
	if err != nil {
		h.usageRecorder.RecordTokenUsage(string(h.providerName), resolvedModel, TaskCompress, 0, 0, false)
		return nil, err
	}

	h.usageRecorder.RecordTokenUsage(string(h.providerName), resolvedModel, TaskCompress, result.PromptTokens, result.CompletionTokens, true)
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
