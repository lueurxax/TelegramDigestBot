package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/lueurxax/telegram-digest-bot/internal/storage/sqlc"
)

// ErrClusterSummaryCacheNotFound is returned when a cluster summary cache entry does not exist.
var ErrClusterSummaryCacheNotFound = errors.New("cluster summary cache entry not found")

type ClusterSummaryCacheEntry struct {
	DigestLanguage     string
	ClusterFingerprint string
	ItemIDs            []string
	Summary            string
	UpdatedAt          time.Time
}

func (db *DB) GetClusterSummaryCache(ctx context.Context, digestLanguage string, since time.Time) ([]ClusterSummaryCacheEntry, error) {
	rows, err := db.Queries.GetClusterSummaryCache(ctx, sqlc.GetClusterSummaryCacheParams{
		DigestLanguage: digestLanguage,
		UpdatedAt:      pgtype.Timestamptz{Time: since, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("get cluster summary cache: %w", err)
	}

	entries := make([]ClusterSummaryCacheEntry, 0, len(rows))

	for _, row := range rows {
		var itemIDs []string
		if len(row.ItemIds) > 0 {
			if err := json.Unmarshal(row.ItemIds, &itemIDs); err != nil {
				return nil, fmt.Errorf("unmarshal cluster summary cache items: %w", err)
			}
		}

		entries = append(entries, ClusterSummaryCacheEntry{
			DigestLanguage:     digestLanguage,
			ClusterFingerprint: row.ClusterFingerprint,
			ItemIDs:            itemIDs,
			Summary:            row.Summary,
			UpdatedAt:          row.UpdatedAt.Time,
		})
	}

	return entries, nil
}

func (db *DB) UpsertClusterSummaryCache(ctx context.Context, entry *ClusterSummaryCacheEntry) error {
	if entry == nil {
		return nil
	}

	// json.Marshal on []string is always safe and cannot fail
	payload, _ := json.Marshal(entry.ItemIDs)

	err := db.Queries.UpsertClusterSummaryCache(ctx, sqlc.UpsertClusterSummaryCacheParams{
		DigestLanguage:     entry.DigestLanguage,
		ClusterFingerprint: entry.ClusterFingerprint,
		ItemIds:            payload,
		Summary:            SanitizeUTF8(entry.Summary),
	})
	if err != nil {
		return fmt.Errorf("upsert cluster summary cache: %w", err)
	}

	return nil
}

func (db *DB) GetClusterSummaryCacheEntry(ctx context.Context, digestLanguage, fingerprint string) (*ClusterSummaryCacheEntry, error) {
	row, err := db.Queries.GetClusterSummaryCacheEntry(ctx, sqlc.GetClusterSummaryCacheEntryParams{
		DigestLanguage:     digestLanguage,
		ClusterFingerprint: fingerprint,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrClusterSummaryCacheNotFound
		}

		return nil, fmt.Errorf("get cluster summary cache entry: %w", err)
	}

	var itemIDs []string
	if len(row.ItemIds) > 0 {
		if err := json.Unmarshal(row.ItemIds, &itemIDs); err != nil {
			return nil, fmt.Errorf("unmarshal cluster summary cache entry items: %w", err)
		}
	}

	return &ClusterSummaryCacheEntry{
		DigestLanguage:     digestLanguage,
		ClusterFingerprint: row.ClusterFingerprint,
		ItemIDs:            itemIDs,
		Summary:            row.Summary,
		UpdatedAt:          row.UpdatedAt.Time,
	}, nil
}
