package mocks

import (
	"context"
	"sync"
)

// BudgetRepository is a thread-safe in-memory implementation of ports.BudgetRepository.
type BudgetRepository struct {
	mu           sync.RWMutex
	dailyCount   int
	monthlyCount int
	dailyCost    float64
	monthlyCost  float64
}

// NewBudgetRepository creates a new mock budget repository.
func NewBudgetRepository() *BudgetRepository {
	return &BudgetRepository{}
}

// GetDailyEnrichmentCount returns the daily enrichment count.
func (b *BudgetRepository) GetDailyEnrichmentCount(_ context.Context) (int, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.dailyCount, nil
}

// GetMonthlyEnrichmentCount returns the monthly enrichment count.
func (b *BudgetRepository) GetMonthlyEnrichmentCount(_ context.Context) (int, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.monthlyCount, nil
}

// GetDailyEnrichmentCost returns the daily enrichment cost.
func (b *BudgetRepository) GetDailyEnrichmentCost(_ context.Context) (float64, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.dailyCost, nil
}

// GetMonthlyEnrichmentCost returns the monthly enrichment cost.
func (b *BudgetRepository) GetMonthlyEnrichmentCost(_ context.Context) (float64, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.monthlyCost, nil
}

// IncrementEnrichmentUsage increments usage counters.
func (b *BudgetRepository) IncrementEnrichmentUsage(_ context.Context, _ string, cost float64) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.dailyCount++
	b.monthlyCount++
	b.dailyCost += cost
	b.monthlyCost += cost

	return nil
}

// IncrementEmbeddingUsage increments embedding cost.
func (b *BudgetRepository) IncrementEmbeddingUsage(_ context.Context, cost float64) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.dailyCost += cost
	b.monthlyCost += cost

	return nil
}

// SetCounts is a convenience method for tests to set counts directly.
func (b *BudgetRepository) SetCounts(daily, monthly int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.dailyCount = daily
	b.monthlyCount = monthly
}

// SetCosts is a convenience method for tests to set costs directly.
func (b *BudgetRepository) SetCosts(daily, monthly float64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.dailyCost = daily
	b.monthlyCost = monthly
}

// Reset clears all counters.
func (b *BudgetRepository) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.dailyCount = 0
	b.monthlyCount = 0
	b.dailyCost = 0
	b.monthlyCost = 0
}
