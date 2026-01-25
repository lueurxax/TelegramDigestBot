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
	taskConfig      map[TaskType]TaskProviderChain
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
		taskConfig:      DefaultTaskConfig(),
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

// ProcessBatch implements Client interface with task-aware fallback.
func (r *Registry) ProcessBatch(ctx context.Context, messages []MessageInput, targetLanguage, model, tone string) ([]BatchResult, error) {
	return executeWithTaskFallback(r, TaskTypeSummarize, model, func(p Provider, m string) ([]BatchResult, error) {
		return p.ProcessBatch(ctx, messages, targetLanguage, m, tone)
	})
}

// TranslateText implements Client interface with task-aware fallback.
func (r *Registry) TranslateText(ctx context.Context, text, targetLanguage, model string) (string, error) {
	return executeWithTaskFallback(r, TaskTypeTranslate, model, func(p Provider, m string) (string, error) {
		return p.TranslateText(ctx, text, targetLanguage, m)
	})
}

// CompleteText implements Client interface with task-aware fallback.
func (r *Registry) CompleteText(ctx context.Context, prompt, model string) (string, error) {
	return executeWithTaskFallback(r, TaskTypeComplete, model, func(p Provider, m string) (string, error) {
		return p.CompleteText(ctx, prompt, m)
	})
}

// GenerateNarrative implements Client interface with task-aware fallback.
func (r *Registry) GenerateNarrative(ctx context.Context, items []domain.Item, targetLanguage, model, tone string) (string, error) {
	return executeWithTaskFallback(r, TaskTypeNarrative, model, func(p Provider, m string) (string, error) {
		return p.GenerateNarrative(ctx, items, targetLanguage, m, tone)
	})
}

// GenerateNarrativeWithEvidence implements Client interface with task-aware fallback.
func (r *Registry) GenerateNarrativeWithEvidence(ctx context.Context, items []domain.Item, evidence ItemEvidence, targetLanguage, model, tone string) (string, error) {
	return executeWithTaskFallback(r, TaskTypeNarrative, model, func(p Provider, m string) (string, error) {
		return p.GenerateNarrativeWithEvidence(ctx, items, evidence, targetLanguage, m, tone)
	})
}

// SummarizeCluster implements Client interface with task-aware fallback.
func (r *Registry) SummarizeCluster(ctx context.Context, items []domain.Item, targetLanguage, model, tone string) (string, error) {
	return executeWithTaskFallback(r, TaskTypeClusterSummary, model, func(p Provider, m string) (string, error) {
		return p.SummarizeCluster(ctx, items, targetLanguage, m, tone)
	})
}

// SummarizeClusterWithEvidence implements Client interface with task-aware fallback.
func (r *Registry) SummarizeClusterWithEvidence(ctx context.Context, items []domain.Item, evidence ItemEvidence, targetLanguage, model, tone string) (string, error) {
	return executeWithTaskFallback(r, TaskTypeClusterSummary, model, func(p Provider, m string) (string, error) {
		return p.SummarizeClusterWithEvidence(ctx, items, evidence, targetLanguage, m, tone)
	})
}

// GenerateClusterTopic implements Client interface with task-aware fallback.
func (r *Registry) GenerateClusterTopic(ctx context.Context, items []domain.Item, targetLanguage, model string) (string, error) {
	return executeWithTaskFallback(r, TaskTypeClusterTopic, model, func(p Provider, m string) (string, error) {
		return p.GenerateClusterTopic(ctx, items, targetLanguage, m)
	})
}

// RelevanceGate implements Client interface with task-aware fallback.
func (r *Registry) RelevanceGate(ctx context.Context, text, model, prompt string) (RelevanceGateResult, error) {
	return executeWithTaskFallback(r, TaskTypeRelevanceGate, model, func(p Provider, m string) (RelevanceGateResult, error) {
		return p.RelevanceGate(ctx, text, m, prompt)
	})
}

// CompressSummariesForCover implements Client interface with task-aware fallback.
func (r *Registry) CompressSummariesForCover(ctx context.Context, summaries []string) ([]string, error) {
	return executeWithTaskFallback(r, TaskTypeCompress, "", func(p Provider, m string) ([]string, error) {
		return p.CompressSummariesForCover(ctx, summaries, m)
	})
}

