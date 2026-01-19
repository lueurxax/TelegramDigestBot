package enrichment

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

var errNotImplemented = errors.New("not implemented in mock")

func TestTruncateText(t *testing.T) {
	tests := []struct {
		text     string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"longer than limit", 10, "longer tha..."},
		{"", 10, ""},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc..."},
	}

	for _, tt := range tests {
		got := truncateText(tt.text, tt.maxLen)
		if got != tt.expected {
			t.Errorf("truncateText(%q, %d) = %q, want %q", tt.text, tt.maxLen, got, tt.expected)
		}
	}
}

// mockEmbeddingClient is a test implementation of EmbeddingClient.
type mockEmbeddingClient struct {
	embedding   []float32
	err         error
	called      bool
	lastTextArg string
}

func (m *mockEmbeddingClient) GetEmbedding(_ context.Context, text string) ([]float32, error) {
	m.called = true
	m.lastTextArg = text

	return m.embedding, m.err
}

// mockRepository is a minimal test implementation of Repository.
type mockRepository struct {
	similarClaim     *db.EvidenceClaim
	similarClaimErr  error
	savedClaims      []*db.EvidenceClaim
	findSimilarCalls int
}

func (m *mockRepository) ClaimNextEnrichment(_ context.Context) (*db.EnrichmentQueueItem, error) {
	return nil, errNotImplemented
}

func (m *mockRepository) UpdateEnrichmentStatus(_ context.Context, _, _, _ string, _ *time.Time) error {
	return nil
}

func (m *mockRepository) GetEvidenceSource(_ context.Context, _ string) (*db.EvidenceSource, error) {
	return nil, errNotImplemented
}

func (m *mockRepository) SaveEvidenceSource(_ context.Context, _ *db.EvidenceSource) (string, error) {
	return "mock-source-id", nil
}

func (m *mockRepository) SaveEvidenceClaim(_ context.Context, claim *db.EvidenceClaim) (string, error) {
	m.savedClaims = append(m.savedClaims, claim)
	return "claim-id", nil
}

func (m *mockRepository) SaveItemEvidence(_ context.Context, _ *db.ItemEvidence) error {
	return nil
}

func (m *mockRepository) UpdateItemFactCheckScore(_ context.Context, _ string, _ float32, _, _ string) error {
	return nil
}

func (m *mockRepository) DeleteExpiredEvidenceSources(_ context.Context) (int64, error) {
	return 0, nil
}

func (m *mockRepository) CleanupExcessEvidencePerItem(_ context.Context, _ int) (int64, error) {
	return 0, nil
}

func (m *mockRepository) DeduplicateEvidenceClaims(_ context.Context) (int64, error) {
	return 0, nil
}

func (m *mockRepository) FindSimilarClaim(_ context.Context, _ string, _ []float32, _ float32) (*db.EvidenceClaim, error) {
	m.findSimilarCalls++
	return m.similarClaim, m.similarClaimErr
}

func (m *mockRepository) GetDailyEnrichmentCount(_ context.Context) (int, error) {
	return 0, nil
}

func (m *mockRepository) GetMonthlyEnrichmentCount(_ context.Context) (int, error) {
	return 0, nil
}

func (m *mockRepository) GetDailyEnrichmentCost(_ context.Context) (float64, error) {
	return 0, nil
}

func (m *mockRepository) GetMonthlyEnrichmentCost(_ context.Context) (float64, error) {
	return 0, nil
}

func (m *mockRepository) IncrementEnrichmentUsage(_ context.Context, _ string, _ float64) error {
	return nil
}

func (m *mockRepository) IncrementEmbeddingUsage(_ context.Context, _ float64) error {
	return errNotImplemented
}

func (m *mockRepository) GetLinksForMessage(_ context.Context, _ string) ([]domain.ResolvedLink, error) {
	return nil, nil
}

func (m *mockRepository) GetSetting(_ context.Context, _ string, _ interface{}) error {
	return errNotImplemented
}

