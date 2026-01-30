package pipeline

import (
	"context"
	"fmt"
	"math"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

// DeduplicatePendingBullets processes pending bullets and marks duplicates.
// Uses higher threshold (0.92) than item dedup due to short string false positives.
func (p *Pipeline) DeduplicatePendingBullets(ctx context.Context, logger zerolog.Logger) error {
	threshold := p.cfg.BulletDedupThreshold
	if threshold <= 0 {
		threshold = defaultBulletDedupThreshold
	}

	bullets, err := p.database.GetPendingBulletsForDedup(ctx)
	if err != nil {
		return fmt.Errorf("get pending bullets for dedup: %w", err)
	}

	if len(bullets) == 0 {
		return nil
	}

	// Group bullets by item (to avoid marking bullets from the same item as duplicates of each other)
	byItem := make(map[string][]db.PendingBulletForDedup)
	for _, b := range bullets {
		byItem[b.ItemID] = append(byItem[b.ItemID], b)
	}

	// Find duplicates across items
	duplicateIDs := findDuplicateBullets(bullets, threshold)

	if len(duplicateIDs) > 0 {
		if err := p.database.MarkDuplicateBullets(ctx, duplicateIDs); err != nil {
			return fmt.Errorf("mark duplicate bullets: %w", err)
		}

		logger.Info().Int(LogFieldCount, len(duplicateIDs)).Msg("marked duplicate bullets")
	}

	// Mark remaining bullets as ready
	readyCount := 0

	for _, b := range bullets {
		if containsString(duplicateIDs, b.ID) {
			continue
		}

		if err := p.database.UpdateBulletStatus(ctx, b.ID, domain.BulletStatusReady); err != nil {
			logger.Warn().Err(err).Str(LogFieldBulletID, b.ID).Msg("failed to mark bullet as ready")

			continue
		}

		readyCount++
	}

	logger.Info().Int(LogFieldReady, readyCount).Int(LogFieldDuplicate, len(duplicateIDs)).Msg("bullet deduplication complete")

	return nil
}

// findDuplicateBullets finds bullets that are semantically similar to higher-scoring bullets.
func findDuplicateBullets(bullets []db.PendingBulletForDedup, threshold float64) []string {
	var duplicates []string

	// Track which bullets we've already marked as canonical (kept)
	canonical := make(map[string]bool)

	// Bullets are already sorted by importance_score DESC
	for i, bullet := range bullets {
		if len(bullet.Embedding) == 0 {
			continue
		}

		// Check if this bullet is already marked as duplicate
		if containsString(duplicates, bullet.ID) {
			continue
		}

		// Mark this bullet as canonical
		canonical[bullet.ID] = true

		// Compare with all subsequent (lower-scored) bullets
		for j := i + 1; j < len(bullets); j++ {
			other := bullets[j]

			if len(other.Embedding) == 0 {
				continue
			}

			// Skip if already marked
			if containsString(duplicates, other.ID) {
				continue
			}

			// Skip if from the same item (don't dedupe within same message)
			if bullet.ItemID == other.ItemID {
				continue
			}

			// Calculate cosine similarity
			similarity := cosineSimilarity(bullet.Embedding, other.Embedding)

			if similarity >= threshold {
				duplicates = append(duplicates, other.ID)
			}
		}
	}

	return duplicates
}

// cosineSimilarity calculates the cosine similarity between two embedding vectors.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dotProduct, normA, normB float64

	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// containsString checks if a string is in a slice.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}

	return false
}

// defaultBulletDedupThreshold is the default similarity threshold for bullet deduplication.
const defaultBulletDedupThreshold = 0.92
