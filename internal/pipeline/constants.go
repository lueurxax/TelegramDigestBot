package pipeline

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
