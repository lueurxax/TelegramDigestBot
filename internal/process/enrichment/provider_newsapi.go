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
	newsAPIBaseURL        = "https://newsapi.org/v2/everything"
	newsAPIDefaultTimeout = 30 * time.Second
	newsAPIDefaultRPM     = 100 // Free tier: 100 requests/day, paid has higher limits
	newsAPIAuthHeader     = "X-Api-Key"
	newsAPIParamLanguage  = "language"
)

var (
	errNewsAPIUnexpectedStatus = errors.New("newsapi unexpected status")
	errNewsAPIBadStatus        = errors.New("newsapi bad status")
	errNewsAPIError            = errors.New("newsapi api error")
)

// NewsAPIProvider implements Provider for NewsAPI.
type NewsAPIProvider struct {
	apiKey      string
	httpClient  *http.Client
	rateLimiter *rate.Limiter
	enabled     bool
}

// NewsAPIConfig holds configuration for the NewsAPI provider.
type NewsAPIConfig struct {
	Enabled        bool
	APIKey         string
	RequestsPerMin int
	Timeout        time.Duration
}

// NewNewsAPIProvider creates a new NewsAPI provider instance.
func NewNewsAPIProvider(cfg NewsAPIConfig) *NewsAPIProvider {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = newsAPIDefaultTimeout
	}

	rpm := cfg.RequestsPerMin
	if rpm <= 0 {
		rpm = newsAPIDefaultRPM
	}

	rps := float64(rpm) / secondsPerMinute

	return &NewsAPIProvider{
		apiKey: cfg.APIKey,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		rateLimiter: rate.NewLimiter(rate.Limit(rps), 1),
		enabled:     cfg.Enabled && cfg.APIKey != "",
	}
}

// Name returns the provider name.
func (p *NewsAPIProvider) Name() ProviderName {
	return ProviderNewsAPI
}

func (p *NewsAPIProvider) Priority() int {
	return PriorityMediumFallback
}

func (p *NewsAPIProvider) IsAvailable(_ context.Context) bool {
	return p.enabled && p.apiKey != ""
}

// Search performs a search query against NewsAPI.
func (p *NewsAPIProvider) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	if !p.enabled {
		return nil, errProviderNotFound
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("newsapi rate limit: %w", err)
	}

	searchURL := p.buildSearchURL(query, maxResults)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create newsapi request: %w", err)
	}

	req.Header.Set(newsAPIAuthHeader, p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("newsapi request: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(errWrapFmtWithCode, errNewsAPIUnexpectedStatus, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read newsapi response: %w", err)
	}

	return p.parseResponse(body, maxResults)
}

func (p *NewsAPIProvider) buildSearchURL(query string, maxResults int) string {
	params := url.Values{}
	params.Set("q", query)
	params.Set("pageSize", fmt.Sprintf("%d", maxResults))
	params.Set("sortBy", "publishedAt")
	params.Set(newsAPIParamLanguage, "en") // Default to English, can be extended

	return newsAPIBaseURL + "?" + params.Encode()
}

// newsAPIResponse represents the JSON response from NewsAPI.
type newsAPIResponse struct {
	Status       string           `json:"status"`
	TotalResults int              `json:"totalResults"` //nolint:tagliatelle // NewsAPI uses camelCase
	Articles     []newsAPIArticle `json:"articles"`
}

type newsAPIArticle struct {
	Source struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"source"`
	Author      string `json:"author"`
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	URLToImage  string `json:"urlToImage"`  //nolint:tagliatelle // NewsAPI uses camelCase
	PublishedAt string `json:"publishedAt"` //nolint:tagliatelle // NewsAPI uses camelCase
	Content     string `json:"content"`
}

func (p *NewsAPIProvider) parseResponse(body []byte, maxResults int) ([]SearchResult, error) {
	if err := checkNewsAPIError(body); err != nil {
		return nil, err
	}

	var resp newsAPIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse newsapi json: %w", err)
	}

	if resp.Status != "ok" {
		return nil, fmt.Errorf("%w: %s", errNewsAPIBadStatus, resp.Status)
	}

	results := make([]SearchResult, 0, min(len(resp.Articles), maxResults))

	for i, article := range resp.Articles {
		if i >= maxResults {
			break
		}

		if article.URL == "" {
			continue
		}

		result := SearchResult{
			URL:         article.URL,
			Title:       article.Title,
			Description: article.Description,
			Domain:      extractDomain(article.URL),
		}

		if article.PublishedAt != "" {
			if t, err := time.Parse(time.RFC3339, article.PublishedAt); err == nil {
				result.PublishedAt = t
			}
		}

		results = append(results, result)
	}

	return results, nil
}

func checkNewsAPIError(body []byte) error {
	if len(body) > 0 && body[0] != '{' && body[0] != '[' {
		// Not JSON, likely an error message or HTML page from NewsAPI
		errMsg := string(body)
		if len(errMsg) > 200 {
			errMsg = errMsg[:200] + "..."
		}

		return fmt.Errorf(fmtErrWrapStr, errNewsAPIError, errMsg)
	}

	return nil
}
