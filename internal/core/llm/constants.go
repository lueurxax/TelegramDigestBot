package llm

import "time"

// Link type constants (matching linkextract package values)
const (
	LinkTypeTelegram = "telegram"
	LinkTypeWeb      = "web"
)

// Error message templates
const (
	errRateLimiter           = "rate limiter error: %w"
	errRateLimiterSimple     = "rate limiter: %w"
	errOpenAIChatCompletion  = "openai chat completion error: %w"
	errGoogleGenAICompletion = "google genai completion: %w"
	errParseResponse         = "failed to parse response: %w"
)

// Model mapping strings
const (
	modelPrefixGPT4   = "gpt-4"
	modelPrefixGPT5   = "gpt-5"
	modelPrefixNano   = "nano"
	modelPrefixMini   = "mini"
	modelPrefixClaude = "claude"
	modelPrefixGemini = "gemini"
	llmAPIKeyMock     = "mock"
)

// Prompt format strings
const (
	indexedPrefixFormat = "[%d] "
	sourceChannelFormat = "(Source: %s) "
	translatePromptFmt  = "Translate to %s. Output ONLY the translation, nothing else. The output must be in %s language.\n\n%s"
	relevanceGateFormat = "%s\n\nText to evaluate:\n%s\n\nRespond in JSON format with fields: decision (relevant/irrelevant), confidence (0-1), reason"
)

// Log message strings
const (
	logMsgCircuitBreakerOpen     = "skipping provider - circuit breaker open"
	logMsgEvidenceHeader         = "   [Supporting Evidence:"
	logMsgParseRelevanceGateFail = "failed to parse relevance gate response"
)

// Log key strings
const (
	logKeyTask  = "task"
	logKeyModel = "model"
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
	toneFormatString  = " Tone: %s."
	logKeyIndex       = "index"
	logKeyTotal       = "total"
	logKeyResponse    = "response"
	indexedItemFormat = "[%d] %s\n"
)

// HTTP header values
const (
	contentTypeJSON     = "application/json"
	contentTypeText     = "text"
	headerAuthorization = "Authorization"
	headerContentType   = "Content-Type"
)

// Error format strings for API clients
const (
	errFmtMarshalRequest = "marshal request: %w"
	errFmtCreateRequest  = "create request: %w"
	errFmtReadResponse   = "read response: %w"
	errFmtDecodeResponse = "decode response: %w"
	errFmtAPIWithMessage = "%w (%d): %s"
	errFmtAPIStatusOnly  = "%w: status %d"
	errFmtContextWrap    = "%s: %w"
)

// Numeric constants
const (
	rateLimiterBurst    = 5
	truncateLengthShort = 500
	truncateLengthLong  = 1000
)

// Mock client constants
const (
	mockRelevanceScore  = 0.8
	mockImportanceScore = 0.5
	mockConfidenceScore = 0.5
)

// Fallback score for bullet extraction stubs
const fallbackBulletScore = 0.5

// Circuit breaker defaults
const (
	defaultCircuitThreshold = 5
	defaultCircuitTimeout   = time.Minute
)

// Usage storage timeout
const (
	usageStorageTimeout = 5 * time.Second
)

// Cost conversion
const (
	usdToMillicents = 100000.0 // 1 USD = 100,000 millicents
)

// Log field keys
const (
	logKeyProvider = "provider"
)

// LLM task names for metrics tracking.
const (
	TaskSummarize     = "summarize"
	TaskTranslate     = "translate"
	TaskComplete      = "complete"
	TaskNarrative     = "narrative"
	TaskCluster       = "cluster"
	TaskTopic         = "topic"
	TaskRelevanceGate = "relevance_gate"
	TaskCompress      = "compress"
	TaskImageGen      = "image_gen"
)

// Request status for metrics.
const (
	StatusSuccess = "success"
	StatusError   = "error"
)

// Metric gauge values.
const (
	MetricValueAvailable   = 1.0
	MetricValueUnavailable = 0.0
	MetricValueCBOpen      = 1.0 // Circuit breaker is open (blocking requests)
	MetricValueCBClosed    = 0.0 // Circuit breaker is closed (allowing requests)
)
