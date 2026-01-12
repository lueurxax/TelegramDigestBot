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

// Format strings
const (
	toneFormatString       = " Tone: %s."
	logKeyIndex            = "index"
	logKeyTotal            = "total"
	indexedItemFormat      = "[%d] %s\n"
)

// Numeric constants
const (
	rateLimiterBurst    = 5
	truncateLengthShort = 500
	truncateLengthLong  = 1000
)

// Mock client constants
const (
	mockEmbeddingDimensions = 1536
	mockRelevanceScore      = 0.8
	mockImportanceScore     = 0.5
	mockConfidenceScore     = 0.5
)
