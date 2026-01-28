package llm

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/platform/observability"
)

// UsageRecorder records token usage metrics for LLM requests.
// This interface allows for dependency injection and easier testing.
type UsageRecorder interface {
	RecordTokenUsage(provider, model, task string, promptTokens, completionTokens int, success bool)
}

// usageRecorder implements UsageRecorder with metrics, budget tracking, and persistence.
type usageRecorder struct {
	budgetTracker *BudgetTracker
	usageStore    UsageStore
	logger        *zerolog.Logger
}

// NewUsageRecorder creates a new UsageRecorder with the given dependencies.
func NewUsageRecorder(budgetTracker *BudgetTracker, usageStore UsageStore, logger *zerolog.Logger) UsageRecorder {
	return &usageRecorder{
		budgetTracker: budgetTracker,
		usageStore:    usageStore,
		logger:        logger,
	}
}

// RecordTokenUsage records token usage metrics for an LLM request.
func (r *usageRecorder) RecordTokenUsage(provider, model, task string, promptTokens, completionTokens int, success bool) {
	r.recordTokenMetrics(provider, model, task, promptTokens, completionTokens, success)

	cost := estimateCost(provider, model, promptTokens, completionTokens)
	r.recordCostMetric(provider, model, task, cost, success)
	r.recordToBudgetTracker(promptTokens, completionTokens, success)
	r.persistUsageToDatabase(provider, model, task, promptTokens, completionTokens, cost, success)
}

// recordTokenMetrics records Prometheus metrics for token usage.
func (r *usageRecorder) recordTokenMetrics(provider, model, task string, promptTokens, completionTokens int, success bool) {
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
func (r *usageRecorder) recordCostMetric(provider, model, task string, cost float64, success bool) {
	if cost > 0 && success {
		costMillicents := cost * usdToMillicents
		observability.LLMEstimatedCost.WithLabelValues(provider, model, task).Add(costMillicents)
	}
}

// recordToBudgetTracker records token usage to the budget tracker.
func (r *usageRecorder) recordToBudgetTracker(promptTokens, completionTokens int, success bool) {
	if r.budgetTracker == nil || !success {
		return
	}

	totalTokens := promptTokens + completionTokens
	if totalTokens > 0 {
		r.budgetTracker.RecordTokens(totalTokens)
	}
}

// persistUsageToDatabase stores usage in the database asynchronously.
func (r *usageRecorder) persistUsageToDatabase(provider, model, task string, promptTokens, completionTokens int, cost float64, success bool) {
	if r.usageStore == nil || !success {
		return
	}

	// Use background context since this is fire-and-forget.
	// Usage storage is best-effort and shouldn't fail the LLM request if it fails.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), usageStorageTimeout)
		defer cancel()

		//nolint:errcheck,gosec // fire-and-forget: errors are intentionally ignored
		r.usageStore.IncrementLLMUsage(ctx, provider, model, task, promptTokens, completionTokens, cost)
	}()
}

// noopUsageRecorder is a no-op implementation for testing or when usage tracking is disabled.
type noopUsageRecorder struct{}

// NoopUsageRecorder returns a no-op implementation of UsageRecorder.
func NoopUsageRecorder() UsageRecorder {
	return &noopUsageRecorder{}
}

// RecordTokenUsage does nothing (no-op implementation).
func (r *noopUsageRecorder) RecordTokenUsage(_, _, _ string, _, _ int, _ bool) {
	// No-op
}
