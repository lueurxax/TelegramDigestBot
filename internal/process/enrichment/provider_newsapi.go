package enrichment

import (
	"bytes"
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
	newsAPIDefaultRPM     = 1 // Free tier: 100 requests/day, set low to avoid instant rate limiting
	newsAPIAuthHeader     = "X-Api-Key"
	newsAPIParamLanguage  = "language"
)

var (
	errNewsAPIUnexpectedStatus = errors.New("newsapi unexpected status")
	errNewsAPIBadStatus        = errors.New("newsapi bad status")
	errNewsAPIError            = errors.New("newsapi api error")
	errNewsAPIRateLimited      = errors.New("newsapi rate limited")
)

// NewsAPIProvider implements Provider for NewsAPI.
type NewsAPIProvider struct {
	baseURL     string
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
		baseURL: newsAPIBaseURL,
		apiKey:  cfg.APIKey,
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
	return p.search(ctx, query, "", maxResults)
}

func (p *NewsAPIProvider) SearchWithLanguage(ctx context.Context, query, language string, maxResults int) ([]SearchResult, error) {
	if isUnknownLanguage(language) || !isNewsAPILanguageSupported(language) {
		return p.search(ctx, query, "", maxResults)
	}

	return p.search(ctx, query, normalizeLanguage(language), maxResults)
}

func (p *NewsAPIProvider) search(ctx context.Context, query, language string, maxResults int) ([]SearchResult, error) {
	if !p.enabled {
		return nil, errProviderNotFound
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("newsapi rate limit: %w", err)
	}

	searchURL := p.buildSearchURL(query, language, maxResults)

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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read newsapi response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, errNewsAPIRateLimited
	}

	if resp.StatusCode != http.StatusOK {
		if err := checkNewsAPIError(body); err != nil {
			return nil, err
		}

		return nil, fmt.Errorf(errWrapFmtWithCode, errNewsAPIUnexpectedStatus, resp.StatusCode)
	}

	return p.parseResponse(body, maxResults)
}

func (p *NewsAPIProvider) buildSearchURL(query, language string, maxResults int) string {
	params := url.Values{}
	params.Set("q", query)
	params.Set("pageSize", fmt.Sprintf("%d", maxResults))
	params.Set("sortBy", "publishedAt")

	if language != "" {
		params.Set(newsAPIParamLanguage, language)
	}

	return p.baseURL + "?" + params.Encode()
}

func isNewsAPILanguageSupported(language string) bool {
	switch normalizeLanguage(language) {
	case "ar", "de", "en", "es", "fr", "he", "it", "nl", "no", "pt", "ru", "sv", "ud", "zh":
		return true
	default:
		return false
	}
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

type newsAPIErrorResponse struct {
	Status  string `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func checkNewsAPIError(body []byte) error {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil
	}

	if trimmed[0] != '{' && trimmed[0] != '[' {
		// Not JSON, likely an error message or HTML page from NewsAPI
		errMsg := string(trimmed)
		if len(errMsg) > responseTruncateLen {
			errMsg = errMsg[:responseTruncateLen] + "..."
		}

		return fmt.Errorf(fmtErrWrapStr, errNewsAPIError, errMsg)
	}

	var errResp newsAPIErrorResponse
	if err := json.Unmarshal(trimmed, &errResp); err == nil && errResp.Status == "error" {
		return fmt.Errorf("%w: %s (%s)", errNewsAPIError, errResp.Message, errResp.Code)
	}

	return nil
}
