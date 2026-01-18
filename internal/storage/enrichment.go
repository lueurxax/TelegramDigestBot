package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pgvector/pgvector-go"
)

const (
	EnrichmentStatusPending    = "pending"
	EnrichmentStatusProcessing = "processing"
	EnrichmentStatusDone       = "done"
	EnrichmentStatusError      = "error"
)

const (
	FactCheckTierHigh   = "high"
	FactCheckTierMedium = "medium"
	FactCheckTierLow    = "low"
)

type EnrichmentQueueItem struct {
	ID           string
	ItemID       string
	Summary      string
	Topic        string
	ChannelTitle string
	AttemptCount int
}

type EnrichmentQueueStat struct {
	Status string
	Count  int
}

type EvidenceSource struct {
	ID               string
	URL              string
	URLHash          string
	Domain           string
	Title            string
	Description      string
	Content          string
	Author           string
	PublishedAt      *time.Time
	Language         string
	Provider         string
	ExtractionFailed bool
	FetchedAt        time.Time
	ExpiresAt        time.Time
}

type EvidenceClaim struct {
	ID          string
	EvidenceID  string
	ClaimText   string
	EntitiesRaw []byte
	Embedding   pgvector.Vector
	CreatedAt   time.Time
}

type ItemEvidence struct {
	ID                string
	ItemID            string
	EvidenceID        string
	AgreementScore    float32
	IsContradiction   bool
	MatchedClaimsJSON []byte
	MatchedAt         time.Time
}

type ItemEvidenceWithSource struct {
	ItemEvidence
	Source EvidenceSource
}

func (db *DB) EnqueueEnrichment(ctx context.Context, itemID, summary string) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO enrichment_queue (item_id, summary)
		VALUES ($1, $2)
		ON CONFLICT (item_id) DO NOTHING
	`, toUUID(itemID), summary)
	if err != nil {
		return fmt.Errorf("enqueue enrichment: %w", err)
	}

	return nil
}

func (db *DB) ClaimNextEnrichment(ctx context.Context) (*EnrichmentQueueItem, error) {
	var (
		item     EnrichmentQueueItem
		queueID  uuid.UUID
		itemUUID uuid.UUID
		topic    pgtype.Text
	)

	err := db.Pool.QueryRow(ctx, `
		WITH picked AS (
			SELECT id
			FROM enrichment_queue
			WHERE status = $1
			  AND (next_retry_at IS NULL OR next_retry_at <= now())
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		),
		updated AS (
			UPDATE enrichment_queue eq
			SET status = $2,
				attempt_count = eq.attempt_count + 1,
				updated_at = now()
			FROM picked
			WHERE eq.id = picked.id
			RETURNING eq.id, eq.item_id, eq.summary, eq.attempt_count
		)
		SELECT u.id, u.item_id, u.summary, u.attempt_count, i.topic, c.title
		FROM updated u
		JOIN items i ON i.id = u.item_id
		JOIN raw_messages rm ON rm.id = i.raw_message_id
		JOIN channels c ON c.id = rm.channel_id
	`, EnrichmentStatusPending, EnrichmentStatusProcessing).Scan(
		&queueID,
		&itemUUID,
		&item.Summary,
		&item.AttemptCount,
		&topic,
		&item.ChannelTitle,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil //nolint:nilnil // nil,nil indicates no pending enrichment available
		}

		return nil, fmt.Errorf("claim next enrichment: %w", err)
	}

	item.ID = queueID.String()
	item.ItemID = itemUUID.String()
	item.Topic = topic.String

	return &item, nil
}

func (db *DB) UpdateEnrichmentStatus(ctx context.Context, queueID, status, errMsg string, retryAt *time.Time) error {
	_, err := db.Pool.Exec(ctx, `
		UPDATE enrichment_queue
		SET status = $2,
			error_message = $3,
			next_retry_at = $4,
			updated_at = now()
		WHERE id = $1
	`, toUUID(queueID), status, errMsg, retryAt)
	if err != nil {
		return fmt.Errorf("update enrichment status: %w", err)
	}

	return nil
}

func (db *DB) CountPendingEnrichments(ctx context.Context) (int, error) {
	var count int

	err := db.Pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM enrichment_queue
		WHERE status = $1
	`, EnrichmentStatusPending).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count pending enrichments: %w", err)
	}

	return count, nil
}

