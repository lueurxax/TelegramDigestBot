package llm

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/core/embeddings"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
)

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

type MessageInput struct {
	domain.RawMessage
	Context       []string
	ResolvedLinks []domain.ResolvedLink
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
	// Budget tracking methods
	SetBudgetLimit(limit int64)
	GetBudgetStatus() (dailyTokens, dailyLimit int64, percentage float64)
	SetBudgetAlertCallback(callback func(alert BudgetAlert))
	RecordTokensForBudget(tokens int)
}

type RelevanceGateResult struct {
	Decision   string  `json:"decision"`
	Confidence float32 `json:"confidence"`
	Reason     string  `json:"reason"`
}

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
	// Register OpenAI as primary provider
	if cfg.LLMAPIKey != "" && cfg.LLMAPIKey != llmAPIKeyMock {
		registry.Register(NewOpenAIProvider(cfg, store, logger), circuitCfg)
	}

	// Register Anthropic as first fallback
	if cfg.AnthropicAPIKey != "" {
		registry.Register(NewAnthropicProvider(cfg, store, logger), circuitCfg)
	}

	// Register Google as second fallback
	if cfg.GoogleAPIKey != "" {
		googleProvider, err := NewGoogleProvider(ctx, cfg, store, logger)
		if err != nil {
			logger.Warn().Err(err).Msg("failed to create Google LLM provider")
		} else {
			registry.Register(googleProvider, circuitCfg)
		}
	}

	// Register Cohere as third fallback
	if cfg.CohereAPIKey != "" {
		registry.Register(NewCohereProvider(cfg, store, logger), circuitCfg)
	}

	// Register OpenRouter as fourth fallback
	if cfg.OpenRouterAPIKey != "" {
		registry.Register(NewOpenRouterProvider(cfg, store, logger), circuitCfg)
	}

	// If no providers configured, use mock
	if registry.ProviderCount() == 0 {
		registry.Register(NewMockProvider(cfg), circuitCfg)
	}
}

// New creates a new LLM client with multi-provider fallback support.
// It registers providers in priority order: OpenAI (primary), Anthropic (fallback), Google (second fallback).
// If no providers are configured, it returns a mock client.
func New(ctx context.Context, cfg *config.Config, store PromptStore, logger *zerolog.Logger) Client {
	if logger == nil {
		nopLogger := zerolog.Nop()
		logger = &nopLogger
	}

	registry := NewRegistry(logger)
	circuitCfg := buildCircuitConfig(cfg)
	registerProviders(ctx, registry, cfg, store, logger, circuitCfg)

	return registry
}
