package enrichment

import (
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
)

const (
	yacyDefaultTimeout    = 30 * time.Second
	yacySearchPath        = "/yacysearch.json"
	yacyHealthCheckTimout = 5 * time.Second
)

var errYaCyUnexpectedStatus = errors.New("yacy unexpected status")

type YaCyProvider struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
	enabled    bool
}

type YaCyConfig struct {
	Enabled  bool
	BaseURL  string
	Timeout  time.Duration
	Username string
	Password string
}

func NewYaCyProvider(cfg YaCyConfig) *YaCyProvider {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = yacyDefaultTimeout
	}

	return &YaCyProvider{
		baseURL:  strings.TrimSuffix(cfg.BaseURL, "/"),
		username: cfg.Username,
		password: cfg.Password,
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

func (p *YaCyProvider) IsAvailable() bool {
	if !p.enabled || p.baseURL == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), yacyHealthCheckTimout)
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
	if !p.enabled {
		return nil, errProviderNotFound
	}

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

	return p.parseResponse(body)
}

func (p *YaCyProvider) buildSearchURL(query string, maxResults int) string {
	params := url.Values{}
	params.Set(searchParamKeyQuery, query)
	params.Set("count", fmt.Sprintf("%d", maxResults))
	params.Set("resource", "global")
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
