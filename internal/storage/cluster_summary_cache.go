package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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
	rows, err := db.Pool.Query(ctx, `
		SELECT cluster_fingerprint,
		       item_ids,
		       summary,
		       updated_at
		FROM cluster_summary_cache
		WHERE digest_language = $1
		  AND updated_at >= $2
	`, digestLanguage, since)
	if err != nil {
		return nil, fmt.Errorf("get cluster summary cache: %w", err)
	}
	defer rows.Close()

	var entries []ClusterSummaryCacheEntry

	for rows.Next() {
		var (
			fingerprint string
			itemIDsRaw  []byte
			summary     string
			updated     pgtype.Timestamptz
		)

		if err := rows.Scan(&fingerprint, &itemIDsRaw, &summary, &updated); err != nil {
			return nil, fmt.Errorf("scan cluster summary cache: %w", err)
		}

		var itemIDs []string
		if len(itemIDsRaw) > 0 {
			if err := json.Unmarshal(itemIDsRaw, &itemIDs); err != nil {
				return nil, fmt.Errorf("unmarshal cluster summary cache items: %w", err)
			}
		}

		entries = append(entries, ClusterSummaryCacheEntry{
			DigestLanguage:     digestLanguage,
			ClusterFingerprint: fingerprint,
			ItemIDs:            itemIDs,
			Summary:            summary,
			UpdatedAt:          updated.Time,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cluster summary cache: %w", err)
	}

	return entries, nil
}

func (db *DB) UpsertClusterSummaryCache(ctx context.Context, entry *ClusterSummaryCacheEntry) error {
	if entry == nil {
		return nil
	}

	// json.Marshal on []string is always safe and cannot fail
	payload, _ := json.Marshal(entry.ItemIDs)

	_, err := db.Pool.Exec(ctx, `
		INSERT INTO cluster_summary_cache (
			digest_language,
			cluster_fingerprint,
			item_ids,
			summary,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, now(), now())
		ON CONFLICT (digest_language, cluster_fingerprint) DO UPDATE SET
			item_ids = EXCLUDED.item_ids,
			summary = EXCLUDED.summary,
			updated_at = now()
	`, entry.DigestLanguage, entry.ClusterFingerprint, payload, SanitizeUTF8(entry.Summary))
	if err != nil {
		return fmt.Errorf("upsert cluster summary cache: %w", err)
	}

	return nil
}

func (db *DB) GetClusterSummaryCacheEntry(ctx context.Context, digestLanguage, fingerprint string) (*ClusterSummaryCacheEntry, error) {
	row := db.Pool.QueryRow(ctx, `
		SELECT cluster_fingerprint,
		       item_ids,
		       summary,
		       updated_at
		FROM cluster_summary_cache
		WHERE digest_language = $1 AND cluster_fingerprint = $2
	`, digestLanguage, fingerprint)

	var (
		itemIDsRaw []byte
		summary    string
		updated    pgtype.Timestamptz
	)

	if err := row.Scan(&fingerprint, &itemIDsRaw, &summary, &updated); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrClusterSummaryCacheNotFound
		}

		return nil, fmt.Errorf("get cluster summary cache entry: %w", err)
	}

	var itemIDs []string
	if len(itemIDsRaw) > 0 {
		if err := json.Unmarshal(itemIDsRaw, &itemIDs); err != nil {
			return nil, fmt.Errorf("unmarshal cluster summary cache entry items: %w", err)
		}
	}

	return &ClusterSummaryCacheEntry{
		DigestLanguage:     digestLanguage,
		ClusterFingerprint: fingerprint,
		ItemIDs:            itemIDs,
		Summary:            summary,
		UpdatedAt:          updated.Time,
	}, nil
}
