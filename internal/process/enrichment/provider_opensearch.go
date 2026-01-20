package enrichment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const (
	opensearchDefaultTimeout     = 30 * time.Second
	opensearchDefaultRPM         = 60
	opensearchSearchPath         = "/_search"
	opensearchHealthCheckTimeout = 5 * time.Second
	opensearchDefaultIndex       = "news"
	opensearchContentType        = "application/json"
)

var (
	errOpenSearchUnexpectedStatus = errors.New("opensearch unexpected status")
	errOpenSearchAPIError         = errors.New("opensearch api error")
)

// OpenSearchProvider implements Provider for OpenSearch.
type OpenSearchProvider struct {
	baseURL     string
	index       string
	httpClient  *http.Client
	rateLimiter *rate.Limiter
	enabled     bool
}

// OpenSearchConfig holds configuration for the OpenSearch provider.
type OpenSearchConfig struct {
	Enabled        bool
	BaseURL        string
	Index          string
	RequestsPerMin int
	Timeout        time.Duration
}

// NewOpenSearchProvider creates a new OpenSearch provider instance.
func NewOpenSearchProvider(cfg OpenSearchConfig) *OpenSearchProvider {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = opensearchDefaultTimeout
	}

	rpm := cfg.RequestsPerMin
	if rpm <= 0 {
		rpm = opensearchDefaultRPM
	}

	index := cfg.Index
	if index == "" {
		index = opensearchDefaultIndex
	}

	rps := float64(rpm) / secondsPerMinute

	return &OpenSearchProvider{
		baseURL: strings.TrimSuffix(cfg.BaseURL, "/"),
		index:   index,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		rateLimiter: rate.NewLimiter(rate.Limit(rps), 1),
		enabled:     cfg.Enabled && cfg.BaseURL != "",
	}
}

// Name returns the provider name.
func (p *OpenSearchProvider) Name() ProviderName {
	return ProviderOpenSearch
}

func (p *OpenSearchProvider) Priority() int {
	return PriorityLow
}

func (p *OpenSearchProvider) IsAvailable() bool {
	if !p.enabled || p.baseURL == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), opensearchHealthCheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL, nil)
	if err != nil {
		return false
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return false
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	return resp.StatusCode == http.StatusOK
}

// Search performs a search query against OpenSearch.
func (p *OpenSearchProvider) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	if !p.enabled {
		return nil, errProviderNotFound
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("opensearch rate limit: %w", err)
	}

	searchURL := p.buildSearchURL()
	requestBody := p.buildSearchBody(query, maxResults)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, searchURL, bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("create opensearch request: %w", err)
	}

	req.Header.Set(httpHeaderContent, opensearchContentType)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opensearch request: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(errWrapFmtWithCode, errOpenSearchUnexpectedStatus, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read opensearch response: %w", err)
	}

	return p.parseResponse(body, maxResults)
}

func (p *OpenSearchProvider) buildSearchURL() string {
	return p.baseURL + "/" + p.index + opensearchSearchPath
}

func (p *OpenSearchProvider) buildSearchBody(query string, maxResults int) []byte {
	searchQuery := opensearchQuery{
		Size: maxResults,
		Query: opensearchQueryBody{
			MultiMatch: &opensearchMultiMatch{
				Query:  query,
				Fields: []string{"title^2", "content", "description"},
			},
		},
		Sort: []map[string]string{
			{"published_at": "desc"},
		},
	}

	body, _ := json.Marshal(searchQuery)

	return body
}

type opensearchQuery struct {
	Size  int                 `json:"size"`
	Query opensearchQueryBody `json:"query"`
	Sort  []map[string]string `json:"sort,omitempty"`
}

type opensearchQueryBody struct {
	MultiMatch *opensearchMultiMatch `json:"multi_match,omitempty"`
}

type opensearchMultiMatch struct {
	Query  string   `json:"query"`
	Fields []string `json:"fields"`
}

type opensearchResponse struct {
	Hits opensearchHits `json:"hits"`
}

type opensearchHits struct {
	Total struct {
		Value int `json:"value"`
	} `json:"total"`
	Hits []opensearchHit `json:"hits"`
}

type opensearchHit struct {
	ID     string             `json:"_id"`     //nolint:tagliatelle // OpenSearch API uses _id
	Score  float64            `json:"_score"`  //nolint:tagliatelle // OpenSearch API uses _score
	Source opensearchDocument `json:"_source"` //nolint:tagliatelle // OpenSearch API uses _source
}

type opensearchDocument struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Domain      string `json:"domain"`
	PublishedAt string `json:"published_at"`
}

func (p *OpenSearchProvider) parseResponse(body []byte, maxResults int) ([]SearchResult, error) {
	if err := checkOpenSearchError(body); err != nil {
		return nil, err
	}

	var resp opensearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse opensearch json: %w", err)
	}

	results := make([]SearchResult, 0, min(len(resp.Hits.Hits), maxResults))

	for i, hit := range resp.Hits.Hits {
		if i >= maxResults {
			break
		}

		if hit.Source.URL == "" {
			continue
		}

		result := SearchResult{
			URL:         hit.Source.URL,
			Title:       hit.Source.Title,
			Description: p.getDescription(hit.Source),
			Domain:      hit.Source.Domain,
			Score:       hit.Score,
		}

		if result.Domain == "" {
			result.Domain = extractDomain(hit.Source.URL)
		}

		if hit.Source.PublishedAt != "" {
			if t, err := time.Parse(time.RFC3339, hit.Source.PublishedAt); err == nil {
				result.PublishedAt = t
			}
		}

		results = append(results, result)
	}

	return results, nil
}

func (p *OpenSearchProvider) getDescription(doc opensearchDocument) string {
	if doc.Description != "" {
		return truncateDescription(doc.Description)
	}

	if doc.Content != "" {
		return truncateDescription(doc.Content)
	}

	return ""
}

func checkOpenSearchError(body []byte) error {
	if len(body) > 0 && body[0] != '{' && body[0] != '[' {
		// Not JSON, likely an error message or HTML page from OpenSearch
		errMsg := string(body)
		if len(errMsg) > 200 {
			errMsg = errMsg[:200] + "..."
		}

		return fmt.Errorf(fmtErrWrapStr, errOpenSearchAPIError, errMsg)
	}

	return nil
}
