package embeddings

import (
	"context"

	"github.com/rs/zerolog"
)

// Client defines the interface for embedding operations.
// This interface is used throughout the codebase for generating embeddings.
type Client interface {
	// GetEmbedding generates an embedding for the given text.
	// Returns a vector with consistent dimensions (1536 by default).
	GetEmbedding(ctx context.Context, text string) ([]float32, error)
}

// Ensure Registry implements Client interface.
var _ Client = (*Registry)(nil)

// Config holds configuration for creating an embedding client.
type Config struct {
	// OpenAI settings
	OpenAIAPIKey     string
	OpenAIModel      string
	OpenAIDimensions int
	OpenAIRateLimit  int

	// Cohere settings
	CohereAPIKey    string
	CohereModel     string
	CohereRateLimit int

	// Circuit breaker settings
	CircuitBreakerConfig CircuitBreakerConfig

	// Target dimensions for output vectors
	TargetDimensions int
}

// NewClient creates a new embedding client with configured providers.
func NewClient(cfg Config, logger *zerolog.Logger) Client {
	if cfg.TargetDimensions == 0 {
		cfg.TargetDimensions = DefaultDimensions
	}

	registry := NewRegistry(cfg.TargetDimensions, logger)

	// Register OpenAI as primary provider
	if cfg.OpenAIAPIKey != "" && cfg.OpenAIAPIKey != mockAPIKey {
		openaiProvider := NewOpenAIProvider(OpenAIConfig{
			APIKey:     cfg.OpenAIAPIKey,
			Model:      cfg.OpenAIModel,
			Dimensions: cfg.OpenAIDimensions,
			RateLimit:  cfg.OpenAIRateLimit,
		})
		registry.Register(openaiProvider, cfg.CircuitBreakerConfig)
	}

	// Register Cohere as fallback provider
	if cfg.CohereAPIKey != "" {
		cohereProvider := NewCohereProvider(CohereConfig{
			APIKey:    cfg.CohereAPIKey,
			Model:     cfg.CohereModel,
			RateLimit: cfg.CohereRateLimit,
		})
		registry.Register(cohereProvider, cfg.CircuitBreakerConfig)
	}

	// If no providers available, return a mock client for testing/development
	if registry.ProviderCount() == 0 {
		logger.Warn().Msg("no embedding providers configured, using mock provider")

		mockProvider := NewMockProvider()
		registry.Register(mockProvider, cfg.CircuitBreakerConfig)
	}

	return registry
}
