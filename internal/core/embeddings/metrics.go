package embeddings

import (
	"time"

	"github.com/lueurxax/telegram-digest-bot/internal/platform/observability"
)

// Metric status constants.
const (
	StatusSuccess = "success"
	StatusError   = "error"

	// Cost per 1M tokens (in USD) - approximate values.
	costOpenAILargePer1M = 0.13  // text-embedding-3-large
	costOpenAISmallPer1M = 0.02  // text-embedding-3-small
	costCoherePer1M      = 0.10  // embed-multilingual-v3.0
	costGooglePer1M      = 0.025 // gemini-embedding-001

	// Conversion factor.
	usdToMillicents  = 100000.0
	tokensPerMillion = 1000000.0
)

// RecordEmbeddingRequest records an embedding request metric.
func RecordEmbeddingRequest(provider, model string, success bool) {
	status := StatusSuccess
	if !success {
		status = StatusError
	}

	observability.EmbeddingRequests.WithLabelValues(provider, model, status).Inc()
}

// RecordEmbeddingTokens records embedding token usage.
func RecordEmbeddingTokens(provider, model string, tokens int) {
	if tokens > 0 {
		observability.EmbeddingTokens.WithLabelValues(provider, model).Add(float64(tokens))

		// Estimate and record cost
		cost := estimateEmbeddingCost(provider, model, tokens)
		if cost > 0 {
			costMillicents := cost * usdToMillicents
			observability.EmbeddingEstimatedCost.WithLabelValues(provider, model).Add(costMillicents)
		}
	}
}

// RecordEmbeddingLatency records embedding request latency.
func RecordEmbeddingLatency(provider, model string, duration time.Duration) {
	observability.EmbeddingLatency.WithLabelValues(provider, model).Observe(duration.Seconds())
}

// RecordEmbeddingFallback records a fallback event.
func RecordEmbeddingFallback(fromProvider, toProvider string) {
	observability.EmbeddingFallbacks.WithLabelValues(fromProvider, toProvider).Inc()
}

// SetEmbeddingProviderAvailable sets the availability status of a provider.
func SetEmbeddingProviderAvailable(provider string, available bool) {
	value := 0.0
	if available {
		value = 1.0
	}

	observability.EmbeddingProviderAvailable.WithLabelValues(provider).Set(value)
}

// estimateEmbeddingCost calculates the estimated cost in USD for embeddings.
func estimateEmbeddingCost(provider, model string, tokens int) float64 {
	var costPer1M float64

	switch provider {
	case string(ProviderOpenAI):
		if model == ModelTextEmbedding3Large {
			costPer1M = costOpenAILargePer1M
		} else {
			costPer1M = costOpenAISmallPer1M
		}
	case string(ProviderCohere):
		costPer1M = costCoherePer1M
	case string(ProviderGoogle):
		costPer1M = costGooglePer1M
	default:
		return 0
	}

	return (float64(tokens) / tokensPerMillion) * costPer1M
}

// estimateTokens estimates the number of tokens for a text.
// Uses a rough approximation of ~4 characters per token.
func estimateTokens(text string) int {
	const charsPerToken = 4
	return (len(text) + charsPerToken - 1) / charsPerToken
}
