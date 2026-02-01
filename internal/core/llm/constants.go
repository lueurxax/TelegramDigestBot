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
	logKeyTask         = "task"
	logKeyModel        = "model"
	logKeyMaxTokens    = "max_tokens"
	logKeyOutputTokens = "output_tokens"
)

// Log message strings
const (
	logMsgTruncated = "LLM output truncated due to max_tokens limit"
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

// Bullet extraction defaults
const (
	fallbackBulletScore = 0.5
	defaultMaxBullets   = 3
)

// Bullet extraction prompts
const (
	bulletContextFormat = "\n[%s]\n%s"

	bulletExtractionPrompt = `Identify the %d most important self-contained claims.
Each claim must be understandable without additional context.
Target language for output: %s
Output must be ONLY in the target language; translate if the source is different.

If [PRIMARY ARTICLE] is provided, extract claims from it and use MESSAGE only for context.
If only [SUPPLEMENTAL LINK] is provided, extract claims from MESSAGE and use the link only to clarify details.

MESSAGE: %s%s

Return ONLY a JSON array (no markdown, no explanation):
[{"text": "claim text", "relevance_score": 0.0-1.0, "importance_score": 0.0-1.0, "topic": "short topic"}]

Guidelines:
- Each claim should be a complete, standalone statement
- relevance_score: how relevant to the channel's typical content (0.0-1.0)
- importance_score: how newsworthy or significant (0.0-1.0)
- topic: 1-3 word category (e.g., "Politics", "Technology", "Economy")
- If text has fewer claims than requested, return fewer bullets
- Return empty array [] if no meaningful claims can be extracted`

	logMsgBulletParseError = "failed to parse bullet extraction response"
)

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
	LogKeyCount    = "count"
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
	TaskBulletExtract = "bullet_extract"
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
