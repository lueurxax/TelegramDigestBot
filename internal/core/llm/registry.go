package llm

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

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
	modelOverrides  map[TaskType]string // Per-task model overrides from config
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
		modelOverrides:  make(map[TaskType]string),
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

	// Track provider availability metric
	available := MetricValueUnavailable
	if p.IsAvailable() {
		available = MetricValueAvailable
	}

	observability.LLMProviderAvailable.WithLabelValues(string(name)).Set(available)

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

// SetTaskModelOverride sets a model override for a specific task type.
// When set, this model will be used instead of the default for that task.
func (r *Registry) SetTaskModelOverride(taskType TaskType, model string) {
	if model == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.modelOverrides[taskType] = model

	r.logger.Debug().
		Str(logKeyTask, string(taskType)).
		Str(logKeyModel, model).
		Msg("set task model override")
}

// getTaskModelOverride returns the model override for a task, if set.
func (r *Registry) getTaskModelOverride(taskType TaskType) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.modelOverrides[taskType]
}

// SettingsReader is an interface for reading settings from a database.
type SettingsReader interface {
	GetSetting(ctx context.Context, key string, target interface{}) error
}

// UsageStore is an interface for storing LLM usage data.
type UsageStore interface {
	IncrementLLMUsage(ctx context.Context, provider, model, task string, promptTokens, completionTokens int, cost float64) error
}

// DBSettingToTaskType maps database setting keys to TaskType.
var DBSettingToTaskType = map[string]TaskType{
	"llm_override_summarize": TaskTypeSummarize,
	"llm_override_cluster":   TaskTypeClusterSummary,
	"llm_override_narrative": TaskTypeNarrative,
	"llm_override_topic":     TaskTypeClusterTopic,
}

// LoadOverridesFromDB loads model overrides from database settings.
// This allows runtime configuration via bot commands like /llm set.
func (r *Registry) LoadOverridesFromDB(ctx context.Context, reader SettingsReader) {
	for settingKey, taskType := range DBSettingToTaskType {
		var model string
		if err := reader.GetSetting(ctx, settingKey, &model); err == nil && model != "" {
			r.SetTaskModelOverride(taskType, model)

			r.logger.Info().
				Str(logKeyTask, string(taskType)).
				Str(logKeyModel, model).
				Msg("loaded task model override from DB")
		}
	}
}

