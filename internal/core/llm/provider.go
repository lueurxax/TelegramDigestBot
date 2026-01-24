package llm

import (
	"context"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
)

// ProviderName identifies an LLM provider.
type ProviderName string

// Provider name constants.
const (
	ProviderOpenAI    ProviderName = "openai"
	ProviderAnthropic ProviderName = "anthropic"
	ProviderGoogle    ProviderName = "google"
	ProviderMock      ProviderName = "mock"
)

// Priority constants for provider ordering.
const (
	PriorityPrimary        = 100 // Primary provider (OpenAI)
	PriorityFallback       = 50  // First fallback (Anthropic)
	PrioritySecondFallback = 25  // Second fallback (Google)
	PriorityMock           = 0   // Mock provider for testing
)

// Log key constants.
const logKeyProvider = "provider"

// Provider defines the interface for LLM providers.
// All providers must implement the core LLM methods.
type Provider interface {
	// Name returns the provider identifier.
	Name() ProviderName

	// IsAvailable returns true if the provider is configured and available.
	IsAvailable() bool

	// Priority returns the provider priority (higher = preferred).
	Priority() int

	// Core LLM methods - all providers must implement these
	ProcessBatch(ctx context.Context, messages []MessageInput, targetLanguage, model, tone string) ([]BatchResult, error)
	TranslateText(ctx context.Context, text, targetLanguage, model string) (string, error)
	CompleteText(ctx context.Context, prompt, model string) (string, error)
	GenerateNarrative(ctx context.Context, items []domain.Item, targetLanguage, model, tone string) (string, error)
	GenerateNarrativeWithEvidence(ctx context.Context, items []domain.Item, evidence ItemEvidence, targetLanguage, model, tone string) (string, error)
	SummarizeCluster(ctx context.Context, items []domain.Item, targetLanguage, model, tone string) (string, error)
	SummarizeClusterWithEvidence(ctx context.Context, items []domain.Item, evidence ItemEvidence, targetLanguage, model, tone string) (string, error)
	GenerateClusterTopic(ctx context.Context, items []domain.Item, targetLanguage, model string) (string, error)
	RelevanceGate(ctx context.Context, text, model, prompt string) (RelevanceGateResult, error)
	CompressSummariesForCover(ctx context.Context, summaries []string) ([]string, error)

	// Optional capability - not all providers support image generation
	SupportsImageGeneration() bool
	GenerateDigestCover(ctx context.Context, topics []string, narrative string) ([]byte, error)
}
