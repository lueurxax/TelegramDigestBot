// Package solr provides a client for interacting with Apache Solr.
//
// The Client is used for:
//   - Crawler work queue management (pending URLs, crawled content)
//   - Dual-write indexing of Telegram messages
//   - Full-text search for enrichment queries
//
// The client handles JSON serialization, error handling, and retries.
package solr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultTimeout      = 10 * time.Second
	defaultMaxResults   = 10
	healthCheckTimeout  = 5 * time.Second
	selectPath          = "/select"
	updatePath          = "/update"
	getPath             = "/get"
	contentTypeJSON     = "application/json"
	contentTypeForm     = "application/x-www-form-urlencoded"
	headerContentType   = "Content-Type"
	httpStatusConflict  = 409
	maxResponseBodySize = 10 * 1024 * 1024 // 10MB
	errBodyReadLimit    = 1024
	errStatusBodyFmt    = "%w: status %d, body: %s"
	errStatusFmt        = "%w: status %d"
	maxURILength        = 4096 // Use POST for queries longer than this
)

// Client provides methods to interact with a SolrCloud collection.
type Client struct {
	baseURL    string
	httpClient *http.Client
	maxResults int
	enabled    bool
}

// New creates a new Solr client with the given configuration.
func New(cfg Config) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	maxResults := cfg.MaxResults
	if maxResults <= 0 {
		maxResults = defaultMaxResults
	}

	return &Client{
		baseURL:    strings.TrimSuffix(cfg.BaseURL, "/"),
		maxResults: maxResults,
		enabled:    cfg.BaseURL != "", // Enabled if BaseURL is configured
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Enabled returns whether the client is enabled.
func (c *Client) Enabled() bool {
	return c.enabled
}

// Ping checks if Solr is reachable and the collection exists.
func (c *Client) Ping(ctx context.Context) error {
	if !c.enabled {
		return ErrClientDisabled
	}

	ctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	// Use admin/ping endpoint
	pingURL := c.baseURL + "/admin/ping"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pingURL, nil)
	if err != nil {
		return fmt.Errorf("create ping request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ping request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf(errStatusFmt, ErrServerError, resp.StatusCode)
	}

	return nil
}

// Search executes a search query and returns matching documents.
// Uses GET for short queries, POST for long queries to avoid URI length limits.
func (c *Client) Search(ctx context.Context, query string, opts ...SearchOption) (*SearchResponse, error) {
	if !c.enabled {
		return nil, ErrClientDisabled
	}

	params := &searchParams{
		q:    query,
		rows: c.maxResults,
	}

	for _, opt := range opts {
		opt(params)
	}

	var req *http.Request

	var err error

	searchURL := c.buildSearchURL(params)

	// Use POST for long queries to avoid URI length limits (HTTP 414)
	if len(searchURL) > maxURILength {
		req, err = c.buildSearchPOSTRequest(ctx, params)
	} else {
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	}

	if err != nil {
		return nil, fmt.Errorf("create search request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, errBodyReadLimit))
		if readErr != nil {
			return nil, fmt.Errorf(errStatusFmt, ErrServerError, resp.StatusCode)
		}

		return nil, fmt.Errorf(errStatusBodyFmt, ErrServerError, resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize))
	if err != nil {
		return nil, fmt.Errorf("read search response: %w", err)
	}

	var result SearchResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse search response: %w", err)
	}

	return &result, nil
}

// buildSearchPOSTRequest creates a POST request for Solr search.
// Used when query parameters exceed URI length limits.
func (c *Client) buildSearchPOSTRequest(ctx context.Context, params *searchParams) (*http.Request, error) {
	postURL := c.baseURL + selectPath

	// Build form data
	formData := c.buildSearchParams(params)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, postURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create POST request: %w", err)
	}

	req.Header.Set(headerContentType, contentTypeForm)

	return req, nil
}