// RefreshOverride reloads a single override from the database.
// Called when a bot command updates a setting.
func (r *Registry) RefreshOverride(ctx context.Context, reader SettingsReader, settingKey string) {
	taskType, ok := DBSettingToTaskType[settingKey]
	if !ok {
		return
	}

	var model string
	if err := reader.GetSetting(ctx, settingKey, &model); err != nil {
		r.logger.Warn().Err(err).Str("setting", settingKey).Msg("failed to load override from DB")
		return
	}

	// Empty model means reset to default (delete override)
	if model == "" {
		r.mu.Lock()
		delete(r.modelOverrides, taskType)
		r.mu.Unlock()

		r.logger.Info().Str(logKeyTask, string(taskType)).Msg("cleared task model override")
	} else {
		r.SetTaskModelOverride(taskType, model)
	}
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

// ExtractBullets implements Client interface with task-aware fallback.
// It extracts key bullet points from a message for bulletized digest output.
func (r *Registry) ExtractBullets(ctx context.Context, input BulletExtractionInput, targetLanguage, model string) (BulletExtractionResult, error) {
	return executeWithTaskFallback(r, TaskTypeBulletExtract, model, func(p Provider, m string) (BulletExtractionResult, error) {
		return p.ExtractBullets(ctx, input, targetLanguage, m)
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

	// Check for config-level model override if no explicit override provided
	effectiveModelOverride := modelOverride
	if effectiveModelOverride == "" {
		effectiveModelOverride = r.getTaskModelOverride(taskType)
	}

	var lastErr error

	var previousProvider ProviderName

	isFirstProvider := true

	for _, pm := range providerModels {
		result, success, err := tryProviderExec(r, pm, effectiveModelOverride, taskType, fn)
		if err != nil {
			lastErr = err

			if isFirstProvider {
				previousProvider = pm.Provider
			}

			isFirstProvider = false

			continue
		}

		if !success {
			continue
		}

		if !isFirstProvider && previousProvider != "" {
			// Record fallback event
			observability.LLMFallbacks.WithLabelValues(
				string(previousProvider),
				string(pm.Provider),
				string(taskType),
			).Inc()

			r.logger.Info().
				Str(logKeyProvider, string(pm.Provider)).
				Str("from_provider", string(previousProvider)).
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
		// Track circuit breaker state as open and provider as unavailable
		observability.LLMCircuitBreakerState.WithLabelValues(string(pm.Provider)).Set(MetricValueCBOpen)
		observability.LLMProviderAvailable.WithLabelValues(string(pm.Provider)).Set(MetricValueUnavailable)

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

	// Track latency
	start := time.Now()

	result, err := fn(p, model)

	duration := time.Since(start)

	// Record latency metric
	observability.LLMRequestLatency.WithLabelValues(
		string(pm.Provider),
		model,
		string(taskType),
	).Observe(duration.Seconds())

	if err != nil {
		wasOpen := !cb.CanAttempt()
		cb.RecordFailure(embeddings.ProviderName(pm.Provider))
		isNowOpen := !cb.CanAttempt()

		// Track if circuit breaker just opened
		if !wasOpen && isNowOpen {
			observability.LLMCircuitBreakerOpens.WithLabelValues(string(pm.Provider)).Inc()
			observability.LLMCircuitBreakerState.WithLabelValues(string(pm.Provider)).Set(MetricValueCBOpen)
			observability.LLMProviderAvailable.WithLabelValues(string(pm.Provider)).Set(MetricValueUnavailable)
		}

		r.logger.Warn().
			Err(err).
			Str(logKeyProvider, string(pm.Provider)).
			Str(logKeyModel, model).
			Str(logKeyTask, string(taskType)).
			Float64("duration_seconds", duration.Seconds()).
			Msg("LLM provider failed, trying fallback")

		return zero, false, err
	}

	cb.RecordSuccess()

	// Circuit breaker recovered - mark as closed and provider as available
	observability.LLMCircuitBreakerState.WithLabelValues(string(pm.Provider)).Set(MetricValueCBClosed)
	observability.LLMProviderAvailable.WithLabelValues(string(pm.Provider)).Set(MetricValueAvailable)

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

// globalUsageStore holds a reference to the usage store for persisting token usage.
//
//nolint:gochecknoglobals
var globalUsageStore UsageStore

// SetGlobalBudgetTracker sets the global budget tracker reference.
func SetGlobalBudgetTracker(bt *BudgetTracker) {
	globalBudgetTracker = bt
}

// SetGlobalUsageStore sets the global usage store reference.
func SetGlobalUsageStore(store UsageStore) {
	globalUsageStore = store
}

// RecordTokenUsage records token usage metrics for an LLM request.
func RecordTokenUsage(provider, model, task string, promptTokens, completionTokens int, success bool) {
	recordTokenMetrics(provider, model, task, promptTokens, completionTokens, success)

	cost := estimateCost(provider, model, promptTokens, completionTokens)
	recordCostMetric(provider, model, task, cost, success)
	recordToBudgetTracker(promptTokens, completionTokens, success)
	persistUsageToDatabase(provider, model, task, promptTokens, completionTokens, cost, success)
}

// recordTokenMetrics records Prometheus metrics for token usage.
func recordTokenMetrics(provider, model, task string, promptTokens, completionTokens int, success bool) {
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
}

// recordCostMetric records the estimated cost metric in millicents.
func recordCostMetric(provider, model, task string, cost float64, success bool) {
	if cost > 0 && success {
		costMillicents := cost * usdToMillicents
		observability.LLMEstimatedCost.WithLabelValues(provider, model, task).Add(costMillicents)
	}
}

// recordToBudgetTracker records token usage to the budget tracker.
func recordToBudgetTracker(promptTokens, completionTokens int, success bool) {
	if globalBudgetTracker == nil || !success {
		return
	}

	totalTokens := promptTokens + completionTokens
	if totalTokens > 0 {
		globalBudgetTracker.RecordTokens(totalTokens)
	}
}

// persistUsageToDatabase stores usage in the database asynchronously.
func persistUsageToDatabase(provider, model, task string, promptTokens, completionTokens int, cost float64, success bool) {
	if globalUsageStore == nil || !success {
		return
	}

	// Use background context since this is fire-and-forget.
	// The context is intentionally not passed from callers because:
	// 1. RecordTokenUsage is called synchronously and should not block on DB writes
	// 2. Usage storage is best-effort and shouldn't fail the LLM request if it fails
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), usageStorageTimeout)
		defer cancel()

		//nolint:errcheck,gosec // fire-and-forget: errors are intentionally ignored
		globalUsageStore.IncrementLLMUsage(ctx, provider, model, task, promptTokens, completionTokens, cost)
	}()
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
