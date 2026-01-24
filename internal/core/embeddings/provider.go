package embeddings

import (
	"context"
	"time"
)

// ProviderName identifies an embedding provider.
type ProviderName string

// Provider name constants.
const (
	ProviderOpenAI ProviderName = "openai"
	ProviderCohere ProviderName = "cohere"
	ProviderMock   ProviderName = "mock"
)

// Priority constants for provider ordering.
const (
	PriorityPrimary  = 100 // Primary provider (OpenAI)
	PriorityFallback = 50  // Fallback provider (Cohere)
	PriorityMock     = 0   // Mock provider for testing
)

// Default dimensions for embeddings (matches existing DB schema).
const DefaultDimensions = 1536

// Circuit breaker constants.
const (
	defaultCircuitThreshold = 5
)

// Shared error format strings.
const errRateLimiterFmt = "rate limiter: %w"

// API key constants.
const mockAPIKey = "mock"

// EmbeddingResult contains the embedding vector and metadata.
type EmbeddingResult struct {
	Vector     []float32
	Dimensions int
	Provider   ProviderName
}

// Provider defines the interface for embedding providers.
type Provider interface {
	// Name returns the provider identifier.
	Name() ProviderName

	// GetEmbedding generates an embedding for the given text.
	GetEmbedding(ctx context.Context, text string) (EmbeddingResult, error)

	// IsAvailable returns true if the provider is currently available.
	IsAvailable() bool

	// Priority returns the provider priority (higher = preferred).
	Priority() int

	// Dimensions returns the native output dimensions of this provider.
	Dimensions() int
}

// CircuitBreakerConfig defines circuit breaker settings.
type CircuitBreakerConfig struct {
	Threshold  int           // Number of failures before opening circuit
	ResetAfter time.Duration // Time before attempting recovery
}

// DefaultCircuitBreakerConfig returns sensible defaults for circuit breaker.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		Threshold:  defaultCircuitThreshold,
		ResetAfter: time.Minute,
	}
}
