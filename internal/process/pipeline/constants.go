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
	LogFieldItemID        = "item_id"
)

// Log message constants
const (
	LogMsgFailedToMarkProcessed = "failed to mark message as processed"
)

// Magic number constants for pipeline configuration
const (
	DefaultPollInterval         = 10 * time.Second
	DefaultMinLength            = 20
	DefaultChannelContextLimit  = 5
	TieredImportanceThreshold   = 0.8
	MinChannelWeight            = 0.1
	MaxChannelWeight            = 2.0
	MaxImportanceScore          = 1.0
	UniqueInfoPenalty           = 0.2
	NormalizationStddevMinimum  = 0.01
)

// Relevance gate confidence constants
const (
	ConfidenceEmpty    float32 = 1.0
	ConfidenceLinkOnly float32 = 0.9
	ConfidenceNoText   float32 = 0.8
	ConfidencePassed   float32 = 0.6
)
