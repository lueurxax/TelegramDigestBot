package domain

import "time"

// RawMessage represents a raw message from a Telegram channel.
type RawMessage struct {
	ID                      string
	ChannelID               string
	ChannelTitle            string
	ChannelContext          string
	ChannelDescription      string
	ChannelCategory         string
	ChannelTone             string
	ChannelUpdateFreq       string
	RelevanceThreshold      float32
	ImportanceThreshold     float32
	ImportanceWeight        float32
	AutoRelevanceEnabled    bool
	RelevanceThresholdDelta float32
	TGPeerID                int64
	TGMessageID             int64
	TGDate                  time.Time
	Text                    string
	PreviewText             string
	EntitiesJSON            []byte
	MediaJSON               []byte
	MediaData               []byte
	CanonicalHash           string
	IsForward               bool
}

// Item represents a processed digest item.
type Item struct {
	ID                 string
	RawMessageID       string
	RelevanceScore     float32
	ImportanceScore    float32
	Topic              string
	Summary            string
	Language           string
	LanguageSource     string
	Status             string
	ErrorJSON          []byte
	CreatedAt          time.Time
	FirstSeenAt        time.Time
	TGDate             time.Time
	SourceChannel      string
	SourceChannelTitle string
	SourceChannelID    int64
	SourceMsgID        int64
	Embedding          []float32
}

// ResolvedLink represents a resolved external or Telegram link.
type ResolvedLink struct {
	ID              string
	URL             string
	Domain          string
	LinkType        string
	Title           string
	Content         string
	Author          string
	PublishedAt     time.Time
	Description     string
	ImageURL        string
	WordCount       int
	ChannelUsername string
	ChannelTitle    string
	ChannelID       int64
	MessageID       int64
	Views           int
	Forwards        int
	HasMedia        bool
	MediaType       string
	Status          string
	ErrorMessage    string
	Language        string
	ResolvedAt      time.Time
	CreatedAt       time.Time
	ExpiresAt       time.Time
}

// Link status constants.
const (
	LinkStatusSuccess = "success"
	LinkStatusFailed  = "failed"
	LinkStatusPending = "pending"
)

// Link enrichment scope constants.
const (
	ScopeSummary   = "summary"
	ScopeRelevance = "relevance"
	ScopeTopic     = "topic"
	ScopeDedup     = "dedup"
	ScopeQueries   = "queries"
	ScopeFactCheck = "factcheck"
)

// LanguageRoutingPolicy defines how enrichment queries are routed to target languages.
type LanguageRoutingPolicy struct {
	Default []string            `json:"default"`
	Topic   map[string][]string `json:"topic"`
	Context []ContextPolicy     `json:"context"`
	Channel map[string][]string `json:"channel"`
}

// ContextPolicy defines language routing for a specific context keyword match.
type ContextPolicy struct {
	Name      string   `json:"name"`
	Languages []string `json:"languages"`
	Keywords  []string `json:"keywords"`
}

// ShortMessageThreshold is the character count threshold for a message to be considered short.
const ShortMessageThreshold = 120
