package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Cohere API constants.
const (
	CohereAPIEndpoint        = "https://api.cohere.ai/v1/embed"
	ModelEmbedMultilingualV3 = "embed-multilingual-v3.0"

	// embed-multilingual-v3.0 produces 1024-dimensional vectors.
	cohereDimensions = 1024

	// Default rate limiter burst.
	cohereRateLimiterBurst = 5

	// Default timeout for Cohere API requests.
	cohereDefaultTimeout = 30 * time.Second

	// HTTP header constants.
	headerContentType = "Content-Type"
	contentTypeJSON   = "application/json"
)

// Cohere errors.
var (
	ErrCohereEmptyResponse = errors.New("empty embedding response from Cohere")
	ErrCohereAPIFailure    = errors.New("cohere API error")
)

// CohereProvider implements the embedding Provider interface for Cohere.
type CohereProvider struct {
	apiKey      string
	model       string
	httpClient  *http.Client
	rateLimiter *rate.Limiter
	mu          sync.RWMutex
	available   bool
}

// CohereConfig holds configuration for the Cohere provider.
type CohereConfig struct {
	APIKey    string
	Model     string // Default: "embed-multilingual-v3.0"
	RateLimit int    // Requests per second
	Timeout   time.Duration
}

// cohereEmbedRequest represents the Cohere API embed request.
type cohereEmbedRequest struct {
	Texts     []string `json:"texts"`
	Model     string   `json:"model"`
	InputType string   `json:"input_type"`
}

// cohereEmbedResponse represents the Cohere API embed response.
type cohereEmbedResponse struct {
	ID         string      `json:"id"`
	Embeddings [][]float32 `json:"embeddings"`
	Meta       struct {
		APIVersion struct {
			Version string `json:"version"`
		} `json:"api_version"`
	} `json:"meta"`
}

// cohereErrorResponse represents the Cohere API error response.
type cohereErrorResponse struct {
	Message string `json:"message"`
}

// NewCohereProvider creates a new Cohere embedding provider.
func NewCohereProvider(cfg CohereConfig) *CohereProvider {
	if cfg.Model == "" {
		cfg.Model = ModelEmbedMultilingualV3
	}

	if cfg.RateLimit == 0 {
		cfg.RateLimit = 1
	}

	if cfg.Timeout == 0 {
		cfg.Timeout = cohereDefaultTimeout
	}

	return &CohereProvider{
		apiKey: cfg.APIKey,
		model:  cfg.Model,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
		rateLimiter: rate.NewLimiter(rate.Limit(cfg.RateLimit), cohereRateLimiterBurst),
		available:   cfg.APIKey != "",
	}
}

// Name returns the provider identifier.
func (p *CohereProvider) Name() ProviderName {
	return ProviderCohere
}

// Priority returns the provider priority.
func (p *CohereProvider) Priority() int {
	return PriorityFallback
}

// Dimensions returns the output dimensions (1024 for embed-multilingual-v3.0).
func (p *CohereProvider) Dimensions() int {
	return cohereDimensions
}

// IsAvailable returns true if the provider is configured and available.
func (p *CohereProvider) IsAvailable() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.available
}

// GetEmbedding generates an embedding for the given text using Cohere API.
func (p *CohereProvider) GetEmbedding(ctx context.Context, text string) (EmbeddingResult, error) {
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return EmbeddingResult{}, fmt.Errorf(errRateLimiterFmt, err)
	}

	body, err := p.callCohereAPI(ctx, text)
	if err != nil {
		return EmbeddingResult{}, err
	}

	return p.parseEmbeddingResponse(body)
}

// callCohereAPI makes the HTTP request to Cohere API.
func (p *CohereProvider) callCohereAPI(ctx context.Context, text string) ([]byte, error) {
	reqBody := cohereEmbedRequest{
		Texts:     []string{text},
		Model:     p.model,
		InputType: "search_document", // Use "search_query" for search queries
	}

	jsonData, err := json.Marshal(reqBody) //nolint:errchkjson // reqBody contains only strings
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, CohereAPIEndpoint, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set(headerContentType, contentTypeJSON)
	req.Header.Set("Accept", contentTypeJSON)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cohere request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, p.parseAPIError(body, resp.StatusCode)
	}

	return body, nil
}

// parseAPIError extracts error details from the API response.
func (p *CohereProvider) parseAPIError(body []byte, statusCode int) error {
	var errResp cohereErrorResponse
	if jsonErr := json.Unmarshal(body, &errResp); jsonErr == nil && errResp.Message != "" {
		return fmt.Errorf("%w (%d): %s", ErrCohereAPIFailure, statusCode, errResp.Message)
	}

	return fmt.Errorf("%w: status %d", ErrCohereAPIFailure, statusCode)
}

// parseEmbeddingResponse parses the Cohere API response.
func (p *CohereProvider) parseEmbeddingResponse(body []byte) (EmbeddingResult, error) {
	var cohereResp cohereEmbedResponse
	if err := json.Unmarshal(body, &cohereResp); err != nil {
		return EmbeddingResult{}, fmt.Errorf("decode response: %w", err)
	}

	if len(cohereResp.Embeddings) == 0 {
		return EmbeddingResult{}, ErrCohereEmptyResponse
	}

	return EmbeddingResult{
		Vector:     cohereResp.Embeddings[0],
		Dimensions: len(cohereResp.Embeddings[0]),
		Provider:   ProviderCohere,
	}, nil
}
