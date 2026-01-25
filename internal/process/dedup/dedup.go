package dedup

import (
	"context"
	"fmt"
	"math"
	"time"

	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const (
	defaultDedupWindowDays = 7
	hoursPerDay            = 24
)

type Deduplicator interface {
	IsDuplicate(ctx context.Context, m db.RawMessage, embedding []float32) (bool, string, error)
}

type Repository interface {
	CheckStrictDuplicate(ctx context.Context, hash string, id string) (bool, error)
	FindSimilarItem(ctx context.Context, embedding []float32, threshold float32, minCreatedAt time.Time) (string, error)
}

type semanticDeduplicator struct {
	database  Repository
	threshold float32
	window    time.Duration
}

func NewSemantic(database Repository, threshold float32, window time.Duration) Deduplicator {
	return &semanticDeduplicator{
		database:  database,
		threshold: threshold,
		window:    window,
	}
}

func (d *semanticDeduplicator) IsDuplicate(ctx context.Context, _ db.RawMessage, embedding []float32) (bool, string, error) {
	if len(embedding) == 0 {
		return false, "", nil
	}

	window := d.window
	if window <= 0 {
		window = defaultDedupWindowDays * hoursPerDay * time.Hour
	}

	minCreatedAt := time.Now().Add(-window)

	similarItemID, err := d.database.FindSimilarItem(ctx, embedding, d.threshold, minCreatedAt)
	if err != nil {
		return false, "", fmt.Errorf("find similar item: %w", err)
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
		return false, "", fmt.Errorf("check strict duplicate: %w", err)
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
