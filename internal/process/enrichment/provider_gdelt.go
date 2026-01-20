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
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const (
	gdeltBaseURL         = "https://api.gdeltproject.org/api/v2/doc/doc"
	gdeltDefaultTimeout  = 30 * time.Second
	gdeltDefaultRPM      = 60
	searchParamKeyQuery  = "query"
	searchParamKeyFormat = "format"
)

var (
	errGDELTUnexpectedStatus = errors.New("gdelt unexpected status")
	errGDELTAPIError         = errors.New("gdelt api error")
)

type GDELTProvider struct {
	baseURL     string
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
		baseURL: gdeltBaseURL,
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

func (p *GDELTProvider) IsAvailable(_ context.Context) bool {
	return p.enabled
}

func (p *GDELTProvider) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	return p.search(ctx, query, "", maxResults)
}

func (p *GDELTProvider) SearchWithLanguage(ctx context.Context, query, language string, maxResults int) ([]SearchResult, error) {
	if isUnknownLanguage(language) {
		return p.search(ctx, query, "", maxResults)
	}

	return p.search(ctx, query, normalizeLanguage(language), maxResults)
}

func (p *GDELTProvider) search(ctx context.Context, query, language string, maxResults int) ([]SearchResult, error) {
	if !p.enabled {
		return nil, errProviderNotFound
	}

	if err := p.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("gdelt rate limit: %w", err)
	}

	searchURL := p.buildGDELTURL(query, maxResults)

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

	return parseGDELTResponse(body, language)
}

func (p *GDELTProvider) buildGDELTURL(query string, maxResults int) string {
	sanitizedQuery := sanitizeGDELTQuery(query)

	params := url.Values{}
	params.Set(searchParamKeyQuery, sanitizedQuery)
	params.Set("mode", "ArtList")
	params.Set("maxrecords", fmt.Sprintf("%d", maxResults))
	params.Set(searchParamKeyFormat, fmtJSON)
	params.Set("sort", "DateDesc")

	return p.baseURL + "?" + params.Encode()
}

func sanitizeGDELTQuery(query string) string {
	words := strings.Fields(query)
	filtered := make([]string, 0, len(words))

	for _, w := range words {
		lower := strings.ToLower(w)
		if len([]rune(w)) >= minKeywordLength && !isStopWord(lower) {
			filtered = append(filtered, w)
		}
	}

	return strings.Join(filtered, " ")
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

func parseGDELTResponse(body []byte, language string) ([]SearchResult, error) {
	if err := checkGDELTError(body); err != nil {
		return nil, err
	}

	var resp gdeltResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse gdelt json: %w", err)
	}

	results := make([]SearchResult, 0, len(resp.Articles))

	for _, article := range resp.Articles {
		if !languageMatches(language, article.Language) {
			continue
		}

		if result := mapGDELTArticle(article); result != nil {
			results = append(results, *result)
		}
	}

	return results, nil
}

func checkGDELTError(body []byte) error {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] != '{' && trimmed[0] != '[' {
		// Not JSON, likely an error message from GDELT
		errMsg := string(trimmed)
		if len(errMsg) > 200 {
			errMsg = errMsg[:200] + "..."
		}

		return fmt.Errorf(fmtErrWrapStr, errGDELTAPIError, errMsg)
	}

	return nil
}

func mapGDELTArticle(article gdeltArticle) *SearchResult {
	articleURL := article.URL
	if articleURL == "" {
		articleURL = article.URLMobile
	}

	if articleURL == "" {
		return nil
	}

	result := &SearchResult{
		URL:    articleURL,
		Title:  article.Title,
		Domain: article.Domain,
	}

	if article.SeenDate != "" {
		if t, err := time.Parse("20060102T150405Z", article.SeenDate); err == nil {
			result.PublishedAt = t
		}
	}

	return result
}
