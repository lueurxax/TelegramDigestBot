package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	FactCheckStatusPending    = "pending"
	FactCheckStatusProcessing = "processing"
	FactCheckStatusDone       = "done"
	FactCheckStatusError      = "error"
)

type FactCheckQueueItem struct {
	ID              string
	ItemID          string
	Claim           string
	NormalizedClaim string
	AttemptCount    int
}

type FactCheckQueueStat struct {
	Status string
	Count  int
}

type FactCheckMatch struct {
	ItemID    string
	Claim     string
	URL       string
	Publisher string
	Rating    string
	MatchedAt time.Time
}

type FactCheckCacheEntry struct {
	NormalizedClaim string
	ResultJSON      []byte
	CachedAt        time.Time
}

func (db *DB) EnqueueFactCheck(ctx context.Context, itemID, claim, normalizedClaim string) error {
	claim = strings.ToValidUTF8(claim, "")
	normalizedClaim = strings.ToValidUTF8(normalizedClaim, "")

	_, err := db.Pool.Exec(ctx, `
		INSERT INTO fact_check_queue (item_id, claim, normalized_claim)
		VALUES ($1, $2, $3)
		ON CONFLICT (item_id) DO NOTHING
	`, toUUID(itemID), claim, normalizedClaim)
	if err != nil {
		return fmt.Errorf("enqueue fact check: %w", err)
	}

	return nil
}

func (db *DB) ClaimNextFactCheck(ctx context.Context) (*FactCheckQueueItem, error) {
	var (
		item     FactCheckQueueItem
		queueID  uuid.UUID
		itemUUID uuid.UUID
	)

	err := db.Pool.QueryRow(ctx, `
		WITH picked AS (
			SELECT id
			FROM fact_check_queue
			WHERE status = $1
			  AND (next_retry_at IS NULL OR next_retry_at <= now())
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE fact_check_queue fq
		SET status = $2,
			attempt_count = fq.attempt_count + 1,
			updated_at = now()
		FROM picked
		WHERE fq.id = picked.id
		RETURNING fq.id, fq.item_id, fq.claim, fq.normalized_claim, fq.attempt_count
	`, FactCheckStatusPending, FactCheckStatusProcessing).Scan(
		&queueID,
		&itemUUID,
		&item.Claim,
		&item.NormalizedClaim,
		&item.AttemptCount,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil //nolint:nilnil // nil,nil indicates no pending fact check available
		}

		return nil, fmt.Errorf("claim next fact check: %w", err)
	}

	item.ID = queueID.String()
	item.ItemID = itemUUID.String()

	return &item, nil
}

func (db *DB) UpdateFactCheckStatus(ctx context.Context, queueID, status, errMsg string, retryAt *time.Time) error {
	_, err := db.Pool.Exec(ctx, `
		UPDATE fact_check_queue
		SET status = $2,
			error_message = $3,
			next_retry_at = $4,
			updated_at = now()
		WHERE id = $1
	`, toUUID(queueID), status, errMsg, retryAt)
	if err != nil {
		return fmt.Errorf("update fact check status: %w", err)
	}

	return nil
}

func (db *DB) GetFactCheckCache(ctx context.Context, normalizedClaim string) (*FactCheckCacheEntry, error) {
	var entry FactCheckCacheEntry

	err := db.Pool.QueryRow(ctx, `
		SELECT normalized_claim, result_json, cached_at
		FROM fact_check_cache
		WHERE normalized_claim = $1
	`, normalizedClaim).Scan(&entry.NormalizedClaim, &entry.ResultJSON, &entry.CachedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil //nolint:nilnil // nil,nil indicates no fact check cache found
		}

		return nil, fmt.Errorf("get fact check cache: %w", err)
	}

	return &entry, nil
}

func (db *DB) CountPendingFactChecks(ctx context.Context) (int, error) {
	var count int

	err := db.Pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM fact_check_queue
		WHERE status = $1
	`, FactCheckStatusPending).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count pending fact checks: %w", err)
	}

	return count, nil
}

func (db *DB) DeleteFactCheckCacheBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := db.Pool.Exec(ctx, `
		DELETE FROM fact_check_cache
		WHERE cached_at < $1
	`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete fact check cache: %w", err)
	}

	return tag.RowsAffected(), nil
}

func (db *DB) SaveFactCheckCache(ctx context.Context, normalizedClaim string, payload []byte, cachedAt time.Time) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO fact_check_cache (normalized_claim, result_json, cached_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (normalized_claim) DO UPDATE
		SET result_json = EXCLUDED.result_json,
			cached_at = EXCLUDED.cached_at
	`, normalizedClaim, payload, cachedAt)
	if err != nil {
		return fmt.Errorf("save fact check cache: %w", err)
	}

	return nil
}

