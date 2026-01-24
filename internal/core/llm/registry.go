package llm

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/core/embeddings"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/observability"
)

// Registry errors.
var (
	ErrNoProvidersAvailable = errors.New("no LLM providers available")
	ErrAllProvidersFailed   = errors.New("all LLM providers failed")
	ErrNoImageProvider      = errors.New("no provider supports image generation")
)

// Registry manages LLM providers with fallback support.
type Registry struct {
	mu              sync.RWMutex
	providers       map[ProviderName]Provider
	order           []ProviderName // Priority order (highest first)
	circuitBreakers map[ProviderName]*embeddings.CircuitBreaker
	budgetTracker   *BudgetTracker
	logger          *zerolog.Logger
}

// NewRegistry creates a new provider registry.
func NewRegistry(logger *zerolog.Logger) *Registry {
	bt := NewBudgetTracker(0, logger) // 0 means no limit
	SetGlobalBudgetTracker(bt)

	return &Registry{
		providers:       make(map[ProviderName]Provider),
		order:           make([]ProviderName, 0),
		circuitBreakers: make(map[ProviderName]*embeddings.CircuitBreaker),
		budgetTracker:   bt,
		logger:          logger,
	}
}

// Register adds a provider to the registry.
func (r *Registry) Register(p Provider, cfg embeddings.CircuitBreakerConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := p.Name()
	r.providers[name] = p
	r.order = append(r.order, name)
	r.circuitBreakers[name] = embeddings.NewCircuitBreaker(cfg, r.logger)

	// Sort by priority (descending)
	r.sortProvidersByPriority()

	r.logger.Info().
		Str(logKeyProvider, string(name)).
		Int("priority", p.Priority()).
		Msg("registered LLM provider")
}

// ProviderCount returns the number of registered providers.
func (r *Registry) ProviderCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.providers)
}

// ProcessBatch implements Client interface with fallback.
func (r *Registry) ProcessBatch(ctx context.Context, messages []MessageInput, targetLanguage, model, tone string) ([]BatchResult, error) {
	return executeWithFallback(r, func(p Provider) ([]BatchResult, error) {
		return p.ProcessBatch(ctx, messages, targetLanguage, model, tone)
	})
}

// TranslateText implements Client interface with fallback.
func (r *Registry) TranslateText(ctx context.Context, text, targetLanguage, model string) (string, error) {
	return executeWithFallback(r, func(p Provider) (string, error) {
		return p.TranslateText(ctx, text, targetLanguage, model)
	})
}

// CompleteText implements Client interface with fallback.
func (r *Registry) CompleteText(ctx context.Context, prompt, model string) (string, error) {
	return executeWithFallback(r, func(p Provider) (string, error) {
		return p.CompleteText(ctx, prompt, model)
	})
}

// GenerateNarrative implements Client interface with fallback.
func (r *Registry) GenerateNarrative(ctx context.Context, items []domain.Item, targetLanguage, model, tone string) (string, error) {
	return executeWithFallback(r, func(p Provider) (string, error) {
		return p.GenerateNarrative(ctx, items, targetLanguage, model, tone)
	})
}

// GenerateNarrativeWithEvidence implements Client interface with fallback.
func (r *Registry) GenerateNarrativeWithEvidence(ctx context.Context, items []domain.Item, evidence ItemEvidence, targetLanguage, model, tone string) (string, error) {
	return executeWithFallback(r, func(p Provider) (string, error) {
		return p.GenerateNarrativeWithEvidence(ctx, items, evidence, targetLanguage, model, tone)
	})
}

// SummarizeCluster implements Client interface with fallback.
func (r *Registry) SummarizeCluster(ctx context.Context, items []domain.Item, targetLanguage, model, tone string) (string, error) {
	return executeWithFallback(r, func(p Provider) (string, error) {
		return p.SummarizeCluster(ctx, items, targetLanguage, model, tone)
	})
}

// SummarizeClusterWithEvidence implements Client interface with fallback.
func (r *Registry) SummarizeClusterWithEvidence(ctx context.Context, items []domain.Item, evidence ItemEvidence, targetLanguage, model, tone string) (string, error) {
	return executeWithFallback(r, func(p Provider) (string, error) {
		return p.SummarizeClusterWithEvidence(ctx, items, evidence, targetLanguage, model, tone)
	})
}

// GenerateClusterTopic implements Client interface with fallback.
func (r *Registry) GenerateClusterTopic(ctx context.Context, items []domain.Item, targetLanguage, model string) (string, error) {
	return executeWithFallback(r, func(p Provider) (string, error) {
		return p.GenerateClusterTopic(ctx, items, targetLanguage, model)
	})
}

// RelevanceGate implements Client interface with fallback.
func (r *Registry) RelevanceGate(ctx context.Context, text, model, prompt string) (RelevanceGateResult, error) {
	return executeWithFallback(r, func(p Provider) (RelevanceGateResult, error) {
		return p.RelevanceGate(ctx, text, model, prompt)
	})
}

// CompressSummariesForCover implements Client interface with fallback.
func (r *Registry) CompressSummariesForCover(ctx context.Context, summaries []string) ([]string, error) {
	return executeWithFallback(r, func(p Provider) ([]string, error) {
		return p.CompressSummariesForCover(ctx, summaries)
	})
}

