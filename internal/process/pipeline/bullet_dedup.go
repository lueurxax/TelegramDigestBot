package pipeline

import (
	"context"
	"fmt"
	"math"

	"github.com/rs/zerolog"

	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

// DeduplicatePendingBullets processes pending bullets and marks duplicates.
// Uses higher threshold (0.92) than item dedup due to short string false positives.
// Also sets bullet_cluster_id to link duplicates to their canonical bullet for corroboration counting.
// Includes ready bullets from the lookback window for global deduplication.
func (p *Pipeline) DeduplicatePendingBullets(ctx context.Context, logger zerolog.Logger) error {
	threshold := p.cfg.BulletDedupThreshold
	if threshold <= 0 {
		threshold = defaultBulletDedupThreshold
	}

	lookbackHours := p.cfg.BulletDedupLookbackHours
	if lookbackHours <= 0 {
		lookbackHours = defaultDedupLookbackHours
	}

	bullets, err := p.database.GetPendingBulletsForDedup(ctx, lookbackHours)
	if err != nil {
		return fmt.Errorf("get pending bullets for dedup: %w", err)
	}

	if len(bullets) == 0 {
		return nil
	}

	// Find duplicates with their canonical bullet mappings
	// Only pending bullets can be marked as duplicates; ready bullets serve as canonical candidates
	duplicateToCanonical := findDuplicateBulletsWithCanonical(bullets, threshold)

	// Mark duplicates (only pending bullets)
	p.markDuplicates(ctx, logger, duplicateToCanonical)

	// Mark remaining pending bullets as canonical (ready bullets are already ready)
	readyCount := p.markCanonicalBullets(ctx, logger, bullets, duplicateToCanonical)

	logger.Info().Int(LogFieldReady, readyCount).Int(LogFieldDuplicate, len(duplicateToCanonical)).Msg("bullet deduplication complete")

	return nil
}

// markDuplicates marks bullets as duplicates and links them to their canonical bullets.
func (p *Pipeline) markDuplicates(ctx context.Context, logger zerolog.Logger, duplicateToCanonical map[string]string) {
	if len(duplicateToCanonical) == 0 {
		return
	}

	for duplicateID, canonicalID := range duplicateToCanonical {
		if err := p.database.MarkBulletAsDuplicateOf(ctx, duplicateID, canonicalID); err != nil {
			logger.Warn().Err(err).Str(LogFieldBulletID, duplicateID).Msg("failed to mark bullet as duplicate")
		}
	}

	logger.Info().Int(LogFieldCount, len(duplicateToCanonical)).Msg("marked duplicate bullets")
}

// markCanonicalBullets marks non-duplicate pending bullets as ready with cluster_id set to self.
// Only processes pending bullets; ready bullets from the global pool are already canonical.
func (p *Pipeline) markCanonicalBullets(ctx context.Context, logger zerolog.Logger, bullets []db.PendingBulletForDedup, duplicateToCanonical map[string]string) int {
	readyCount := 0

	for _, b := range bullets {
		// Skip bullets that are already ready (from global dedup pool)
		if b.Status == BulletStatusReady {
			continue
		}

		// Skip bullets marked as duplicates
		if _, isDupe := duplicateToCanonical[b.ID]; isDupe {
			continue
		}

		if err := p.database.MarkBulletAsCanonical(ctx, b.ID); err != nil {
			logger.Warn().Err(err).Str(LogFieldBulletID, b.ID).Msg("failed to mark bullet as ready")

			continue
		}

		readyCount++
	}

	return readyCount
}

// findDuplicateBulletsWithCanonical finds pending bullets that are semantically similar to canonical bullets.
// Returns a map of duplicate bullet ID -> canonical bullet ID for corroboration tracking.
// Only pending bullets can be marked as duplicates; ready bullets serve as canonical candidates only.
func findDuplicateBulletsWithCanonical(bullets []db.PendingBulletForDedup, threshold float64) map[string]string {
	duplicateToCanonical := make(map[string]string)

	// Bullets are sorted: ready first (canonical candidates), then pending by importance_score DESC
	for i, bullet := range bullets {
		if len(bullet.Embedding) == 0 {
			continue
		}

		// Skip if this bullet is already marked as duplicate
		if _, isDupe := duplicateToCanonical[bullet.ID]; isDupe {
			continue
		}

		// Compare with all subsequent bullets
		for j := i + 1; j < len(bullets); j++ {
			other := bullets[j]

			if len(other.Embedding) == 0 {
				continue
			}

			// Only pending bullets can be marked as duplicates
			if other.Status != BulletStatusPending {
				continue
			}

			// Skip if already marked
			if _, isDupe := duplicateToCanonical[other.ID]; isDupe {
				continue
			}

			// Skip if from the same item (don't dedupe within same message)
			if bullet.ItemID == other.ItemID {
				continue
			}

			// Calculate cosine similarity
			similarity := cosineSimilarity(bullet.Embedding, other.Embedding)

			if similarity >= threshold {
				duplicateToCanonical[other.ID] = bullet.ID
			}
		}
	}

	return duplicateToCanonical
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

// Bullet deduplication constants.
const (
	defaultBulletDedupThreshold = 0.92 // Default similarity threshold for bullet deduplication
	defaultDedupLookbackHours   = 48   // Default lookback window for global dedup pool
	BulletStatusPending         = "pending"
	BulletStatusReady           = "ready"
)
