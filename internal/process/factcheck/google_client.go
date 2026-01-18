package factcheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/time/rate"
)

const (
	googleFactCheckEndpoint = "https://factchecktools.googleapis.com/v1alpha1/claims:search"
	defaultHTTPTimeout      = 20 * time.Second
)

var errFactCheckStatus = errors.New("fact check status")

type Result struct {
	Claim     string
	URL       string
	Publisher string
	Rating    string
}

type GoogleClient struct {
	apiKey     string
	maxResults int
	limiter    *rate.Limiter
	client     *http.Client
}

func NewGoogleClient(apiKey string, rpm int, maxResults int) *GoogleClient {
	if rpm <= 0 {
		rpm = 60
	}

	if maxResults <= 0 {
		maxResults = 3
	}

	return &GoogleClient{
		apiKey:     apiKey,
		maxResults: maxResults,
		limiter:    rate.NewLimiter(rate.Every(time.Minute/time.Duration(rpm)), 1),
		client: &http.Client{
			Timeout: defaultHTTPTimeout,
		},
	}
}

func (c *GoogleClient) Search(ctx context.Context, claim string) ([]Result, []byte, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, nil, fmt.Errorf("fact check rate limit: %w", err)
	}

	endpoint, err := c.buildURL(claim)
	if err != nil {
		return nil, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("fact check request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("%w: %d", errFactCheckStatus, resp.StatusCode)
	}

	var payload googleResponse

	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&payload); err != nil {
		return nil, nil, fmt.Errorf("decode fact check response: %w", err)
	}

	raw, _ := json.Marshal(payload)

	results := parseGoogleResults(payload, claim, c.maxResults)

	return results, raw, nil
}

func ParseGoogleResults(payload []byte, fallbackClaim string, maxResults int) ([]Result, error) {
	if maxResults <= 0 {
		maxResults = 3
	}

	var resp googleResponse
	if err := json.Unmarshal(payload, &resp); err != nil {
		return nil, fmt.Errorf("parse cached fact check: %w", err)
	}

	return parseGoogleResults(resp, fallbackClaim, maxResults), nil
}

func (c *GoogleClient) buildURL(claim string) (string, error) {
	values := url.Values{}
	values.Set("query", claim)
	values.Set("pageSize", fmt.Sprintf("%d", c.maxResults))
	values.Set("key", c.apiKey)

	u, err := url.Parse(googleFactCheckEndpoint)
	if err != nil {
		return "", fmt.Errorf("parse fact check endpoint: %w", err)
	}

	u.RawQuery = values.Encode()

	return u.String(), nil
}

type googleResponse struct {
	Claims []struct {
		Text        string `json:"text"`
		ClaimReview []struct {
			Publisher struct {
				Name string `json:"name"`
			} `json:"publisher"`
			URL           string `json:"url"`
			TextualRating string `json:"textualRating"` //nolint:tagliatelle
		} `json:"claimReview"` //nolint:tagliatelle
	} `json:"claims"`
}

func parseGoogleResults(resp googleResponse, fallbackClaim string, maxResults int) []Result {
	results := make([]Result, 0, maxResults)

	for _, claim := range resp.Claims {
		claimText := claim.Text
		if claimText == "" {
			claimText = fallbackClaim
		}

		for _, review := range claim.ClaimReview {
			if review.URL == "" {
				continue
			}

			results = append(results, Result{
				Claim:     claimText,
				URL:       review.URL,
				Publisher: review.Publisher.Name,
				Rating:    review.TextualRating,
			})

			if len(results) >= maxResults {
				return results
			}
		}
	}

	return results
}