// GenerateDigestCover implements Client interface.
// Only uses providers that support image generation.
func (r *Registry) GenerateDigestCover(ctx context.Context, topics []string, narrative string) ([]byte, error) {
	r.mu.RLock()
	providers := r.getActiveProviders()
	r.mu.RUnlock()

	var lastErr error

	for _, p := range providers {
		if !p.SupportsImageGeneration() {
			continue
		}

		cb := r.getCircuitBreaker(p.Name())
		if !cb.CanAttempt() {
			r.logger.Debug().
				Str(logKeyProvider, string(p.Name())).
				Msg(logMsgCircuitBreakerOpen)

			continue
		}

		result, err := p.GenerateDigestCover(ctx, topics, narrative)
		if err != nil {
			cb.RecordFailure(embeddings.ProviderName(p.Name()))

			lastErr = err

			r.logger.Warn().
				Err(err).
				Str(logKeyProvider, string(p.Name())).
				Msg("image generation failed")

			continue
		}

		cb.RecordSuccess()

		return result, nil
	}

	if lastErr != nil {
		return nil, errors.Join(ErrAllProvidersFailed, lastErr)
	}

	return nil, ErrNoImageProvider
}

// executeWithFallback is a generic helper for fallback execution.
func executeWithFallback[T any](r *Registry, fn func(Provider) (T, error)) (T, error) {
	r.mu.RLock()
	providers := r.getActiveProviders()
	r.mu.RUnlock()

	var zero T

	if len(providers) == 0 {
		return zero, ErrNoProvidersAvailable
	}

	var lastErr error

	for _, p := range providers {
		cb := r.getCircuitBreaker(p.Name())

		if !cb.CanAttempt() {
			r.logger.Debug().
				Str(logKeyProvider, string(p.Name())).
				Msg(logMsgCircuitBreakerOpen)

			continue
		}

		result, err := fn(p)
		if err != nil {
			cb.RecordFailure(embeddings.ProviderName(p.Name()))

			lastErr = err

			r.logger.Warn().
				Err(err).
				Str(logKeyProvider, string(p.Name())).
				Msg("LLM provider failed, trying fallback")

			continue
		}

		cb.RecordSuccess()

		// Log if we used a fallback provider
		if len(providers) > 1 && p.Name() != r.order[0] {
			r.logger.Info().
				Str(logKeyProvider, string(p.Name())).
				Msg("used fallback LLM provider")
		}

		return result, nil
	}

	if lastErr != nil {
		return zero, errors.Join(ErrAllProvidersFailed, lastErr)
	}

	return zero, ErrNoProvidersAvailable
}

// getActiveProviders returns providers that are available.
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
func (r *Registry) getCircuitBreaker(name ProviderName) *embeddings.CircuitBreaker {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.circuitBreakers[name]
}

// ProviderStatus holds status information for a provider.
type ProviderStatus struct {
	Name             ProviderName
	Priority         int
	Available        bool
	CircuitBreakerOK bool
}

// GetProviderStatuses returns status information for all registered providers.
func (r *Registry) GetProviderStatuses() []ProviderStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	statuses := make([]ProviderStatus, 0, len(r.order))

	for _, name := range r.order {
		p := r.providers[name]
		cb := r.circuitBreakers[name]

		statuses = append(statuses, ProviderStatus{
			Name:             name,
			Priority:         p.Priority(),
			Available:        p.IsAvailable(),
			CircuitBreakerOK: cb.CanAttempt(),
		})
	}

	return statuses
}

// globalBudgetTracker holds a reference to the active budget tracker for token recording.
//
//nolint:gochecknoglobals
var globalBudgetTracker *BudgetTracker

// SetGlobalBudgetTracker sets the global budget tracker reference.
func SetGlobalBudgetTracker(bt *BudgetTracker) {
	globalBudgetTracker = bt
}

// RecordTokenUsage records token usage metrics for an LLM request.
func RecordTokenUsage(provider, model, task string, promptTokens, completionTokens int, success bool) {
	status := StatusSuccess
	if !success {
		status = StatusError
	}

	observability.LLMRequests.WithLabelValues(provider, model, task, status).Inc()

	if promptTokens > 0 {
		observability.LLMTokensPrompt.WithLabelValues(provider, model, task).Add(float64(promptTokens))
	}

	if completionTokens > 0 {
		observability.LLMTokensCompletion.WithLabelValues(provider, model, task).Add(float64(completionTokens))
	}

	// Record to budget tracker if set
	if globalBudgetTracker != nil && success {
		totalTokens := promptTokens + completionTokens
		if totalTokens > 0 {
			globalBudgetTracker.RecordTokens(totalTokens)
		}
	}
}

// SetBudgetLimit sets the daily token budget limit.
func (r *Registry) SetBudgetLimit(limit int64) {
	r.budgetTracker.SetDailyLimit(limit)
}

// GetBudgetStatus returns the current budget status.
func (r *Registry) GetBudgetStatus() (dailyTokens, dailyLimit int64, percentage float64) {
	return r.budgetTracker.GetStatus()
}

// SetBudgetAlertCallback sets the callback for budget alerts.
func (r *Registry) SetBudgetAlertCallback(callback func(alert BudgetAlert)) {
	r.budgetTracker.SetAlertCallback(callback)
}

// RecordTokensForBudget records tokens for budget tracking.
func (r *Registry) RecordTokensForBudget(tokens int) {
	r.budgetTracker.RecordTokens(tokens)
}

// Ensure Registry implements Client interface.
var _ Client = (*Registry)(nil)
