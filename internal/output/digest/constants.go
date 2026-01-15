package digest

// Default topic for items without a topic
const (
	DefaultTopic = "General"
)

// Default source label for items without a source
const (
	DefaultSourceLabel = "Source"
)

// Rating type constants
const (
	RatingGood       = "good"
	RatingBad        = "bad"
	RatingIrrelevant = "irrelevant"
)

// Observability label constants
const (
	StatusError  = "error"
	StatusPosted = "posted"
)

// Log field name constants
const (
	LogFieldGlobalCount   = "global_count"
	LogFieldMinGlobal     = "min_global"
	LogFieldChannelID     = "channel_id"
	LogFieldChannel       = "channel"
	LogFieldUpdated       = "updated"
	LogFieldSkipped       = "skipped"
	LogFieldTotal         = "total"
	LogFieldCount         = "count"
	LogFieldTask          = "task"
	LogFieldCorrelationID = "correlation_id"
	LogFieldWindow        = "window"
	LogFieldStart         = "start"
	LogFieldEnd           = "end"
	LogFieldNetScore      = "net_score"
	LogFieldRatingCount   = "rating_count"
	LogFieldMsgID         = "msg_id"
)

// Error message constants
const (
	ErrMsgFailedToSaveDigestError = "failed to save digest error"
)

// Database setting key constants
const (
	SettingDigestLanguage      = "digest_language"
	SettingSmartLLMModel       = "smart_llm_model"
	SettingTargetChatID        = "target_chat_id"
	SettingImportanceThreshold = "importance_threshold"
	SettingRelevanceThreshold  = "relevance_threshold"
)

// Log message constants
const (
	MsgCouldNotGetDigestLanguage = "could not get digest_language from DB"
	MsgCouldNotGetSmartLLMModel  = "could not get smart_llm_model from DB"
	MsgFailedToProcessDigest     = "failed to process digest"
	msgFailedToProcessWindow     = "failed to process window"
)

// Time format constants
const (
	TimeFormatHourMinute = "15:04"
)

// Digest formatting constants
const (
	DigestSeparatorLine   = "━━━━━━━━━━━━━━━━━━━━━━\n"
	DigestTopicBorderTop  = "┌─────────────────────\n"
	DigestTopicBorderBot  = "└─────────────────────\n"
	DigestSourceSeparator = " • "
	FormatPrefixSummary   = "%s %s"
	DefaultTopicEmoji     = "📂"
	EmojiBreaking         = "🔴"
	EmojiNotable          = "📌"
	EmojiStandard         = "📝"
	EmojiBullet           = "•"
	DigestSourceVia       = "\n    ↳ <i>via %s</i>"
)

// Magic number constants for auto-weight calculation
const (
	AutoWeightDefaultMinMessages       = 10
	AutoWeightDefaultExpectedFrequency = 5.0
	AutoWeightDefaultMinWeight         = 0.5
	AutoWeightDefaultMaxWeight         = 1.5
	AutoWeightDefaultRollingDays       = 30
	AutoWeightInclusionFactor          = 0.4
	AutoWeightImportanceFactor         = 0.3
	AutoWeightConsistencyFactor        = 0.2
	AutoWeightSignalFactor             = 0.1
	AutoWeightBaseOffset               = 0.5
)

// Time conversion constants
const (
	HoursPerDay = 24
)

// Hash calculation constants
const (
	HashMultiplier = 31
)

// Default duration constants
const (
	DefaultTickIntervalMinutes = 10
	DefaultCatchupWindowHours  = 24
)

// Digest pool and threshold constants
const (
	DigestPoolMultiplier    = 3
	ImportanceScoreBreaking = 0.8
	ImportanceScoreNotable  = 0.6
	ImportanceScoreStandard = 0.4
)

// Clustering constants
const (
	ClusterMaxItemsLimit             = 500
	ClusterDefaultCoherenceThreshold = 0.7
)

// Normalized score constants (0-1 range)
const (
	MaxNormalizedScore     = 1.0
	PerfectSimilarityScore = 1.0
)

// Smart selection constants
const (
	SourceDiversityBonus = 0.1
	BacklogThreshold     = 100
)

// Log truncation constants
const (
	LogTruncateLength = 50
)
