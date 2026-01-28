package dedup

import (
	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
)

// Log key constants for deduplication.
const (
	logKeySkippedID   = "skipped_id"
	logKeyDuplicateOf = "duplicate_of"
)

// DeduplicateScorables removes semantically similar items from a list.
// It keeps the first occurrence and removes subsequent duplicates based on
// cosine similarity of embeddings exceeding the threshold.
//
// This generic implementation works with any type implementing domain.Scorable,
// enabling uniform deduplication for Items, Bullets, and future scorable types.
func DeduplicateScorables[T domain.Scorable](items []T, threshold float32, logger *zerolog.Logger) []T {
	if len(items) == 0 {
		return items
	}

	result := make([]T, 0, len(items))

	for _, item := range items {
		embedding := item.GetEmbedding()
		if len(embedding) == 0 {
			// Keep items without embeddings (can't deduplicate them)
			result = append(result, item)
			continue
		}

		isDuplicate := false

		for _, kept := range result {
			keptEmbedding := kept.GetEmbedding()
			if len(keptEmbedding) == 0 {
				continue
			}

			similarity := CosineSimilarity(embedding, keptEmbedding)
			if similarity > threshold {
				if logger != nil {
					logger.Debug().
						Str(logKeySkippedID, item.GetID()).
						Str(logKeyDuplicateOf, kept.GetID()).
						Float32("similarity", similarity).
						Msg("Skipping semantic duplicate")
				}

				isDuplicate = true

				break
			}
		}

		if !isDuplicate {
			result = append(result, item)
		}
	}

	return result
}

// DeduplicateResult contains the result of deduplication with metadata.
type DeduplicateResult[T domain.Scorable] struct {
	// Items contains the deduplicated items.
	Items []T

	// DroppedCount is the number of items removed as duplicates.
	DroppedCount int

	// DuplicateMap maps dropped item IDs to the ID of the item they duplicated.
	DuplicateMap map[string]string
}

// DeduplicateScorablesFull performs deduplication and returns detailed results.
// Use this when you need to track which items were dropped and why.
func DeduplicateScorablesFull[T domain.Scorable](items []T, threshold float32, logger *zerolog.Logger) DeduplicateResult[T] {
	result := DeduplicateResult[T]{
		Items:        make([]T, 0, len(items)),
		DuplicateMap: make(map[string]string),
	}

	if len(items) == 0 {
		return result
	}

	for _, item := range items {
		embedding := item.GetEmbedding()
		if len(embedding) == 0 {
			result.Items = append(result.Items, item)
			continue
		}

		duplicateOf := ""

		for _, kept := range result.Items {
			keptEmbedding := kept.GetEmbedding()
			if len(keptEmbedding) == 0 {
				continue
			}

			similarity := CosineSimilarity(embedding, keptEmbedding)
			if similarity > threshold {
				duplicateOf = kept.GetID()

				break
			}
		}

		if duplicateOf != "" {
			result.DroppedCount++
			result.DuplicateMap[item.GetID()] = duplicateOf

			if logger != nil {
				logger.Debug().
					Str(logKeySkippedID, item.GetID()).
					Str(logKeyDuplicateOf, duplicateOf).
					Msg("Dropping semantic duplicate")
			}
		} else {
			result.Items = append(result.Items, item)
		}
	}

	return result
}

// FindDuplicates returns a list of items that are duplicates of items in the reference set.
// Useful for checking if new items duplicate existing ones without modifying the reference set.
func FindDuplicates[T domain.Scorable](candidates []T, reference []T, threshold float32) []T {
	duplicates := make([]T, 0)

	for _, candidate := range candidates {
		embedding := candidate.GetEmbedding()
		if len(embedding) == 0 {
			continue
		}

		for _, ref := range reference {
			refEmbedding := ref.GetEmbedding()
			if len(refEmbedding) == 0 {
				continue
			}

			if CosineSimilarity(embedding, refEmbedding) > threshold {
				duplicates = append(duplicates, candidate)

				break
			}
		}
	}

	return duplicates
}
