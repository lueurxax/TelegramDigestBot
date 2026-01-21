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
	"strconv"
	"strings"
	"time"

	"github.com/lueurxax/telegram-digest-bot/internal/core/links"
)

const (
	yacyDefaultTimeout    = 30 * time.Second
	yacySearchPath        = "/yacysearch.json"
	yacyHealthCheckTimout = 5 * time.Second
)

var (
	errYaCyUnexpectedStatus = errors.New("yacy unexpected status")
	errYaCyAPIError         = errors.New("yacy api error")
)

type YaCyProvider struct {
	baseURL    string
	username   string
	password   string
	resource   string
	httpClient *http.Client
	maxResults int
	enabled    bool
}

type YaCyConfig struct {
	Enabled    bool
	BaseURL    string
	Timeout    time.Duration
	Username   string
	Password   string
	Resource   string
	MaxResults int
}

func NewYaCyProvider(cfg YaCyConfig) *YaCyProvider {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = yacyDefaultTimeout
	}

	resource := strings.TrimSpace(cfg.Resource)
	if resource == "" {
		resource = "local"
	}

	return &YaCyProvider{
		baseURL:    strings.TrimSuffix(cfg.BaseURL, "/"),
		username:   cfg.Username,
		password:   cfg.Password,
		resource:   resource,
		maxResults: cfg.MaxResults,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		enabled: cfg.Enabled,
	}
}

func (p *YaCyProvider) Name() ProviderName {
	return ProviderYaCy
}

func (p *YaCyProvider) Priority() int {
	return PriorityHighSelfHosted
}

func (p *YaCyProvider) IsAvailable(ctx context.Context) bool {
	if !p.enabled || p.baseURL == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(ctx, yacyHealthCheckTimout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/Status.html", nil)
	if err != nil {
		return false
	}

	resp, err := p.doRequest(req)
	if err != nil {
		return false
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	return resp.StatusCode == http.StatusOK
}

func (p *YaCyProvider) Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	return p.SearchWithLanguage(ctx, query, "", maxResults)
}

func (p *YaCyProvider) SearchWithLanguage(ctx context.Context, query, language string, maxResults int) ([]SearchResult, error) {
	if !p.enabled {
		return nil, errProviderNotFound
	}

	if p.maxResults > 0 {
		maxResults = p.maxResults
	}

	body, err := p.fetchSearchResults(ctx, query, maxResults)
	if err != nil {
		return nil, err
	}

	results, err := p.parseResponse(body)
	if err != nil {
		return nil, err
	}

	return p.filterResultsByLanguage(results, language), nil
}

func (p *YaCyProvider) fetchSearchResults(ctx context.Context, query string, maxResults int) ([]byte, error) {
	searchURL := p.buildSearchURL(query, maxResults)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create yacy request: %w", err)
	}

	resp, err := p.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("yacy request: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(errWrapFmtWithCode, errYaCyUnexpectedStatus, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read yacy response: %w", err)
	}

	return body, nil
}

func (p *YaCyProvider) filterResultsByLanguage(results []SearchResult, language string) []SearchResult {
	if language == "" || isUnknownLanguage(language) {
		return results
	}

	// Filter results by language
	filtered := make([]SearchResult, 0, len(results))

	for _, res := range results {
		detected := links.DetectLanguage(res.Title + " " + res.Description)
		// Allow if language matches or if detection is unsure
		if detected == language || detected == "" {
			filtered = append(filtered, res)
		}
	}

	return filtered
}

func (p *YaCyProvider) buildSearchURL(query string, maxResults int) string {
	params := url.Values{}
	params.Set(searchParamKeyQuery, query)
	params.Set("count", fmt.Sprintf("%d", maxResults))
	params.Set("resource", p.resource)
	params.Set("urlmaskfilter", ".*")
	params.Set("prefermaskfilter", "")

	return p.baseURL + yacySearchPath + "?" + params.Encode()
}

type yacyResponse struct {
	Channels []yacyChannel `json:"channels"`
}

type yacyChannel struct {
	Items []yacyItem `json:"items"`
}

type yacyItem struct {
	Title       string          `json:"title"`
	Link        string          `json:"link"`
	Description string          `json:"description"`
	PubDate     string          `json:"pubDate"` //nolint:tagliatelle // YaCy API uses camelCase
	Size        string          `json:"size"`
	SizeNumber  int64           `json:"sizelong"`
	Ranking     floatFlexible64 `json:"ranking"`
}

func (p *YaCyProvider) parseResponse(body []byte) ([]SearchResult, error) {
	if err := checkYaCyError(body); err != nil {
		return nil, err
	}

	var resp yacyResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse yacy json: %w", err)
	}

	results := []SearchResult{}

	for _, channel := range resp.Channels {
		for _, item := range channel.Items {
			result := SearchResult{
				URL:         item.Link,
				Title:       item.Title,
				Description: item.Description,
				Domain:      extractDomain(item.Link),
				Score:       float64(item.Ranking),
			}

			if item.PubDate != "" {
				if t, err := time.Parse(time.RFC1123, item.PubDate); err == nil {
					result.PublishedAt = t
				}
			}

			results = append(results, result)
		}
	}

	return results, nil
}

type floatFlexible64 float64

func (f *floatFlexible64) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*f = 0
		return nil
	}

	if data[0] == '"' {
		var str string
		if err := json.Unmarshal(data, &str); err != nil {
			return fmt.Errorf("unmarshal float string: %w", err)
		}

		if strings.TrimSpace(str) == "" {
			*f = 0
			return nil
		}

		val, err := strconv.ParseFloat(str, 64)
		if err != nil {
			return fmt.Errorf("parse float: %w", err)
		}

		*f = floatFlexible64(val)

		return nil
	}

	var val float64
	if err := json.Unmarshal(data, &val); err != nil {
		return fmt.Errorf("unmarshal float: %w", err)
	}

	*f = floatFlexible64(val)

	return nil
}

func extractDomain(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	return parsed.Host
}

func checkYaCyError(body []byte) error {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] != '{' && trimmed[0] != '[' {
		// Not JSON, likely an error message or HTML page from YaCy
		errMsg := string(trimmed)
		if len(errMsg) > 200 {
			errMsg = errMsg[:200] + "..."
		}

		return fmt.Errorf(fmtErrWrapStr, errYaCyAPIError, errMsg)
	}

	return nil
}
