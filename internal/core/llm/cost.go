package llm

import "strings"

// Cost per 1M tokens (in USD) for various providers and models.
// These are approximate costs and should be updated as pricing changes.
// Reference: https://openai.com/pricing, https://www.anthropic.com/pricing, https://ai.google.dev/pricing
const (
	// OpenAI GPT-5 series (approximate based on GPT-4 pricing patterns)
	costGPT5PromptPer1M       = 2.50  // $2.50 per 1M prompt tokens
	costGPT5CompletionPer1M   = 10.00 // $10.00 per 1M completion tokens
	costGPT5NanoPromptPer1M   = 0.05  // $0.05 per 1M prompt tokens
	costGPT5NanoCompletePer1M = 0.40  // $0.40 per 1M completion tokens
	costGPT4OPromptPer1M      = 2.50  // $2.50 per 1M prompt tokens
	costGPT4OCompletionPer1M  = 10.00 // $10.00 per 1M completion tokens
	costGPT4OMiniPrompt       = 0.15  // $0.15 per 1M prompt tokens
	costGPT4OMiniComplete     = 0.60  // $0.60 per 1M completion tokens

	// Anthropic Claude 3.5 series
	costClaudeHaikuPrompt    = 1.00  // $1.00 per 1M prompt tokens
	costClaudeHaikuComplete  = 5.00  // $5.00 per 1M completion tokens
	costClaudeSonnetPrompt   = 3.00  // $3.00 per 1M prompt tokens
	costClaudeSonnetComplete = 15.00 // $15.00 per 1M completion tokens

	// Google Gemini
	costGeminiFlashPrompt   = 0.10  // $0.10 per 1M prompt tokens
	costGeminiFlashComplete = 0.40  // $0.40 per 1M completion tokens
	costGeminiProPrompt     = 3.50  // $3.50 per 1M prompt tokens
	costGeminiProComplete   = 10.50 // $10.50 per 1M completion tokens

	// Cohere
	costCohereCommandRPrompt   = 0.50 // $0.50 per 1M prompt tokens
	costCohereCommandRComplete = 1.50 // $1.50 per 1M completion tokens

	// OpenRouter - varies by model, using defaults
	costOpenRouterDefaultPrompt   = 1.00
	costOpenRouterDefaultComplete = 2.00

	// Conversion factor
	tokensPerMillion = 1000000.0
)

// estimateCost calculates an estimated cost for a request based on provider, model, and token counts.
// Returns cost in USD.
func estimateCost(provider, model string, promptTokens, completionTokens int) float64 {
	promptCost, completionCost := getCostRates(provider, model)

	promptUSD := float64(promptTokens) * promptCost / tokensPerMillion
	completionUSD := float64(completionTokens) * completionCost / tokensPerMillion

	return promptUSD + completionUSD
}

// getCostRates returns the cost per 1M tokens for prompt and completion based on provider and model.
func getCostRates(provider, model string) (promptRate, completionRate float64) {
	modelLower := strings.ToLower(model)

	switch provider {
	case "openai":
		return getOpenAICostRates(modelLower)
	case "anthropic":
		return getAnthropicCostRates(modelLower)
	case "google":
		return getGoogleCostRates(modelLower)
	case "cohere":
		return costCohereCommandRPrompt, costCohereCommandRComplete
	case "openrouter":
		return costOpenRouterDefaultPrompt, costOpenRouterDefaultComplete
	default:
		// Default fallback - use GPT-4o-mini rates as conservative estimate
		return costGPT4OMiniPrompt, costGPT4OMiniComplete
	}
}

// getOpenAICostRates returns cost rates for OpenAI models.
func getOpenAICostRates(model string) (float64, float64) {
	switch {
	case strings.Contains(model, modelPrefixGPT5) && strings.Contains(model, modelPrefixNano):
		return costGPT5NanoPromptPer1M, costGPT5NanoCompletePer1M
	case strings.Contains(model, modelPrefixGPT5):
		return costGPT5PromptPer1M, costGPT5CompletionPer1M
	case strings.Contains(model, "gpt-4o-mini"):
		return costGPT4OMiniPrompt, costGPT4OMiniComplete
	case strings.Contains(model, "gpt-4o"):
		return costGPT4OPromptPer1M, costGPT4OCompletionPer1M
	case strings.Contains(model, "gpt-4"):
		return costGPT4OPromptPer1M, costGPT4OCompletionPer1M
	default:
		// Default to GPT-4o-mini rates
		return costGPT4OMiniPrompt, costGPT4OMiniComplete
	}
}

// getAnthropicCostRates returns cost rates for Anthropic models.
func getAnthropicCostRates(model string) (float64, float64) {
	switch {
	case strings.Contains(model, "haiku"):
		return costClaudeHaikuPrompt, costClaudeHaikuComplete
	case strings.Contains(model, "sonnet"), strings.Contains(model, "opus"):
		return costClaudeSonnetPrompt, costClaudeSonnetComplete
	default:
		// Default to Haiku rates
		return costClaudeHaikuPrompt, costClaudeHaikuComplete
	}
}

// getGoogleCostRates returns cost rates for Google Gemini models.
func getGoogleCostRates(model string) (float64, float64) {
	switch {
	case strings.Contains(model, "pro"):
		return costGeminiProPrompt, costGeminiProComplete
	case strings.Contains(model, "flash"):
		return costGeminiFlashPrompt, costGeminiFlashComplete
	default:
		// Default to Flash rates
		return costGeminiFlashPrompt, costGeminiFlashComplete
	}
}
