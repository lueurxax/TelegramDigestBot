package embeddings

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/sashabaranov/go-openai"
	"golang.org/x/time/rate"
)

// OpenAI model constants.
const (
	ModelTextEmbedding3Large = "text-embedding-3-large"
	ModelTextEmbedding3Small = "text-embedding-3-small"

	// Default rate limiter burst.
	openaiRateLimiterBurst = 5
)

// OpenAI errors.
var ErrOpenAIEmptyResponse = errors.New("empty embedding response from OpenAI")

// OpenAIProvider implements the embedding Provider interface for OpenAI.
type OpenAIProvider struct {
	client      *openai.Client
	model       string
	dimensions  int
	rateLimiter *rate.Limiter
	mu          sync.RWMutex
	available   bool
}

// OpenAIConfig holds configuration for the OpenAI provider.
type OpenAIConfig struct {
	APIKey     string
	Model      string // "text-embedding-3-large" or "text-embedding-3-small"
	Dimensions int    // Output dimensions (3072 max for large, 1536 for small)
	RateLimit  int    // Requests per second
}

// NewOpenAIProvider creates a new OpenAI embedding provider.
func NewOpenAIProvider(cfg OpenAIConfig) *OpenAIProvider {
	if cfg.Model == "" {
		cfg.Model = ModelTextEmbedding3Large
	}

	if cfg.Dimensions == 0 {
		cfg.Dimensions = DefaultDimensions // 1536 to match existing DB schema
	}

	if cfg.RateLimit == 0 {
		cfg.RateLimit = 1
	}

	return &OpenAIProvider{
		client:      openai.NewClient(cfg.APIKey),
		model:       cfg.Model,
		dimensions:  cfg.Dimensions,
		rateLimiter: rate.NewLimiter(rate.Limit(cfg.RateLimit), openaiRateLimiterBurst),
		available:   cfg.APIKey != "" && cfg.APIKey != mockAPIKey,
	}
}

// Name returns the provider identifier.
func (p *OpenAIProvider) Name() ProviderName {
	return ProviderOpenAI
}

// Priority returns the provider priority.
func (p *OpenAIProvider) Priority() int {
	return PriorityPrimary
}

// Dimensions returns the configured output dimensions.
func (p *OpenAIProvider) Dimensions() int {
	return p.dimensions
}

// IsAvailable returns true if the provider is configured and available.
func (p *OpenAIProvider) IsAvailable() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.available
}

// GetEmbedding generates an embedding for the given text using OpenAI API.
func (p *OpenAIProvider) GetEmbedding(ctx context.Context, text string) (EmbeddingResult, error) {
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return EmbeddingResult{}, fmt.Errorf(errRateLimiterFmt, err)
	}

	req := openai.EmbeddingRequest{
		Input: []string{text},
		Model: openai.EmbeddingModel(p.model),
	}

	// text-embedding-3-large supports dimension reduction via API parameter
	// This allows us to request 1536 dimensions instead of 3072
	if p.model == ModelTextEmbedding3Large && p.dimensions > 0 && p.dimensions < maxLargeDimensions {
		req.Dimensions = p.dimensions
	}

	resp, err := p.client.CreateEmbeddings(ctx, req)
	if err != nil {
		return EmbeddingResult{}, fmt.Errorf("openai embeddings: %w", err)
	}

	if len(resp.Data) == 0 {
		return EmbeddingResult{}, ErrOpenAIEmptyResponse
	}

	return EmbeddingResult{
		Vector:     resp.Data[0].Embedding,
		Dimensions: len(resp.Data[0].Embedding),
		Provider:   ProviderOpenAI,
	}, nil
}

// Maximum dimensions for text-embedding-3-large.
const maxLargeDimensions = 3072