func (db *DB) SaveItemFactChecks(ctx context.Context, itemID string, matches []FactCheckMatch) error {
	if len(matches) == 0 {
		return nil
	}

	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin fact check save: %w", err)
	}

	defer func() {
		_ = tx.Rollback(ctx) //nolint:errcheck // best-effort rollback
	}()

	for _, match := range matches {
		_, err := tx.Exec(ctx, `
			INSERT INTO item_fact_checks (item_id, claim, url, publisher, rating, matched_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (item_id, url) DO NOTHING
		`, toUUID(itemID), match.Claim, match.URL, match.Publisher, match.Rating, match.MatchedAt)
		if err != nil {
			return fmt.Errorf("save item fact check: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit fact check save: %w", err)
	}

	return nil
}

func (db *DB) GetFactChecksForItems(ctx context.Context, itemIDs []string) (map[string]FactCheckMatch, error) {
	if len(itemIDs) == 0 {
		return map[string]FactCheckMatch{}, nil
	}

	ids := make([]uuid.UUID, 0, len(itemIDs))
	for _, id := range itemIDs {
		parsed, err := uuid.Parse(id)
		if err != nil {
			continue
		}

		ids = append(ids, parsed)
	}

	if len(ids) == 0 {
		return map[string]FactCheckMatch{}, nil
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT DISTINCT ON (item_id) item_id, claim, url, publisher, rating, matched_at
		FROM item_fact_checks
		WHERE item_id = ANY($1)
		ORDER BY item_id, matched_at DESC
	`, ids)
	if err != nil {
		return nil, fmt.Errorf("get fact checks for items: %w", err)
	}
	defer rows.Close()

	results := make(map[string]FactCheckMatch)

	for rows.Next() {
		var (
			match  FactCheckMatch
			itemID uuid.UUID
		)

		if err := rows.Scan(&itemID, &match.Claim, &match.URL, &match.Publisher, &match.Rating, &match.MatchedAt); err != nil {
			return nil, fmt.Errorf("scan fact check: %w", err)
		}

		match.ItemID = itemID.String()
		results[match.ItemID] = match
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate fact checks: %w", rows.Err())
	}

	return results, nil
}

func (db *DB) GetFactCheckQueueStats(ctx context.Context) ([]FactCheckQueueStat, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT status, COUNT(*)::bigint
		FROM fact_check_queue
		GROUP BY status
	`)
	if err != nil {
		return nil, fmt.Errorf("get fact check queue stats: %w", err)
	}
	defer rows.Close()

	stats := []FactCheckQueueStat{}

	for rows.Next() {
		var (
			status string
			count  int64
		)

		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("scan fact check queue stats: %w", err)
		}

		stats = append(stats, FactCheckQueueStat{
			Status: status,
			Count:  int(count),
		})
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate fact check queue stats: %w", rows.Err())
	}

	return stats, nil
}

func (db *DB) GetFactCheckCacheCount(ctx context.Context) (int, error) {
	var count int64

	err := db.Pool.QueryRow(ctx, `
		SELECT COUNT(*)::bigint
		FROM fact_check_cache
	`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("get fact check cache count: %w", err)
	}

	return int(count), nil
}

func (db *DB) CountFactCheckMatches(ctx context.Context) (int, error) {
	var count int64

	err := db.Pool.QueryRow(ctx, `
		SELECT COUNT(*)::bigint
		FROM item_fact_checks
	`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count fact check matches: %w", err)
	}

	return int(count), nil
}

func (db *DB) CountFactCheckMatchesSince(ctx context.Context, since time.Time) (int, error) {
	var count int64

	err := db.Pool.QueryRow(ctx, `
		SELECT COUNT(*)::bigint
		FROM item_fact_checks
		WHERE matched_at >= $1
	`, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count fact check matches since: %w", err)
	}

	return int(count), nil
}

func (db *DB) GetRecentFactCheckMatches(ctx context.Context, limit int) ([]FactCheckMatch, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT item_id, claim, url, publisher, rating, matched_at
		FROM item_fact_checks
		ORDER BY matched_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("get recent fact check matches: %w", err)
	}
	defer rows.Close()

	matches := []FactCheckMatch{}

	for rows.Next() {
		var (
			itemID    uuid.UUID
			publisher pgtype.Text
			rating    pgtype.Text
			match     FactCheckMatch
		)

		if err := rows.Scan(&itemID, &match.Claim, &match.URL, &publisher, &rating, &match.MatchedAt); err != nil {
			return nil, fmt.Errorf("scan fact check match: %w", err)
		}

		match.ItemID = itemID.String()
		match.Publisher = publisher.String
		match.Rating = rating.String
		matches = append(matches, match)
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate fact check matches: %w", rows.Err())
	}

	return matches, nil
}
