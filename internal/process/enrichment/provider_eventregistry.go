package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/time/rate"
)

const (
	eventRegistryBaseURL        = "https://eventregistry.org/api/v1/article/getArticles"
	eventRegistryDefaultTimeout = 30 * time.Second
	eventRegistryDefaultRPM     = 30
	eventRegistryParamKeyword   = "keyword"
)

var (
	errEventRegistryUnexpectedStatus = errors.New("eventregistry unexpected status")
	errEventRegistryAPIError         = errors.New("eventregistry api error")
)

// EventRegistryProvider implements Provider for Event Registry API.
type EventRegistryProvider struct {
	apiKey      string
	httpClient  *http.Client
	rateLimiter *rate.Limiter
	enabled     bool
}

// EventRegistryConfig holds configuration for the Event Registry provider.
type EventRegistryConfig struct {
	Enabled        bool
	APIKey         string
	RequestsPerMin int
	Timeout        time.Duration
}

// NewEventRegistryProvider creates a new Event Registry provider instance.
func NewEventRegistryProvider(cfg EventRegistryConfig) *EventRegistryProvider {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = eventRegistryDefaultTimeout
	}

	rpm := cfg.RequestsPerMin
	if rpm <= 0 {
		rpm = eventRegistryDefaultRPM
	}

	rps := float64(rpm) / secondsPerMinute

	return &EventRegistryProvider{
		apiKey: cfg.APIKey,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		rateLimiter: rate.NewLimiter(rate.Limit(rps), 1),
		enabled:     cfg.Enabled && cfg.APIKey != "",
	}
}

// Name returns the provider name.
func (p *EventRegistryProvider) Name() ProviderName {
	return ProviderEventRegistry
}

func (p *EventRegistryProvider) Priority() int {
	return PriorityMedium
}

func (p *EventRegistryProvider) IsAvailable(_ context.Context) bool {
	return p.enabled && p.apiKey != ""
}

// Search performs a search query against Event Registry.
func (p *EventRegistryProvider) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	if !p.enabled {
		return nil, errProviderNotFound
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("eventregistry rate limit: %w", err)
	}

	searchURL := p.buildSearchURL(query, maxResults)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create eventregistry request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("eventregistry request: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(errWrapFmtWithCode, errEventRegistryUnexpectedStatus, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read eventregistry response: %w", err)
	}

	return p.parseResponse(body, maxResults)
}

func (p *EventRegistryProvider) buildSearchURL(query string, maxResults int) string {
	params := url.Values{}
	params.Set("apiKey", p.apiKey)
	params.Set(eventRegistryParamKeyword, query)
	params.Set("articlesCount", fmt.Sprintf("%d", maxResults))
	params.Set("articlesSortBy", "date")
	params.Set("articlesSortByAsc", "false")
	params.Set("resultType", "articles")

	return eventRegistryBaseURL + "?" + params.Encode()
}

// eventRegistryResponse represents the JSON response from Event Registry.
type eventRegistryResponse struct {
	Articles eventRegistryArticles `json:"articles"`
}

type eventRegistryArticles struct {
	Results []eventRegistryArticle `json:"results"`
}

type eventRegistryArticle struct {
	URI      string `json:"uri"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	DateTime string `json:"dateTime"` //nolint:tagliatelle // Event Registry API uses camelCase
	Source   struct {
		URI   string `json:"uri"`
		Title string `json:"title"`
	} `json:"source"`
	Lang string `json:"lang"`
}

func (p *EventRegistryProvider) parseResponse(body []byte, maxResults int) ([]SearchResult, error) {
	if err := checkEventRegistryError(body); err != nil {
		return nil, err
	}

	var resp eventRegistryResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse eventregistry json: %w", err)
	}

	results := make([]SearchResult, 0, min(len(resp.Articles.Results), maxResults))

	for i, article := range resp.Articles.Results {
		if i >= maxResults {
			break
		}

		articleURL := article.URL
		if articleURL == "" {
			continue
		}

		result := SearchResult{
			URL:         articleURL,
			Title:       article.Title,
			Description: truncateDescription(article.Body),
			Domain:      extractDomain(articleURL),
		}

		if article.DateTime != "" {
			if t, err := time.Parse("2006-01-02T15:04:05Z", article.DateTime); err == nil {
				result.PublishedAt = t
			}
		}

		results = append(results, result)
	}

	return results, nil
}

const maxDescriptionLength = 300

func truncateDescription(text string) string {
	if len(text) <= maxDescriptionLength {
		return text
	}

	return text[:maxDescriptionLength] + "..."
}

func checkEventRegistryError(body []byte) error {
	if len(body) > 0 && body[0] != '{' && body[0] != '[' {
		// Not JSON, likely an error message or HTML page from Event Registry
		errMsg := string(body)
		if len(errMsg) > 200 {
			errMsg = errMsg[:200] + "..."
		}

		return fmt.Errorf(fmtErrWrapStr, errEventRegistryAPIError, errMsg)
	}

	return nil
}
