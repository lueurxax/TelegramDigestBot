package llm

// Link type constants (matching linkextract package values)
const (
	LinkTypeTelegram = "telegram"
	LinkTypeWeb      = "web"
)

// Error message templates
const (
	errRateLimiter          = "rate limiter error: %w"
	errOpenAIChatCompletion = "openai chat completion error: %w"
)

// Default topic when none is available
const (
	DefaultTopic = "General"
)

// Tone setting constants
const (
	ToneProfessional = "professional"
	ToneCasual       = "casual"
	ToneBrief        = "brief"
)