func (db *DB) GetEnrichmentQueueStats(ctx context.Context) ([]EnrichmentQueueStat, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT status, COUNT(*)::bigint
		FROM enrichment_queue
		GROUP BY status
	`)
	if err != nil {
		return nil, fmt.Errorf("get enrichment queue stats: %w", err)
	}
	defer rows.Close()

	stats := []EnrichmentQueueStat{}

	for rows.Next() {
		var (
			status string
			count  int64
		)

		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("scan enrichment queue stats: %w", err)
		}

		stats = append(stats, EnrichmentQueueStat{
			Status: status,
			Count:  int(count),
		})
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate enrichment queue stats: %w", rows.Err())
	}

	return stats, nil
}

func URLHash(url string) string {
	h := sha256.Sum256([]byte(url))
	return hex.EncodeToString(h[:])
}

func (db *DB) GetEvidenceSource(ctx context.Context, urlHash string) (*EvidenceSource, error) {
	var (
		src         EvidenceSource
		id          uuid.UUID
		title       pgtype.Text
		description pgtype.Text
		content     pgtype.Text
		author      pgtype.Text
		publishedAt pgtype.Timestamptz
		language    pgtype.Text
	)

	err := db.Pool.QueryRow(ctx, `
		SELECT id, url, url_hash, domain, title, description, content, author,
		       published_at, language, provider, extraction_failed, fetched_at, expires_at
		FROM evidence_sources
		WHERE url_hash = $1
	`, urlHash).Scan(
		&id, &src.URL, &src.URLHash, &src.Domain,
		&title, &description, &content, &author,
		&publishedAt, &language, &src.Provider, &src.ExtractionFailed, &src.FetchedAt, &src.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil //nolint:nilnil // nil,nil indicates no evidence source found
		}

		return nil, fmt.Errorf("get evidence source: %w", err)
	}

	src.ID = id.String()
	src.Title = title.String
	src.Description = description.String
	src.Content = content.String
	src.Author = author.String
	src.Language = language.String

	if publishedAt.Valid {
		src.PublishedAt = &publishedAt.Time
	}

	return &src, nil
}

func (db *DB) SaveEvidenceSource(ctx context.Context, src *EvidenceSource) (string, error) {
	var id uuid.UUID

	err := db.Pool.QueryRow(ctx, `
		INSERT INTO evidence_sources (url, url_hash, domain, title, description, content,
		                              author, published_at, language, provider, extraction_failed, fetched_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (url_hash) DO UPDATE
		SET title = EXCLUDED.title,
			description = EXCLUDED.description,
			content = EXCLUDED.content,
			author = EXCLUDED.author,
			published_at = EXCLUDED.published_at,
			language = EXCLUDED.language,
			provider = EXCLUDED.provider,
			extraction_failed = EXCLUDED.extraction_failed,
			fetched_at = EXCLUDED.fetched_at,
			expires_at = EXCLUDED.expires_at
		RETURNING id
	`, src.URL, src.URLHash, src.Domain, toText(src.Title), toText(src.Description),
		toText(src.Content), toText(src.Author), toTimestamptzPtr(src.PublishedAt),
		toText(src.Language), src.Provider, src.ExtractionFailed, src.FetchedAt, src.ExpiresAt).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("save evidence source: %w", err)
	}

	return id.String(), nil
}

func (db *DB) SaveEvidenceClaim(ctx context.Context, claim *EvidenceClaim) (string, error) {
	var (
		id        uuid.UUID
		embedding any
	)

	if len(claim.Embedding.Slice()) > 0 {
		embedding = claim.Embedding
	}

	err := db.Pool.QueryRow(ctx, `
		INSERT INTO evidence_claims (evidence_id, claim_text, entities_json, embedding)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, toUUID(claim.EvidenceID), claim.ClaimText, claim.EntitiesRaw, embedding).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("save evidence claim: %w", err)
	}

	return id.String(), nil
}

