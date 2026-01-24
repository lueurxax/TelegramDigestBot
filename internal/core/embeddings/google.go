package embeddings

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/google/generative-ai-go/genai"
	"golang.org/x/time/rate"
	"google.golang.org/api/option"
)

// Google embedding constants.
const (
	// ModelGeminiEmbedding001 is the latest Google embedding model (replaces text-embedding-004).
	ModelGeminiEmbedding001 = "gemini-embedding-001"

	// gemini-embedding-001 produces 3072-dimensional vectors by default.
	// Can be truncated to 768, 1536, or 3072 via output_dimensionality.
	googleDimensions = 3072

	// Default rate limiter burst for Google.
	googleRateLimiterBurst = 5
)

// Google embedding errors.
var (
	ErrGoogleEmptyResponse = errors.New("empty embedding response from Google")
	ErrGoogleAPIFailure    = errors.New("google embedding API error")
)

// GoogleProvider implements the embedding Provider interface for Google Gemini.
type GoogleProvider struct {
	client      *genai.Client
	model       string
	rateLimiter *rate.Limiter
	mu          sync.RWMutex
	available   bool
}

// GoogleConfig holds configuration for the Google embedding provider.
type GoogleConfig struct {
	APIKey    string
	Model     string // Default: "gemini-embedding-001"
	RateLimit int    // Requests per second
}

// NewGoogleProvider creates a new Google embedding provider.
func NewGoogleProvider(ctx context.Context, cfg GoogleConfig) (*GoogleProvider, error) {
	if cfg.APIKey == "" {
		return &GoogleProvider{available: false}, nil
	}

	if cfg.Model == "" {
		cfg.Model = ModelGeminiEmbedding001
	}

	if cfg.RateLimit == 0 {
		cfg.RateLimit = 1
	}

	client, err := genai.NewClient(ctx, option.WithAPIKey(cfg.APIKey))
	if err != nil {
		return nil, fmt.Errorf("creating google genai client: %w", err)
	}

	return &GoogleProvider{
		client:      client,
		model:       cfg.Model,
		rateLimiter: rate.NewLimiter(rate.Limit(cfg.RateLimit), googleRateLimiterBurst),
		available:   true,
	}, nil
}

// Name returns the provider identifier.
func (p *GoogleProvider) Name() ProviderName {
	return ProviderGoogle
}

// Priority returns the provider priority.
func (p *GoogleProvider) Priority() int {
	return PrioritySecondFallback
}

// Dimensions returns the output dimensions (3072 for gemini-embedding-001).
func (p *GoogleProvider) Dimensions() int {
	return googleDimensions
}

// IsAvailable returns true if the provider is configured and available.
func (p *GoogleProvider) IsAvailable() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.available
}

// GetEmbedding generates an embedding for the given text using Google Gemini API.
func (p *GoogleProvider) GetEmbedding(ctx context.Context, text string) (EmbeddingResult, error) {
	if err := p.rateLimiter.Wait(ctx); err != nil {
		return EmbeddingResult{}, fmt.Errorf(errRateLimiterFmt, err)
	}

	em := p.client.EmbeddingModel(p.model)

	resp, err := em.EmbedContent(ctx, genai.Text(text))
	if err != nil {
		return EmbeddingResult{}, fmt.Errorf("%w: %w", ErrGoogleAPIFailure, err)
	}

	if resp == nil || resp.Embedding == nil || len(resp.Embedding.Values) == 0 {
		return EmbeddingResult{}, ErrGoogleEmptyResponse
	}

	return EmbeddingResult{
		Vector:     resp.Embedding.Values,
		Dimensions: len(resp.Embedding.Values),
		Provider:   ProviderGoogle,
	}, nil
}

// Close closes the Google client.
func (p *GoogleProvider) Close() error {
	if p.client != nil {
		if err := p.client.Close(); err != nil {
			return fmt.Errorf("closing google embedding client: %w", err)
		}
	}

	return nil
}