// Get retrieves a document by its ID.
func (c *Client) Get(ctx context.Context, id string) (*Document, error) {
	if !c.enabled {
		return nil, ErrClientDisabled
	}

	getURL := c.baseURL + getPath + "?id=" + url.QueryEscape(id)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, getURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create get request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(errStatusFmt, ErrServerError, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize))
	if err != nil {
		return nil, fmt.Errorf("read get response: %w", err)
	}

	// Solr real-time get returns {"doc": {...}}
	var result struct {
		Doc *Document `json:"doc"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse get response: %w", err)
	}

	if result.Doc == nil {
		return nil, ErrNotFound
	}

	return result.Doc, nil
}

// Index adds or updates documents in the collection.
func (c *Client) Index(ctx context.Context, docs ...IndexDocument) error {
	if !c.enabled {
		return ErrClientDisabled
	}

	if len(docs) == 0 {
		return nil
	}

	return c.sendUpdate(ctx, docs, true)
}

// AtomicUpdate performs an atomic update on a document.
// Only the specified fields are modified.
func (c *Client) AtomicUpdate(ctx context.Context, id string, fields map[string]interface{}) error {
	if !c.enabled {
		return ErrClientDisabled
	}

	update := make(map[string]interface{})
	update["id"] = id

	for field, value := range fields {
		update[field] = map[string]interface{}{"set": value}
	}

	return c.sendUpdate(ctx, []interface{}{update}, true)
}

// ConditionalUpdate performs an optimistic locking update using the _version_ field.
// Returns ErrVersionConflict if the document was modified since the given version.
func (c *Client) ConditionalUpdate(ctx context.Context, id string, version int64, fields map[string]interface{}) error {
	if !c.enabled {
		return ErrClientDisabled
	}

	update := make(map[string]interface{})
	update["id"] = id
	update["_version_"] = version

	for field, value := range fields {
		update[field] = map[string]interface{}{"set": value}
	}

	updateURL := c.baseURL + updatePath + "?commit=true"

	body, err := json.Marshal([]interface{}{update})
	if err != nil {
		return fmt.Errorf("marshal conditional update: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, updateURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create conditional update request: %w", err)
	}

	req.Header.Set(headerContentType, contentTypeJSON)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("conditional update request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == httpStatusConflict {
		return ErrVersionConflict
	}

	if resp.StatusCode != http.StatusOK {
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, errBodyReadLimit))
		if readErr != nil {
			return fmt.Errorf(errStatusFmt, ErrServerError, resp.StatusCode)
		}

		return fmt.Errorf(errStatusBodyFmt, ErrServerError, resp.StatusCode, string(respBody))
	}

	return nil
}

// Delete removes documents by their IDs.
func (c *Client) Delete(ctx context.Context, ids ...string) error {
	if !c.enabled {
		return ErrClientDisabled
	}

	if len(ids) == 0 {
		return nil
	}

	deleteCmd := map[string]interface{}{
		"delete": ids,
	}

	body, err := json.Marshal(deleteCmd)
	if err != nil {
		return fmt.Errorf("marshal delete: %w", err)
	}

	updateURL := c.baseURL + updatePath + "?commit=true"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, updateURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create delete request: %w", err)
	}

	req.Header.Set(headerContentType, contentTypeJSON)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, errBodyReadLimit))
		if readErr != nil {
			return fmt.Errorf(errStatusFmt, ErrServerError, resp.StatusCode)
		}

		return fmt.Errorf(errStatusBodyFmt, ErrServerError, resp.StatusCode, string(respBody))
	}

	return nil
}

// DeleteByQuery removes documents matching a query.
func (c *Client) DeleteByQuery(ctx context.Context, query string) error {
	if !c.enabled {
		return ErrClientDisabled
	}

	deleteCmd := map[string]interface{}{
		"delete": map[string]string{
			"query": query,
		},
	}

	body, err := json.Marshal(deleteCmd)
	if err != nil {
		return fmt.Errorf("marshal delete by query: %w", err)
	}

	updateURL := c.baseURL + updatePath + "?commit=true"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, updateURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create delete by query request: %w", err)
	}

	req.Header.Set(headerContentType, contentTypeJSON)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete by query request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, errBodyReadLimit))
		if readErr != nil {
			return fmt.Errorf(errStatusFmt, ErrServerError, resp.StatusCode)
		}

		return fmt.Errorf(errStatusBodyFmt, ErrServerError, resp.StatusCode, string(respBody))
	}

	return nil
}

// ClaimURL attempts to claim a URL for crawling using optimistic locking.
// Returns the document if claimed successfully, ErrVersionConflict if already claimed,
// or ErrNotFound if the URL is not in the queue.
func (c *Client) ClaimURL(ctx context.Context, docID string, claimedUntil time.Time) (*Document, error) {
	if !c.enabled {
		return nil, ErrClientDisabled
	}

	// First get the document with its current version
	doc, err := c.Get(ctx, docID)
	if err != nil {
		return nil, err
	}

	// Check if it's already being processed
	if doc.CrawlStatus == CrawlStatusProcessing {
		return nil, ErrVersionConflict
	}

	// Try to claim it with optimistic locking
	err = c.ConditionalUpdate(ctx, docID, doc.Version, map[string]interface{}{
		"crawl_status": CrawlStatusProcessing,
		"crawled_at":   claimedUntil,
	})
	if err != nil {
		return nil, err
	}

	doc.CrawlStatus = CrawlStatusProcessing
	doc.CrawledAt = claimedUntil

	return doc, nil
}

// sendUpdate sends documents to the update handler.
func (c *Client) sendUpdate(ctx context.Context, docs interface{}, commit bool) error {
	body, err := json.Marshal(docs)
	if err != nil {
		return fmt.Errorf("marshal update: %w", err)
	}

	updateURL := c.baseURL + updatePath
	if commit {
		updateURL += "?commit=true"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, updateURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create update request: %w", err)
	}

	req.Header.Set(headerContentType, contentTypeJSON)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("update request: %w", err)
	}
	defer resp.Body.Close()

	// Treat 409 Conflict as success for idempotent indexing operations.
	// A version conflict means another pod already indexed the same document,
	// which is the desired end state.
	if resp.StatusCode == httpStatusConflict {
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, errBodyReadLimit))
		if readErr != nil {
			return fmt.Errorf(errStatusFmt, ErrServerError, resp.StatusCode)
		}

		return fmt.Errorf(errStatusBodyFmt, ErrServerError, resp.StatusCode, string(respBody))
	}

	return nil
}

// searchParams holds search query parameters.
type searchParams struct {
	q       string
	fq      []string
	fl      string
	rows    int
	start   int
	sort    string
	defType string
	qf      string // query fields for edismax
}

// SearchOption configures a search query.
type SearchOption func(*searchParams)

// WithFilterQuery adds a filter query.
func WithFilterQuery(fq string) SearchOption {
	return func(p *searchParams) {
		p.fq = append(p.fq, fq)
	}
}

// WithFields sets the fields to return.
func WithFields(fields string) SearchOption {
	return func(p *searchParams) {
		p.fl = fields
	}
}

// WithRows sets the maximum number of results.
func WithRows(rows int) SearchOption {
	return func(p *searchParams) {
		p.rows = rows
	}
}

// WithStart sets the offset for pagination.
func WithStart(start int) SearchOption {
	return func(p *searchParams) {
		p.start = start
	}
}

// WithSort sets the sort order.
func WithSort(sort string) SearchOption {
	return func(p *searchParams) {
		p.sort = sort
	}
}

// WithEdismax enables edismax query parser with query fields.
func WithEdismax(queryFields string) SearchOption {
	return func(p *searchParams) {
		p.defType = "edismax"
		p.qf = queryFields
	}
}

// buildSearchParams constructs the URL values for a search query.
func (c *Client) buildSearchParams(params *searchParams) url.Values {
	q := url.Values{}
	q.Set("q", params.q)
	q.Set("rows", strconv.Itoa(params.rows))
	q.Set("wt", "json")

	if params.start > 0 {
		q.Set("start", strconv.Itoa(params.start))
	}

	for _, fq := range params.fq {
		q.Add("fq", fq)
	}

	if params.fl != "" {
		q.Set("fl", params.fl)
	}

	if params.sort != "" {
		q.Set("sort", params.sort)
	}

	if params.defType != "" {
		q.Set("defType", params.defType)
	}

	if params.qf != "" {
		q.Set("qf", params.qf)
	}

	return q
}

// buildSearchURL constructs the search URL with query parameters.
func (c *Client) buildSearchURL(params *searchParams) string {
	return c.baseURL + selectPath + "?" + c.buildSearchParams(params).Encode()
}
