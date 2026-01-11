package dedup

import (
	"context"
	"math"

	"github.com/lueurxax/telegram-digest-bot/internal/db"
)

type Deduplicator interface {
	IsDuplicate(ctx context.Context, m db.RawMessage, embedding []float32) (bool, string, error)
}

type Repository interface {
	CheckStrictDuplicate(ctx context.Context, hash string, id string) (bool, error)
	FindSimilarItem(ctx context.Context, embedding []float32, threshold float32) (string, error)
}

type semanticDeduplicator struct {
	database  Repository
	threshold float32
}

func NewSemantic(database Repository, threshold float32) Deduplicator {
	return &semanticDeduplicator{
		database:  database,
		threshold: threshold,
	}
}

func (d *semanticDeduplicator) IsDuplicate(ctx context.Context, m db.RawMessage, embedding []float32) (bool, string, error) {
	if len(embedding) == 0 {
		return false, "", nil
	}
	similarItemID, err := d.database.FindSimilarItem(ctx, embedding, d.threshold)
	if err != nil {
		return false, "", err
	}
	if similarItemID != "" {
		return true, similarItemID, nil
	}
	return false, "", nil
}

type strictDeduplicator struct {
	database Repository
}

func NewStrict(database Repository) Deduplicator {
	return &strictDeduplicator{
		database: database,
	}
}

func (d *strictDeduplicator) IsDuplicate(ctx context.Context, m db.RawMessage, _ []float32) (bool, string, error) {
	exists, err := d.database.CheckStrictDuplicate(ctx, m.CanonicalHash, m.ID)
	if err != nil {
		return false, "", err
	}
	if exists {
		return true, "strict_duplicate", nil
	}
	return false, "", nil
}

func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dotProduct, normA, normB float32
	for i := 0; i < len(a); i++ {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dotProduct / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}
