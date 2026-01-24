package solr

import "time"

// Config holds configuration for the Solr client.
type Config struct {
	// Enabled controls whether the Solr client is active.
	Enabled bool
	// BaseURL is the Solr collection URL, e.g., "http://solr:8983/solr/news".
	BaseURL string
	// Timeout is the HTTP request timeout.
	Timeout time.Duration
	// MaxResults is the default maximum number of search results.
	MaxResults int
}

// SearchResponse represents the Solr search response.
type SearchResponse struct {
	Response     ResponseBody         `json:"response"`
	FacetCounts  *FacetCounts         `json:"facet_counts,omitempty"`
	Highlighting map[string]Highlight `json:"highlighting,omitempty"`
}

// ResponseBody contains the main response data.
type ResponseBody struct {
	NumFound int        `json:"numFound"` //nolint:tagliatelle // Solr API field name
	Start    int        `json:"start"`
	Docs     []Document `json:"docs"`
}

// FacetCounts contains facet results.
type FacetCounts struct {
	FacetFields map[string][]interface{} `json:"facet_fields,omitempty"`
}

// Highlight contains highlighted snippets for a document.
type Highlight map[string][]string

// Document represents a Solr document.
// Fields are flexible to accommodate different document types.
type Document struct {
	// Core fields
	ID      string `json:"id"`
	Version int64  `json:"_version_,omitempty"` //nolint:tagliatelle // Solr internal field name

	// Common fields
	Source      string    `json:"source,omitempty"`
	URL         string    `json:"url,omitempty"`
	Title       string    `json:"title,omitempty"`
	Content     string    `json:"content,omitempty"`
	Description string    `json:"description,omitempty"`
	Language    string    `json:"language,omitempty"`
	Domain      string    `json:"domain,omitempty"`
	PublishedAt time.Time `json:"published_at,omitempty"`
	IndexedAt   time.Time `json:"indexed_at,omitempty"`

	// Telegram-specific fields
	TGPeerID    int64  `json:"tg_peer_id,omitempty"`
	TGMessageID int64  `json:"tg_message_id,omitempty"`
	ChannelName string `json:"channel_name,omitempty"`

	// Web crawl fields
	CrawlStatus string    `json:"crawl_status,omitempty"`
	CrawlDepth  int       `json:"crawl_depth,omitempty"`
	CrawledAt   time.Time `json:"crawled_at,omitempty"`
	ErrorMsg    string    `json:"error_msg,omitempty"`

	// Language-specific dynamic fields (populated during indexing)
	TitleEN   string `json:"title_en,omitempty"`
	TitleRU   string `json:"title_ru,omitempty"`
	ContentEN string `json:"content_en,omitempty"`
	ContentRU string `json:"content_ru,omitempty"`
}

// IndexDocument is a simplified document for indexing.
// It uses interface{} to allow flexible field population.
type IndexDocument map[string]interface{}

// NewIndexDocument creates a new IndexDocument with the given ID.
func NewIndexDocument(id string) IndexDocument {
	return IndexDocument{
		"id": id,
	}
}

// SetField sets a field on the document.
func (d IndexDocument) SetField(name string, value interface{}) IndexDocument {
	d[name] = value
	return d
}

// AtomicUpdate represents an atomic update operation.
type AtomicUpdate struct {
	ID     string                 `json:"id"`
	Fields map[string]UpdateField `json:"-"`
}

// UpdateField represents a single field update operation.
type UpdateField struct {
	Set interface{} `json:"set,omitempty"`
	Add interface{} `json:"add,omitempty"`
	Inc interface{} `json:"inc,omitempty"`
}

// CrawlStatus constants for the work queue.
const (
	CrawlStatusPending    = "pending"
	CrawlStatusProcessing = "processing"
	CrawlStatusDone       = "done"
	CrawlStatusError      = "error"
)

// DocumentSource constants.
const (
	SourceTelegram = "telegram"
	SourceWeb      = "web"
)
