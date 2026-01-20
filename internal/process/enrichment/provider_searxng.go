package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	searxngDefaultTimeout       = 30 * time.Second
	searxngSearchPath           = "/search"
	searxngHealthCheckTimeout   = 5 * time.Second
	searxngResponseFormatJSON   = "json"
	searxngCategoriesGeneral    = "general"
	searxngLanguageFilterPrefix = "lang_"
	httpHeaderAccept            = "Accept"
	httpContentTypeJSON         = "application/json"
)

var (
	errSearxNGUnexpectedStatus = errors.New("searxng unexpected status")
	errSearxNGAPIError         = errors.New("searxng api error")
)

// SearxNGProvider implements Provider for SearxNG metasearch instances.
type SearxNGProvider struct {
	baseURL    string
	httpClient *http.Client
	enabled    bool
	engines    []string // optional: limit to specific engines
}

// SearxNGConfig holds configuration for the SearxNG provider.
type SearxNGConfig struct {
	Enabled bool
	BaseURL string
	Timeout time.Duration
	Engines []string // optional: e.g., ["google", "duckduckgo", "bing"]
}

// NewSearxNGProvider creates a new SearxNG provider instance.
func NewSearxNGProvider(cfg SearxNGConfig) *SearxNGProvider {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = searxngDefaultTimeout
	}

	return &SearxNGProvider{
		baseURL: strings.TrimSuffix(cfg.BaseURL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
		enabled: cfg.Enabled,
		engines: cfg.Engines,
	}
}

// Name returns the provider name.
func (p *SearxNGProvider) Name() ProviderName {
	return ProviderSearxNG
}

func (p *SearxNGProvider) Priority() int {
	return PriorityHighMeta
}

func (p *SearxNGProvider) IsAvailable(ctx context.Context) bool {
	if !p.enabled || p.baseURL == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(ctx, searxngHealthCheckTimeout)
	defer cancel()

	// SearxNG has a /config endpoint that returns instance configuration
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/config", nil)
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

// Search performs a search query against the SearxNG instance.
func (p *SearxNGProvider) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	if !p.enabled {
		return nil, errProviderNotFound
	}

	searchURL := p.buildSearchURL(query)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create searxng request: %w", err)
	}

	// SearxNG requires Accept header for JSON responses
	req.Header.Set(httpHeaderAccept, httpContentTypeJSON)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("searxng request: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(errWrapFmtWithCode, errSearxNGUnexpectedStatus, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read searxng response: %w", err)
	}

	return p.parseResponse(body, maxResults)
}

func (p *SearxNGProvider) buildSearchURL(query string) string {
	params := url.Values{}
	params.Set("q", query)
	params.Set(searchParamKeyFormat, searxngResponseFormatJSON)
	params.Set("categories", searxngCategoriesGeneral)

	// Add engine filter if specified
	if len(p.engines) > 0 {
		params.Set("engines", strings.Join(p.engines, ","))
	}

	return p.baseURL + searxngSearchPath + "?" + params.Encode()
}

// searxngResponse represents the JSON response from SearxNG.
type searxngResponse struct {
	Query       string          `json:"query"`
	Results     []searxngResult `json:"results"`
	Suggestions []string        `json:"suggestions"`
}

// searxngResult represents a single search result from SearxNG.
type searxngResult struct {
	URL           string   `json:"url"`
	Title         string   `json:"title"`
	Content       string   `json:"content"`
	PublishedDate string   `json:"publishedDate"` //nolint:tagliatelle // SearxNG API uses camelCase
	ParsedURL     []string `json:"parsed_url"`
	Engine        string   `json:"engine"`
	Engines       []string `json:"engines"`
	Score         float64  `json:"score"`
	Category      string   `json:"category"`
}

func (p *SearxNGProvider) parseResponse(body []byte, maxResults int) ([]SearchResult, error) {
	if err := checkSearxNGError(body); err != nil {
		return nil, err
	}

	var resp searxngResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse searxng json: %w", err)
	}

	results := make([]SearchResult, 0, min(len(resp.Results), maxResults))

	for i, item := range resp.Results {
		if i >= maxResults {
			break
		}

		if item.URL == "" {
			continue
		}

		result := SearchResult{
			URL:         item.URL,
			Title:       item.Title,
			Description: item.Content,
			Domain:      extractDomain(item.URL),
			Score:       item.Score,
		}

		// Parse published date if available
		if item.PublishedDate != "" {
			result.PublishedAt = parseSearxNGDate(item.PublishedDate)
		}

		results = append(results, result)
	}

	return results, nil
}

// parseSearxNGDate attempts to parse various date formats from SearxNG.
func parseSearxNGDate(dateStr string) time.Time {
	// SearxNG returns dates in various formats depending on the source engine
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02 15:04:05",
		"2006-01-02",
		"Jan 2, 2006",
		"January 2, 2006",
		"02 Jan 2006",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t
		}
	}

	return time.Time{}
}

func checkSearxNGError(body []byte) error {
	if len(body) > 0 && body[0] != '{' && body[0] != '[' {
		// Not JSON, likely an error message or HTML page from SearxNG
		errMsg := string(body)
		if len(errMsg) > 200 {
			errMsg = errMsg[:200] + "..."
		}

		return fmt.Errorf(fmtErrWrapStr, errSearxNGAPIError, errMsg)
	}

	return nil
}
