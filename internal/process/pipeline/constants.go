package pipeline

import "time"

// Item status constants
const (
	StatusReady    = "ready"
	StatusRejected = "rejected"
	StatusError    = "error"
)

// Relevance gate decision constants
const (
	DecisionRelevant   = "relevant"
	DecisionIrrelevant = "irrelevant"
)

// Relevance gate reason constants
const (
	ReasonEmpty    = "empty"
	ReasonLinkOnly = "link_only"
	ReasonNoText   = "no_text"
	ReasonPassed   = "passed"
)

// Deduplication mode constants
const (
	DedupModeSemantic = "semantic"
	DedupModeStrict   = "strict"
)

// Default filter mode
const (
	FilterModeMixed = "mixed"
)

// Log field constants
const (
	LogFieldMsgID         = "msg_id"
	LogFieldCorrelationID = "correlation_id"
	LogFieldDuplicateID   = "duplicate_id"
	LogFieldCount         = "count"
	LogFieldModel         = "model"
	LogFieldTask          = "task"
	LogFieldItemID        = "item_id"
	LogFieldLinkID        = "link_id"
	LogFieldHours         = "hours"
	LogFieldLimit         = "limit"
)

// Setting key constants
const (
	SettingMaxLinksPerMessage = "max_links_per_message"
	SettingLinkCacheTTL       = "link_cache_ttl"
	SettingTgLinkCacheTTL     = "tg_link_cache_ttl"
)

// Log message constants
const (
	LogMsgFailedToMarkProcessed = "failed to mark message as processed"
)

// Magic number constants for pipeline configuration
const (
	DefaultPollInterval        = 10 * time.Second
	DefaultMinLength           = 20
	DefaultChannelContextLimit = 5
	TieredImportanceThreshold  = 0.8
	MinChannelWeight           = 0.1
	MaxChannelWeight           = 2.0
	MaxImportanceScore         = 1.0
	UniqueInfoPenalty          = 0.2
	NormalizationStddevMinimum = 0.01

	// Deduplication window defaults
	DefaultDedupWindowHours            = 36
	DefaultDedupSameChannelWindowHours = 6
)

// Recovery constants for stuck message handling
const (
	// StuckMessageThreshold is how long a message can be claimed before considered stuck.
	// Should be longer than the typical batch processing time (~2-3 minutes).
	StuckMessageThreshold = 10 * time.Minute

	// RecoveryInterval is how often to check for and recover stuck messages.
	RecoveryInterval = 5 * time.Minute
)

// Timeout constants for pipeline processing
const (
	// LLMBatchTimeout is the maximum time to wait for LLM batch processing.
	// Should be generous enough to handle large batches but prevent indefinite hangs.
	LLMBatchTimeout = 5 * time.Minute
)

// Relevance gate confidence constants
const (
	ConfidenceEmpty    float32 = 1.0
	ConfidenceLinkOnly float32 = 0.9
	ConfidenceNoText   float32 = 0.8
	ConfidencePassed   float32 = 0.6
)