func (db *DB) SaveItemEvidence(ctx context.Context, ie *ItemEvidence) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO item_evidence (item_id, evidence_id, agreement_score, is_contradiction, matched_claims_json, matched_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (item_id, evidence_id) DO UPDATE
		SET agreement_score = EXCLUDED.agreement_score,
			is_contradiction = EXCLUDED.is_contradiction,
			matched_claims_json = EXCLUDED.matched_claims_json,
			matched_at = EXCLUDED.matched_at
	`, toUUID(ie.ItemID), toUUID(ie.EvidenceID), ie.AgreementScore, ie.IsContradiction,
		ie.MatchedClaimsJSON, ie.MatchedAt)
	if err != nil {
		return fmt.Errorf("save item evidence: %w", err)
	}

	return nil
}

func (db *DB) UpdateItemFactCheckScore(ctx context.Context, itemID string, score float32, tier, notes string) error {
	_, err := db.Pool.Exec(ctx, `
		UPDATE items
		SET fact_check_score = $2,
			fact_check_tier = $3,
			fact_check_notes = $4
		WHERE id = $1
	`, toUUID(itemID), score, toText(tier), toText(notes))
	if err != nil {
		return fmt.Errorf("update item fact check score: %w", err)
	}

	return nil
}

func (db *DB) GetEvidenceForItems(ctx context.Context, itemIDs []string) (map[string][]ItemEvidenceWithSource, error) {
	if len(itemIDs) == 0 {
		return map[string][]ItemEvidenceWithSource{}, nil
	}

	ids := parseUUIDs(itemIDs)
	if len(ids) == 0 {
		return map[string][]ItemEvidenceWithSource{}, nil
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT ie.id, ie.item_id, ie.evidence_id, ie.agreement_score, ie.is_contradiction,
		       ie.matched_claims_json, ie.matched_at,
		       es.url, es.domain, es.title, es.description, es.author, es.published_at, es.language, es.provider
		FROM item_evidence ie
		JOIN evidence_sources es ON es.id = ie.evidence_id
		WHERE ie.item_id = ANY($1)
		ORDER BY ie.item_id, ie.agreement_score DESC
	`, ids)
	if err != nil {
		return nil, fmt.Errorf("get evidence for items: %w", err)
	}
	defer rows.Close()

	results := make(map[string][]ItemEvidenceWithSource)

	for rows.Next() {
		ies, err := scanItemEvidenceWithSource(rows)
		if err != nil {
			return nil, fmt.Errorf("scan item evidence: %w", err)
		}

		results[ies.ItemID] = append(results[ies.ItemID], ies)
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate item evidence: %w", rows.Err())
	}

	return results, nil
}

func parseUUIDs(ids []string) []uuid.UUID {
	result := make([]uuid.UUID, 0, len(ids))

	for _, id := range ids {
		parsed, err := uuid.Parse(id)
		if err != nil {
			continue
		}

		result = append(result, parsed)
	}

	return result
}

type evidenceRowScanner interface {
	Scan(dest ...any) error
}

func scanItemEvidenceWithSource(row evidenceRowScanner) (ItemEvidenceWithSource, error) {
	var (
		ies         ItemEvidenceWithSource
		ieID        uuid.UUID
		itemID      uuid.UUID
		evidenceID  uuid.UUID
		matchedJSON pgtype.Text
		title       pgtype.Text
		description pgtype.Text
		author      pgtype.Text
		publishedAt pgtype.Timestamptz
		language    pgtype.Text
	)

	if err := row.Scan(
		&ieID, &itemID, &evidenceID, &ies.AgreementScore, &ies.IsContradiction,
		&matchedJSON, &ies.MatchedAt,
		&ies.Source.URL, &ies.Source.Domain, &title, &description, &author,
		&publishedAt, &language, &ies.Source.Provider,
	); err != nil {
		return ItemEvidenceWithSource{}, fmt.Errorf("scan row: %w", err)
	}

	ies.ID = ieID.String()
	ies.ItemID = itemID.String()
	ies.EvidenceID = evidenceID.String()
	ies.Source.ID = evidenceID.String()
	ies.Source.Title = title.String
	ies.Source.Description = description.String
	ies.Source.Author = author.String
	ies.Source.Language = language.String

	if matchedJSON.Valid {
		ies.MatchedClaimsJSON = []byte(matchedJSON.String)
	}

	if publishedAt.Valid {
		ies.Source.PublishedAt = &publishedAt.Time
	}

	return ies, nil
}

func (db *DB) DeleteExpiredEvidenceSources(ctx context.Context) (int64, error) {
	tag, err := db.Pool.Exec(ctx, `
		DELETE FROM evidence_sources
		WHERE expires_at < now()
	`)
	if err != nil {
		return 0, fmt.Errorf("delete expired evidence sources: %w", err)
	}

	return tag.RowsAffected(), nil
}

