// Package embeddings provides text embedding generation with multi-provider support.
//
// The package supports multiple embedding providers with automatic fallback:
//   - OpenAI text-embedding-ada-002 / text-embedding-3-small
//   - Cohere embed-v3
//   - Google text-embedding-004
//
// Features include:
//   - Circuit breaker pattern for provider resilience
//   - Dimension normalization across providers
//   - Rate limiting per provider
package embeddings

import (
	"context"
	"strings"

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

	// Google settings
	GoogleAPIKey    string
	GoogleModel     string
	GoogleRateLimit int

	// Provider order (comma-separated: "openai,cohere,google")
	ProviderOrder string

	// Circuit breaker settings
	CircuitBreakerConfig CircuitBreakerConfig

	// Target dimensions for output vectors
	TargetDimensions int
}

// NewClient creates a new embedding client with configured providers.
func NewClient(ctx context.Context, cfg Config, logger *zerolog.Logger) Client {
	if cfg.TargetDimensions == 0 {
		cfg.TargetDimensions = DefaultDimensions
	}

	registry := NewRegistry(cfg.TargetDimensions, logger)

	// Parse provider order (default: openai,cohere,google)
	providerOrder := parseProviderOrder(cfg.ProviderOrder)

	// Register providers in the specified order
	for _, provider := range providerOrder {
		switch provider {
		case "openai":
			registerOpenAI(registry, cfg)
		case "cohere":
			registerCohere(registry, cfg)
		case "google":
			registerGoogle(ctx, registry, cfg, logger)
		}
	}

	// If no providers available, return a mock client for testing/development
	if registry.ProviderCount() == 0 {
		logger.Warn().Msg("no embedding providers configured, using mock provider")

		mockProvider := NewMockProvider()
		registry.Register(mockProvider, cfg.CircuitBreakerConfig)
	}

	return registry
}

// parseProviderOrder parses the provider order string into a list.
func parseProviderOrder(order string) []string {
	if order == "" {
		return []string{"openai", "cohere", "google"}
	}

	var providers []string

	for _, p := range strings.Split(order, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			providers = append(providers, strings.ToLower(p))
		}
	}

	return providers
}

func registerOpenAI(registry *Registry, cfg Config) {
	if cfg.OpenAIAPIKey != "" && cfg.OpenAIAPIKey != mockAPIKey {
		openaiProvider := NewOpenAIProvider(OpenAIConfig{
			APIKey:     cfg.OpenAIAPIKey,
			Model:      cfg.OpenAIModel,
			Dimensions: cfg.OpenAIDimensions,
			RateLimit:  cfg.OpenAIRateLimit,
		})
		registry.Register(openaiProvider, cfg.CircuitBreakerConfig)
	}
}

func registerCohere(registry *Registry, cfg Config) {
	if cfg.CohereAPIKey != "" {
		cohereProvider := NewCohereProvider(CohereConfig{
			APIKey:    cfg.CohereAPIKey,
			Model:     cfg.CohereModel,
			RateLimit: cfg.CohereRateLimit,
		})
		registry.Register(cohereProvider, cfg.CircuitBreakerConfig)
	}
}

func registerGoogle(ctx context.Context, registry *Registry, cfg Config, logger *zerolog.Logger) {
	if cfg.GoogleAPIKey != "" {
		googleProvider, err := NewGoogleProvider(ctx, GoogleConfig{
			APIKey:    cfg.GoogleAPIKey,
			Model:     cfg.GoogleModel,
			RateLimit: cfg.GoogleRateLimit,
		})
		if err != nil {
			logger.Error().Err(err).Msg("failed to create Google embedding provider")
		} else if googleProvider.IsAvailable() {
			registry.Register(googleProvider, cfg.CircuitBreakerConfig)
		}
	}
}
