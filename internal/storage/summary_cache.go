package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/lueurxax/telegram-digest-bot/internal/storage/sqlc"
)

// ErrSummaryCacheNotFound is returned when a summary cache entry does not exist.
var ErrSummaryCacheNotFound = errors.New("summary cache entry not found")

type SummaryCacheEntry struct {
	CanonicalHash   string
	DigestLanguage  string
	Summary         string
	Topic           string
	Language        string
	RelevanceScore  float32
	ImportanceScore float32
	UpdatedAt       time.Time
}

func (db *DB) GetSummaryCache(ctx context.Context, canonicalHash, digestLanguage string) (*SummaryCacheEntry, error) {
	row, err := db.Queries.GetSummaryCache(ctx, sqlc.GetSummaryCacheParams{
		CanonicalHash:  canonicalHash,
		DigestLanguage: digestLanguage,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSummaryCacheNotFound
		}

		return nil, fmt.Errorf("get summary cache: %w", err)
	}

	return &SummaryCacheEntry{
		CanonicalHash:   row.CanonicalHash,
		DigestLanguage:  row.DigestLanguage,
		Summary:         row.Summary,
		Topic:           row.Topic.String,
		Language:        row.Language.String,
		RelevanceScore:  row.RelevanceScore,
		ImportanceScore: row.ImportanceScore,
		UpdatedAt:       row.UpdatedAt.Time,
	}, nil
}

func (db *DB) UpsertSummaryCache(ctx context.Context, entry *SummaryCacheEntry) error {
	if entry == nil {
		return nil
	}

	err := db.Queries.UpsertSummaryCache(ctx, sqlc.UpsertSummaryCacheParams{
		CanonicalHash:   entry.CanonicalHash,
		DigestLanguage:  entry.DigestLanguage,
		Summary:         SanitizeUTF8(entry.Summary),
		Topic:           toText(entry.Topic),
		Language:        toText(entry.Language),
		RelevanceScore:  entry.RelevanceScore,
		ImportanceScore: entry.ImportanceScore,
	})
	if err != nil {
		return fmt.Errorf("upsert summary cache: %w", err)
	}

	return nil
}
