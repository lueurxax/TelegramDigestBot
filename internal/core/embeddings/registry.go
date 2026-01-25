package embeddings

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Registry errors.
var (
	ErrNoProvidersAvailable = errors.New("no embedding providers available")
	ErrProviderNotFound     = errors.New("embedding provider not found")
	ErrAllProvidersFailed   = errors.New("all embedding providers failed")
)

// Log key constants.
const logKeyProvider = "provider"

// Registry manages embedding providers with fallback support.
type Registry struct {
	mu              sync.RWMutex
	providers       map[ProviderName]Provider
	order           []ProviderName // Priority order (highest first)
	circuitBreakers map[ProviderName]*CircuitBreaker
	targetDimension int
	logger          *zerolog.Logger
}

// NewRegistry creates a new provider registry.
func NewRegistry(targetDimension int, logger *zerolog.Logger) *Registry {
	return &Registry{
		providers:       make(map[ProviderName]Provider),
		order:           make([]ProviderName, 0),
		circuitBreakers: make(map[ProviderName]*CircuitBreaker),
		targetDimension: targetDimension,
		logger:          logger,
	}
}

// Register adds a provider to the registry.
func (r *Registry) Register(p Provider, cfg CircuitBreakerConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := p.Name()
	r.providers[name] = p
	r.order = append(r.order, name)
	r.circuitBreakers[name] = NewCircuitBreaker(cfg, r.logger)

	// Sort by priority (descending)
	r.sortProvidersByPriority()

	// Track provider availability metric
	SetEmbeddingProviderAvailable(string(name), p.IsAvailable())

	r.logger.Info().
		Str(logKeyProvider, string(name)).
		Int("priority", p.Priority()).
		Int("dimensions", p.Dimensions()).
		Msg("registered embedding provider")
}

// GetEmbedding attempts to get an embedding using available providers with fallback.
// Returns a vector padded/truncated to the target dimension.
func (r *Registry) GetEmbedding(ctx context.Context, text string) ([]float32, error) {
	r.mu.RLock()
	providers := r.getActiveProviders()

	primaryProvider := ""
	if len(r.order) > 0 {
		primaryProvider = string(r.order[0])
	}

	r.mu.RUnlock()

	if len(providers) == 0 {
		return nil, ErrNoProvidersAvailable
	}

	var lastErr error

	estimatedTokens := estimateTokens(text)

	for _, p := range providers {
		cb := r.getCircuitBreaker(p.Name())
		providerName := string(p.Name())
		model := r.getModelForProvider(p.Name())

		if !cb.CanAttempt() {
			r.logger.Debug().
				Str(logKeyProvider, providerName).
				Msg("skipping provider - circuit breaker open")
			SetEmbeddingProviderAvailable(providerName, false)

			continue
		}

		start := time.Now()
		result, err := p.GetEmbedding(ctx, text)
		duration := time.Since(start)

		RecordEmbeddingLatency(providerName, model, duration)

		if err != nil {
			cb.RecordFailure(p.Name())
			RecordEmbeddingRequest(providerName, model, false)

			lastErr = err

			r.logger.Warn().
				Err(err).
				Str(logKeyProvider, providerName).
				Msg("embedding provider failed, trying fallback")

			continue
		}

		cb.RecordSuccess()
		RecordEmbeddingRequest(providerName, model, true)
		RecordEmbeddingTokens(providerName, model, estimatedTokens)
		SetEmbeddingProviderAvailable(providerName, true)

		// Log and record if we used a fallback provider
		if primaryProvider != "" && providerName != primaryProvider {
			RecordEmbeddingFallback(primaryProvider, providerName)
			r.logger.Info().
				Str(logKeyProvider, providerName).
				Str("from_provider", primaryProvider).
				Msg("used fallback embedding provider")
		}

		// Pad or truncate to target dimension
		return PadToTargetDimensions(result.Vector, r.targetDimension), nil
	}

	if lastErr != nil {
		return nil, errors.Join(ErrAllProvidersFailed, lastErr)
	}

	return nil, ErrNoProvidersAvailable
}

// GetEmbeddingWithMetadata returns the full embedding result including metadata.
func (r *Registry) GetEmbeddingWithMetadata(ctx context.Context, text string) (EmbeddingResult, error) {
	r.mu.RLock()
	providers := r.getActiveProviders()

	primaryProvider := ""
	if len(r.order) > 0 {
		primaryProvider = string(r.order[0])
	}

	r.mu.RUnlock()

	if len(providers) == 0 {
		return EmbeddingResult{}, ErrNoProvidersAvailable
	}

	var lastErr error

	estimatedTokens := estimateTokens(text)

	for _, p := range providers {
		cb := r.getCircuitBreaker(p.Name())
		providerName := string(p.Name())
		model := r.getModelForProvider(p.Name())

		if !cb.CanAttempt() {
			SetEmbeddingProviderAvailable(providerName, false)
			continue
		}

		start := time.Now()
		result, err := p.GetEmbedding(ctx, text)
		duration := time.Since(start)

		RecordEmbeddingLatency(providerName, model, duration)

		if err != nil {
			cb.RecordFailure(p.Name())
			RecordEmbeddingRequest(providerName, model, false)

			lastErr = err

			continue
		}

		cb.RecordSuccess()
		RecordEmbeddingRequest(providerName, model, true)
		RecordEmbeddingTokens(providerName, model, estimatedTokens)
		SetEmbeddingProviderAvailable(providerName, true)

		// Record fallback if not using primary provider
		if primaryProvider != "" && providerName != primaryProvider {
			RecordEmbeddingFallback(primaryProvider, providerName)
		}

		// Pad to target dimension
		result.Vector = PadToTargetDimensions(result.Vector, r.targetDimension)
		result.Dimensions = r.targetDimension

		return result, nil
	}

	if lastErr != nil {
		return EmbeddingResult{}, errors.Join(ErrAllProvidersFailed, lastErr)
	}

	return EmbeddingResult{}, ErrNoProvidersAvailable
}

// ProviderCount returns the number of registered providers.
func (r *Registry) ProviderCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.providers)
}

// ProviderNames returns the names of all registered providers in priority order.
func (r *Registry) ProviderNames() []ProviderName {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]ProviderName, len(r.order))
	copy(names, r.order)

	return names
}

// getActiveProviders returns providers that are available (not checking circuit breaker).
func (r *Registry) getActiveProviders() []Provider {
	active := make([]Provider, 0, len(r.providers))

	for _, name := range r.order {
		p := r.providers[name]
		if p.IsAvailable() {
			active = append(active, p)
		}
	}

	return active
}

// sortProvidersByPriority sorts providers by priority in descending order.
func (r *Registry) sortProvidersByPriority() {
	sort.SliceStable(r.order, func(i, j int) bool {
		pi := r.providers[r.order[i]].Priority()
		pj := r.providers[r.order[j]].Priority()

		return pi > pj
	})
}

// getCircuitBreaker returns the circuit breaker for a provider.
func (r *Registry) getCircuitBreaker(name ProviderName) *CircuitBreaker {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.circuitBreakers[name]
}

// getModelForProvider returns the model name used by a provider for metrics.
func (r *Registry) getModelForProvider(name ProviderName) string {
	switch name {
	case ProviderOpenAI:
		return ModelTextEmbedding3Large
	case ProviderCohere:
		return ModelEmbedMultilingualV3
	case ProviderGoogle:
		return ModelGeminiEmbedding001
	default:
		return "unknown"
	}
}
