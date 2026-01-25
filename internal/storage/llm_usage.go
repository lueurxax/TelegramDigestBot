package db

import (
	"context"
	"fmt"
	"time"
)

// LLMUsage represents daily usage statistics for LLM providers.
type LLMUsage struct {
	Date             string
	Provider         string
	Model            string
	Task             string
	PromptTokens     int
	CompletionTokens int
	RequestCount     int
	CostUSD          float64
}

// LLMUsageSummary provides aggregated usage statistics.
type LLMUsageSummary struct {
	TotalPromptTokens     int64
	TotalCompletionTokens int64
	TotalRequests         int64
	TotalCostUSD          float64
	ByProvider            map[string]ProviderUsage
	ByTask                map[string]TaskUsage
}

// ProviderUsage holds usage for a single provider.
type ProviderUsage struct {
	Provider         string
	PromptTokens     int64
	CompletionTokens int64
	RequestCount     int64
	CostUSD          float64
}

// TaskUsage holds usage for a single task type.
type TaskUsage struct {
	Task             string
	PromptTokens     int64
	CompletionTokens int64
	RequestCount     int64
	CostUSD          float64
}

// IncrementLLMUsage increments LLM usage counters for the current day.
func (db *DB) IncrementLLMUsage(ctx context.Context, provider, model, task string, promptTokens, completionTokens int, cost float64) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO llm_usage (date, provider, model, task, prompt_tokens, completion_tokens, request_count, cost_usd)
		VALUES (CURRENT_DATE, $1, $2, $3, $4, $5, 1, $6)
		ON CONFLICT (date, provider, model, task)
		DO UPDATE SET
			prompt_tokens = llm_usage.prompt_tokens + EXCLUDED.prompt_tokens,
			completion_tokens = llm_usage.completion_tokens + EXCLUDED.completion_tokens,
			request_count = llm_usage.request_count + 1,
			cost_usd = llm_usage.cost_usd + EXCLUDED.cost_usd,
			updated_at = now()
	`, provider, model, task, promptTokens, completionTokens, cost)
	if err != nil {
		return fmt.Errorf("increment llm usage: %w", err)
	}

	return nil
}

// GetDailyLLMUsage returns LLM usage for the current day.
func (db *DB) GetDailyLLMUsage(ctx context.Context) (*LLMUsageSummary, error) {
	return db.getLLMUsageSince(ctx, "date = CURRENT_DATE")
}

// GetMonthlyLLMUsage returns LLM usage for the current month.
func (db *DB) GetMonthlyLLMUsage(ctx context.Context) (*LLMUsageSummary, error) {
	return db.getLLMUsageSince(ctx, "date >= DATE_TRUNC('month', CURRENT_DATE)")
}

// GetLLMUsageSince returns LLM usage since a given time.
func (db *DB) GetLLMUsageSince(ctx context.Context, since time.Time) (*LLMUsageSummary, error) {
	return db.getLLMUsageSince(ctx, fmt.Sprintf("date >= '%s'", since.Format("2006-01-02")))
}

// getLLMUsageSince is a helper that fetches and aggregates LLM usage.
func (db *DB) getLLMUsageSince(ctx context.Context, whereClause string) (*LLMUsageSummary, error) {
	query := fmt.Sprintf(`
		SELECT provider, model, task,
			   COALESCE(SUM(prompt_tokens), 0)::bigint,
			   COALESCE(SUM(completion_tokens), 0)::bigint,
			   COALESCE(SUM(request_count), 0)::bigint,
			   COALESCE(SUM(cost_usd), 0)::numeric
		FROM llm_usage
		WHERE %s
		GROUP BY provider, model, task
	`, whereClause)

	rows, err := db.Pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("get llm usage: %w", err)
	}
	defer rows.Close()

	summary := &LLMUsageSummary{
		ByProvider: make(map[string]ProviderUsage),
		ByTask:     make(map[string]TaskUsage),
	}

	for rows.Next() {
		var (
			provider         string
			model            string
			task             string
			promptTokens     int64
			completionTokens int64
			requestCount     int64
			costUSD          float64
		)

		if err := rows.Scan(&provider, &model, &task, &promptTokens, &completionTokens, &requestCount, &costUSD); err != nil {
			return nil, fmt.Errorf("scan llm usage row: %w", err)
		}

		// Aggregate totals
		summary.TotalPromptTokens += promptTokens
		summary.TotalCompletionTokens += completionTokens
		summary.TotalRequests += requestCount
		summary.TotalCostUSD += costUSD

		// Aggregate by provider
		provUsage := summary.ByProvider[provider]
		provUsage.Provider = provider
		provUsage.PromptTokens += promptTokens
		provUsage.CompletionTokens += completionTokens
		provUsage.RequestCount += requestCount
		provUsage.CostUSD += costUSD
		summary.ByProvider[provider] = provUsage

		// Aggregate by task
		taskUsage := summary.ByTask[task]
		taskUsage.Task = task
		taskUsage.PromptTokens += promptTokens
		taskUsage.CompletionTokens += completionTokens
		taskUsage.RequestCount += requestCount
		taskUsage.CostUSD += costUSD
		summary.ByTask[task] = taskUsage
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate llm usage rows: %w", rows.Err())
	}

	return summary, nil
}

// GetLLMUsageDetails returns detailed LLM usage for a date range.
func (db *DB) GetLLMUsageDetails(ctx context.Context, since time.Time) ([]LLMUsage, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT date::text, provider, model, task, prompt_tokens, completion_tokens, request_count, cost_usd
		FROM llm_usage
		WHERE date >= $1
		ORDER BY date DESC, provider, model, task
	`, since)
	if err != nil {
		return nil, fmt.Errorf("get llm usage details: %w", err)
	}
	defer rows.Close()

	var usages []LLMUsage

	for rows.Next() {
		var u LLMUsage

		if err := rows.Scan(&u.Date, &u.Provider, &u.Model, &u.Task, &u.PromptTokens, &u.CompletionTokens, &u.RequestCount, &u.CostUSD); err != nil {
			return nil, fmt.Errorf("scan llm usage detail row: %w", err)
		}

		usages = append(usages, u)
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate llm usage detail rows: %w", rows.Err())
	}

	return usages, nil
}
