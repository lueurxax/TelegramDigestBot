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
	gdeltBaseURL         = "https://api.gdeltproject.org/api/v2/doc/doc"
	gdeltDefaultTimeout  = 30 * time.Second
	gdeltDefaultRPM      = 60
	secondsPerMinute     = 60.0
	searchParamKeyQuery  = "query"
	searchParamKeyFormat = "format"
)

var errGDELTUnexpectedStatus = errors.New("gdelt unexpected status")

type GDELTProvider struct {
	httpClient  *http.Client
	rateLimiter *rate.Limiter
	enabled     bool
}

type GDELTConfig struct {
	Enabled        bool
	RequestsPerMin int
	Timeout        time.Duration
}

func NewGDELTProvider(cfg GDELTConfig) *GDELTProvider {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = gdeltDefaultTimeout
	}

	rpm := cfg.RequestsPerMin
	if rpm <= 0 {
		rpm = gdeltDefaultRPM
	}

	rps := float64(rpm) / secondsPerMinute

	return &GDELTProvider{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		rateLimiter: rate.NewLimiter(rate.Limit(rps), 1),
		enabled:     cfg.Enabled,
	}
}

func (p *GDELTProvider) Name() ProviderName {
	return ProviderGDELT
}

func (p *GDELTProvider) Priority() int {
	return PriorityHighFree
}

func (p *GDELTProvider) IsAvailable() bool {
	return p.enabled
}

func (p *GDELTProvider) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	if !p.enabled {
		return nil, errProviderNotFound
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("gdelt rate limit: %w", err)
	}

	searchURL := buildGDELTURL(query, maxResults)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create gdelt request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gdelt request: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(errWrapFmtWithCode, errGDELTUnexpectedStatus, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read gdelt response: %w", err)
	}

	return parseGDELTResponse(body)
}

func buildGDELTURL(query string, maxResults int) string {
	params := url.Values{}
	params.Set(searchParamKeyQuery, query)
	params.Set("mode", "ArtList")
	params.Set("maxrecords", fmt.Sprintf("%d", maxResults))
	params.Set(searchParamKeyFormat, "json")
	params.Set("sort", "DateDesc")

	return gdeltBaseURL + "?" + params.Encode()
}

type gdeltResponse struct {
	Articles []gdeltArticle `json:"articles"`
}

type gdeltArticle struct {
	URL           string `json:"url"`
	URLMobile     string `json:"url_mobile"`
	Title         string `json:"title"`
	SeenDate      string `json:"seendate"`
	SocialImage   string `json:"socialimage"`
	Domain        string `json:"domain"`
	Language      string `json:"language"`
	SourceCountry string `json:"sourcecountry"`
}

func parseGDELTResponse(body []byte) ([]SearchResult, error) {
	var resp gdeltResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse gdelt json: %w", err)
	}

	results := make([]SearchResult, 0, len(resp.Articles))

	for _, article := range resp.Articles {
		articleURL := article.URL
		if articleURL == "" {
			articleURL = article.URLMobile
		}

		if articleURL == "" {
			continue
		}

		result := SearchResult{
			URL:    articleURL,
			Title:  article.Title,
			Domain: article.Domain,
		}

		if article.SeenDate != "" {
			if t, err := time.Parse("20060102T150405Z", article.SeenDate); err == nil {
				result.PublishedAt = t
			}
		}

		results = append(results, result)
	}

	return results, nil
}