func TestWorker_generateClaimEmbedding(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("returns nil when no embedding client", func(t *testing.T) {
		w := &Worker{
			embeddingClient: nil,
			logger:          &logger,
		}

		result := w.generateClaimEmbedding(context.Background(), "sample claim text")
		if result != nil {
			t.Error("expected nil embedding when client is nil")
		}
	})

	t.Run("returns embedding from client", func(t *testing.T) {
		expectedEmb := []float32{0.1, 0.2, 0.3}
		mock := &mockEmbeddingClient{embedding: expectedEmb}
		repo := &mockRepository{}
		w := &Worker{
			db:              repo,
			embeddingClient: mock,
			logger:          &logger,
		}

		result := w.generateClaimEmbedding(context.Background(), "another claim text")

		if !mock.called {
			t.Error("expected embedding client to be called")
		}

		if len(result) != len(expectedEmb) {
			t.Errorf("embedding length: got %d, want %d", len(result), len(expectedEmb))
		}
	})
}

func TestWorker_saveClaimsWithDedup(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("saves claims without embedding when client is nil", func(t *testing.T) {
		repo := &mockRepository{}
		cfg := &config.Config{EnrichmentDedupSimilarity: 0.98}

		w := &Worker{
			cfg:             cfg,
			db:              repo,
			embeddingClient: nil,
			logger:          &logger,
		}

		claims := []ExtractedClaim{
			{Text: "claim 1"},
			{Text: "claim 2"},
		}

		w.saveClaimsWithDedup(context.Background(), "source-a", claims)

		if len(repo.savedClaims) != 2 {
			t.Errorf("saved claims: got %d, want 2", len(repo.savedClaims))
		}

		// No similarity checks when no embeddings
		if repo.findSimilarCalls != 0 {
			t.Errorf("findSimilar without embeddings: got %d, want 0", repo.findSimilarCalls)
		}
	})

	t.Run("skips duplicate claims when similar claim exists", func(t *testing.T) {
		existingClaim := &db.EvidenceClaim{ID: "existing-id", ClaimText: "existing claim"}
		repo := &mockRepository{similarClaim: existingClaim}
		embClient := &mockEmbeddingClient{embedding: []float32{0.1, 0.2, 0.3}}
		cfg := &config.Config{EnrichmentDedupSimilarity: 0.98}

		w := &Worker{
			cfg:             cfg,
			db:              repo,
			embeddingClient: embClient,
			logger:          &logger,
		}

		claims := []ExtractedClaim{
			{Text: "duplicate claim"},
		}

		w.saveClaimsWithDedup(context.Background(), "source-b", claims)

		// Should check for similar claim
		if repo.findSimilarCalls != 1 {
			t.Errorf("findSimilar for duplicate: got %d, want 1", repo.findSimilarCalls)
		}

		// Should NOT save since similar exists
		if len(repo.savedClaims) != 0 {
			t.Errorf("saved claims: got %d, want 0 (duplicate should be skipped)", len(repo.savedClaims))
		}
	})

	t.Run("saves claim when no similar claim exists", func(t *testing.T) {
		repo := &mockRepository{similarClaim: nil} // No similar claim found
		embClient := &mockEmbeddingClient{embedding: []float32{0.1, 0.2, 0.3}}
		cfg := &config.Config{EnrichmentDedupSimilarity: 0.98}

		w := &Worker{
			cfg:             cfg,
			db:              repo,
			embeddingClient: embClient,
			logger:          &logger,
		}

		claims := []ExtractedClaim{
			{Text: "new unique claim"},
		}

		w.saveClaimsWithDedup(context.Background(), "source-c", claims)

		if repo.findSimilarCalls != 1 {
			t.Errorf("findSimilar for unique: got %d, want 1", repo.findSimilarCalls)
		}

		if len(repo.savedClaims) != 1 {
			t.Errorf("saved claims: got %d, want 1", len(repo.savedClaims))
		}

		// Check that embedding was saved
		if len(repo.savedClaims[0].Embedding.Slice()) == 0 {
			t.Error("expected embedding to be saved with claim")
		}
	})

	t.Run("uses default similarity when config is zero", func(t *testing.T) {
		repo := &mockRepository{}
		embClient := &mockEmbeddingClient{embedding: []float32{0.1}}
		cfg := &config.Config{EnrichmentDedupSimilarity: 0} // Will use default

		w := &Worker{
			cfg:             cfg,
			db:              repo,
			embeddingClient: embClient,
			logger:          &logger,
		}

		claims := []ExtractedClaim{{Text: "test"}}
		w.saveClaimsWithDedup(context.Background(), "source-d", claims)

		// Should still work with default similarity
		if repo.findSimilarCalls != 1 {
			t.Errorf("findSimilar with default: got %d, want 1", repo.findSimilarCalls)
		}
	})
}