// GenerateDigestCover implements Client interface.
// Uses task-aware fallback for image generation (OpenAI only).
func (r *Registry) GenerateDigestCover(ctx context.Context, topics []string, narrative string) ([]byte, error) {
	r.mu.RLock()
	taskChain, hasConfig := r.taskConfig[TaskTypeImageGen]
	r.mu.RUnlock()

	var providerModels []ProviderModel
	if hasConfig {
		providerModels = taskChain.GetProviderChain()
	}

	var lastErr error

	for _, pm := range providerModels {
		r.mu.RLock()
		p, exists := r.providers[pm.Provider]
		r.mu.RUnlock()

		if !exists || !p.IsAvailable() || !p.SupportsImageGeneration() {
			continue
		}

		cb := r.getCircuitBreaker(pm.Provider)
		if !cb.CanAttempt() {
			r.logger.Debug().
				Str(logKeyProvider, string(pm.Provider)).
				Msg(logMsgCircuitBreakerOpen)

			continue
		}

		result, err := p.GenerateDigestCover(ctx, topics, narrative)
		if err != nil {
			cb.RecordFailure(embeddings.ProviderName(pm.Provider))

			lastErr = err

			r.logger.Warn().
				Err(err).
				Str(logKeyProvider, string(pm.Provider)).
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

// getProviderChainForTask returns the provider/model chain for a task.
// It returns task-specific providers first, then falls back to all registered providers.
func (r *Registry) getProviderChainForTask(taskType TaskType) []ProviderModel {
	r.mu.RLock()
	taskChain, hasConfig := r.taskConfig[taskType]
	order := r.order
	r.mu.RUnlock()

	var providerModels []ProviderModel

	// Add task-specific providers first
	if hasConfig {
		providerModels = taskChain.GetProviderChain()
	}

	// Track which providers are already in the chain
	seen := make(map[ProviderName]bool)

	for _, pm := range providerModels {
		seen[pm.Provider] = true
	}

	// Add remaining registered providers as fallbacks
	for _, name := range order {
		if !seen[name] {
			providerModels = append(providerModels, ProviderModel{Provider: name, Model: ""})
			seen[name] = true
		}
	}

	return providerModels
}

// executeWithTaskFallback is a generic helper for task-aware fallback execution.
func executeWithTaskFallback[T any](r *Registry, taskType TaskType, modelOverride string, fn func(Provider, string) (T, error)) (T, error) {
	providerModels := r.getProviderChainForTask(taskType)

	var zero T

	if len(providerModels) == 0 {
		return zero, ErrNoProvidersAvailable
	}

	var lastErr error

	isFirstProvider := true

	for _, pm := range providerModels {
		result, success, err := tryProviderExec(r, pm, modelOverride, taskType, fn)
		if err != nil {
			lastErr = err
			isFirstProvider = false

			continue
		}

		if !success {
			continue
		}

		if !isFirstProvider {
			r.logger.Info().
				Str(logKeyProvider, string(pm.Provider)).
				Str(logKeyTask, string(taskType)).
				Msg("used fallback LLM provider")
		}

		return result, nil
	}

	if lastErr != nil {
		return zero, errors.Join(ErrAllProvidersFailed, lastErr)
	}

	return zero, ErrNoProvidersAvailable
}

// tryProviderExec attempts to execute function with a provider.
func tryProviderExec[T any](r *Registry, pm ProviderModel, modelOverride string, taskType TaskType, fn func(Provider, string) (T, error)) (T, bool, error) {
	var zero T

	r.mu.RLock()
	p, exists := r.providers[pm.Provider]
	r.mu.RUnlock()

	if !exists || !p.IsAvailable() {
		return zero, false, nil
	}

	cb := r.getCircuitBreaker(pm.Provider)

	if !cb.CanAttempt() {
		r.logger.Debug().
			Str(logKeyProvider, string(pm.Provider)).
			Str(logKeyTask, string(taskType)).
			Msg(logMsgCircuitBreakerOpen)

		return zero, false, nil
	}

	model := pm.Model
	if modelOverride != "" {
		model = modelOverride
	}

	result, err := fn(p, model)
	if err != nil {
		cb.RecordFailure(embeddings.ProviderName(pm.Provider))

		r.logger.Warn().
			Err(err).
			Str(logKeyProvider, string(pm.Provider)).
			Str(logKeyModel, model).
			Str(logKeyTask, string(taskType)).
			Msg("LLM provider failed, trying fallback")

		return zero, false, err
	}

	cb.RecordSuccess()

	return result, true, nil
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
