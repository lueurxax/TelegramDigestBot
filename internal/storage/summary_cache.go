package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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
	row := db.Pool.QueryRow(ctx, `
		SELECT canonical_hash,
		       digest_language,
		       summary,
		       topic,
		       language,
		       relevance_score,
		       importance_score,
		       updated_at
		FROM summary_cache
		WHERE canonical_hash = $1 AND digest_language = $2
	`, canonicalHash, digestLanguage)

	var (
		hash     string
		lang     string
		summary  string
		topic    pgtype.Text
		language pgtype.Text
		rel      float32
		imp      float32
		updated  pgtype.Timestamptz
	)

	if err := row.Scan(&hash, &lang, &summary, &topic, &language, &rel, &imp, &updated); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSummaryCacheNotFound
		}

		return nil, fmt.Errorf("get summary cache: %w", err)
	}

	return &SummaryCacheEntry{
		CanonicalHash:   hash,
		DigestLanguage:  lang,
		Summary:         summary,
		Topic:           topic.String,
		Language:        language.String,
		RelevanceScore:  rel,
		ImportanceScore: imp,
		UpdatedAt:       updated.Time,
	}, nil
}

func (db *DB) UpsertSummaryCache(ctx context.Context, entry *SummaryCacheEntry) error {
	if entry == nil {
		return nil
	}

	_, err := db.Pool.Exec(ctx, `
		INSERT INTO summary_cache (
			canonical_hash,
			digest_language,
			summary,
			topic,
			language,
			relevance_score,
			importance_score,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, now(), now())
		ON CONFLICT (canonical_hash, digest_language) DO UPDATE SET
			summary = EXCLUDED.summary,
			topic = EXCLUDED.topic,
			language = EXCLUDED.language,
			relevance_score = EXCLUDED.relevance_score,
			importance_score = EXCLUDED.importance_score,
			updated_at = now()
	`, entry.CanonicalHash, entry.DigestLanguage, SanitizeUTF8(entry.Summary), toText(entry.Topic), toText(entry.Language), entry.RelevanceScore, entry.ImportanceScore)
	if err != nil {
		return fmt.Errorf("upsert summary cache: %w", err)
	}

	return nil
}
