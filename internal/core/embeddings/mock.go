package embeddings

import (
	"context"
	"hash/fnv"
)

// Mock provider constants.
const (
	// LCG (Linear Congruential Generator) constants for deterministic pseudo-random generation.
	// These are standard values used in PCG/LCG algorithms.
	lcgMultiplier = 6364136223846793005
	lcgIncrement  = 1442695040888963407

	// Constants for float conversion.
	seedShift      = 33
	floatScale     = 0x40000000
	sqrtDivisor    = 2
	sqrtIterations = 10
)

// MockProvider implements the embedding Provider interface for testing.
// It generates deterministic embeddings based on input text hash.
type MockProvider struct {
	dimensions int
}

// NewMockProvider creates a new mock embedding provider.
func NewMockProvider() *MockProvider {
	return &MockProvider{
		dimensions: DefaultDimensions,
	}
}

// NewMockProviderWithDimensions creates a mock provider with custom dimensions.
func NewMockProviderWithDimensions(dims int) *MockProvider {
	return &MockProvider{
		dimensions: dims,
	}
}

// Name returns the provider identifier.
func (p *MockProvider) Name() ProviderName {
	return ProviderMock
}

// Priority returns the provider priority.
func (p *MockProvider) Priority() int {
	return PriorityMock
}

// Dimensions returns the output dimensions.
func (p *MockProvider) Dimensions() int {
	return p.dimensions
}

// IsAvailable returns true (mock is always available).
func (p *MockProvider) IsAvailable() bool {
	return true
}

// GetEmbedding generates a deterministic mock embedding based on text hash.
// This allows tests to get consistent embeddings for the same input.
func (p *MockProvider) GetEmbedding(_ context.Context, text string) (EmbeddingResult, error) {
	// Generate deterministic embedding from text hash
	h := fnv.New64a()
	_, _ = h.Write([]byte(text)) // fnv.Write never returns an error
	seed := h.Sum64()

	vec := make([]float32, p.dimensions)
	for i := range vec {
		// Generate pseudo-random values between -1 and 1
		// Using simple LCG with the hash as seed
		seed = seed*lcgMultiplier + lcgIncrement
		//nolint:gosec // intentional uint64->int64 conversion for pseudo-random generation
		vec[i] = float32(int64(seed>>seedShift)-floatScale) / float32(floatScale)
	}

	// Normalize the vector
	vec = normalizeVector(vec)

	return EmbeddingResult{
		Vector:     vec,
		Dimensions: p.dimensions,
		Provider:   ProviderMock,
	}, nil
}

// normalizeVector normalizes a vector to unit length.
func normalizeVector(vec []float32) []float32 {
	var sum float32
	for _, v := range vec {
		sum += v * v
	}

	if sum == 0 {
		return vec
	}

	norm := sqrt32(sum)
	for i := range vec {
		vec[i] /= norm
	}

	return vec
}

// sqrt32 computes square root for float32.
func sqrt32(x float32) float32 {
	// Newton's method for square root
	if x <= 0 {
		return 0
	}

	z := x
	for i := 0; i < sqrtIterations; i++ {
		z = (z + x/z) / sqrtDivisor
	}

	return z
}