func (db *DB) CountEvidenceSources(ctx context.Context) (int, error) {
	var count int64

	err := db.Pool.QueryRow(ctx, `
		SELECT COUNT(*)::bigint
		FROM evidence_sources
	`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count evidence sources: %w", err)
	}

	return int(count), nil
}

func (db *DB) CountItemEvidence(ctx context.Context) (int, error) {
	var count int64

	err := db.Pool.QueryRow(ctx, `
		SELECT COUNT(*)::bigint
		FROM item_evidence
	`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count item evidence: %w", err)
	}

	return int(count), nil
}

func (db *DB) CountItemEvidenceSince(ctx context.Context, since time.Time) (int, error) {
	var count int64

	err := db.Pool.QueryRow(ctx, `
		SELECT COUNT(*)::bigint
		FROM item_evidence
		WHERE matched_at >= $1
	`, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count item evidence since: %w", err)
	}

	return int(count), nil
}

// CleanupExcessEvidencePerItem removes evidence rows exceeding maxPerItem for each item,
// keeping only the ones with highest agreement scores.
func (db *DB) CleanupExcessEvidencePerItem(ctx context.Context, maxPerItem int) (int64, error) {
	if maxPerItem <= 0 {
		return 0, nil
	}

	tag, err := db.Pool.Exec(ctx, `
		DELETE FROM item_evidence
		WHERE id IN (
			SELECT id FROM (
				SELECT id,
					   ROW_NUMBER() OVER (PARTITION BY item_id ORDER BY agreement_score DESC, matched_at DESC) as rn
				FROM item_evidence
			) ranked
			WHERE rn > $1
		)
	`, maxPerItem)
	if err != nil {
		return 0, fmt.Errorf("cleanup excess evidence per item: %w", err)
	}

	return tag.RowsAffected(), nil
}

// DeduplicateEvidenceClaims removes duplicate evidence claims based on text similarity.
// Claims with identical text (after normalization) are merged, keeping the oldest.
func (db *DB) DeduplicateEvidenceClaims(ctx context.Context) (int64, error) {
	tag, err := db.Pool.Exec(ctx, `
		DELETE FROM evidence_claims
		WHERE id IN (
			SELECT id FROM (
				SELECT id,
					   ROW_NUMBER() OVER (
						   PARTITION BY evidence_id, LOWER(TRIM(claim_text))
						   ORDER BY created_at ASC
					   ) as rn
				FROM evidence_claims
			) ranked
			WHERE rn > 1
		)
	`)
	if err != nil {
		return 0, fmt.Errorf("deduplicate evidence claims: %w", err)
	}

	return tag.RowsAffected(), nil
}

// EnrichmentUsage represents daily usage counters for enrichment.
type EnrichmentUsage struct {
	Date           string
	Provider       string
	RequestCount   int
	EmbeddingCount int
	CostUSD        float64
}

// IncrementEnrichmentUsage increments the request counter and cost for the current day and provider.
func (db *DB) IncrementEnrichmentUsage(ctx context.Context, provider string, cost float64) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO enrichment_usage (date, provider, request_count, embedding_count, cost_usd)
		VALUES (CURRENT_DATE, $1, 1, 0, $2)
		ON CONFLICT (date, provider)
		DO UPDATE SET
			request_count = enrichment_usage.request_count + 1,
			cost_usd = enrichment_usage.cost_usd + EXCLUDED.cost_usd,
			updated_at = now()
	`, provider, cost)
	if err != nil {
		return fmt.Errorf("increment enrichment usage: %w", err)
	}

	return nil
}

// IncrementEmbeddingUsage increments the embedding counter and cost for the current day.
func (db *DB) IncrementEmbeddingUsage(ctx context.Context, cost float64) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO enrichment_usage (date, provider, request_count, embedding_count, cost_usd)
		VALUES (CURRENT_DATE, 'embedding', 0, 1, $1)
		ON CONFLICT (date, provider)
		DO UPDATE SET
			embedding_count = enrichment_usage.embedding_count + 1,
			cost_usd = enrichment_usage.cost_usd + EXCLUDED.cost_usd,
			updated_at = now()
	`, cost)
	if err != nil {
		return fmt.Errorf("increment embedding usage: %w", err)
	}

	return nil
}

// GetDailyEnrichmentCount returns the total enrichment request count for the current day.
func (db *DB) GetDailyEnrichmentCount(ctx context.Context) (int, error) {
	var count int64

	err := db.Pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(request_count), 0)::bigint
		FROM enrichment_usage
		WHERE date = CURRENT_DATE
	`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("get daily enrichment count: %w", err)
	}

	return int(count) + int(db.getEmbeddingCount(ctx)), nil
}

func (db *DB) getEmbeddingCount(ctx context.Context) int64 {
	var count int64
	_ = db.Pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(embedding_count), 0)::bigint
		FROM enrichment_usage
		WHERE date = CURRENT_DATE
	`).Scan(&count)

	return count
}

// GetMonthlyEnrichmentCount returns the total enrichment request count for the current month.
func (db *DB) GetMonthlyEnrichmentCount(ctx context.Context) (int, error) {
	var count int64

	err := db.Pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(request_count + embedding_count), 0)::bigint
		FROM enrichment_usage
		WHERE date >= DATE_TRUNC('month', CURRENT_DATE)
	`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("get monthly enrichment count: %w", err)
	}

	return int(count), nil
}

// GetDailyEnrichmentCost returns the total enrichment cost in USD for the current day.
func (db *DB) GetDailyEnrichmentCost(ctx context.Context) (float64, error) {
	var cost float64

	err := db.Pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(cost_usd), 0)::numeric
		FROM enrichment_usage
		WHERE date = CURRENT_DATE
	`).Scan(&cost)
	if err != nil {
		return 0, fmt.Errorf("get daily enrichment cost: %w", err)
	}

	return cost, nil
}

// GetMonthlyEnrichmentCost returns the total enrichment cost in USD for the current month.
func (db *DB) GetMonthlyEnrichmentCost(ctx context.Context) (float64, error) {
	var cost float64

	err := db.Pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(cost_usd), 0)::numeric
		FROM enrichment_usage
		WHERE date >= DATE_TRUNC('month', CURRENT_DATE)
	`).Scan(&cost)
	if err != nil {
		return 0, fmt.Errorf("get monthly enrichment cost: %w", err)
	}

	return cost, nil
}

// GetEnrichmentUsageStats returns usage statistics for display.
func (db *DB) GetEnrichmentUsageStats(ctx context.Context) (daily, monthly int, err error) {
	daily, err = db.GetDailyEnrichmentCount(ctx)
	if err != nil {
		return 0, 0, err
	}

	monthly, err = db.GetMonthlyEnrichmentCount(ctx)
	if err != nil {
		return 0, 0, err
	}

	return daily, monthly, nil
}

// FindSimilarClaim finds an existing claim with embedding similarity above the threshold.
// Uses pgvector cosine distance operator (<=>). Returns nil if no similar claim found.
// The similarity parameter should be 0-1 where 1 is identical; we convert to distance.
func (db *DB) FindSimilarClaim(ctx context.Context, evidenceID string, embedding []float32, similarity float32) (*EvidenceClaim, error) {
	if len(embedding) == 0 {
		return nil, nil //nolint:nilnil // no embedding means no similarity search possible
	}

	// Convert similarity (0-1) to cosine distance threshold
	// Cosine distance = 1 - cosine similarity
	// For similarity >= 0.98, we want distance <= 0.02
	distanceThreshold := 1 - similarity

	var (
		claim       EvidenceClaim
		id          uuid.UUID
		evidenceUID uuid.UUID
		entitiesRaw pgtype.Text
	)

	// pgvector uses <=> for cosine distance
	// We look for claims within the same evidence source that are semantically similar
	err := db.Pool.QueryRow(ctx, `
		SELECT id, evidence_id, claim_text, entities_json, embedding, created_at
		FROM evidence_claims
		WHERE evidence_id = $1
		  AND embedding IS NOT NULL
		  AND embedding <=> $2::vector < $3
		ORDER BY embedding <=> $2::vector
		LIMIT 1
	`, toUUID(evidenceID), pgvector.NewVector(embedding), distanceThreshold).Scan(
		&id, &evidenceUID, &claim.ClaimText, &entitiesRaw, &claim.Embedding, &claim.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil //nolint:nilnil // nil means no similar claim found
		}

		return nil, fmt.Errorf("find similar claim: %w", err)
	}

	claim.ID = id.String()
	claim.EvidenceID = evidenceUID.String()

	if entitiesRaw.Valid {
		claim.EntitiesRaw = []byte(entitiesRaw.String)
	}

	return &claim, nil
}
