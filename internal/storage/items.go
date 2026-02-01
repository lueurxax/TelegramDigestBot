package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/storage/sqlc"
	"github.com/pgvector/pgvector-go"
)

const errSaveItemRating = "save item rating: %w"

// ErrSimilarIrrelevantItemNotFound is returned when no similar irrelevant item exists.
var ErrSimilarIrrelevantItemNotFound = errors.New("similar irrelevant item not found")

// Item is an alias for the domain type.
type Item = domain.Item

type SimilarIrrelevantItem struct {
	ItemID     string
	Similarity float64
}

type CanonicalItem struct {
	ItemID   string
	Summary  string
	Topic    string
	Language string
}

// ItemWithMedia extends Item with media data for inline image support.
type ItemWithMedia struct {
	Item
	MediaData []byte
}

type ItemRating struct {
	ChannelID string
	Rating    string
	CreatedAt time.Time
}

type ItemRatingDetail struct {
	UserID    int64
	Rating    string
	Feedback  string
	Source    string
	CreatedAt time.Time
}

func (db *DB) SaveItem(ctx context.Context, item *Item) error {
	id, err := db.Queries.SaveItem(ctx, sqlc.SaveItemParams{
		RawMessageID:        toUUID(item.RawMessageID),
		RelevanceScore:      item.RelevanceScore,
		ImportanceScore:     item.ImportanceScore,
		Topic:               toText(item.Topic),
		Summary:             toText(item.Summary),
		Language:            toText(item.Language),
		LanguageSource:      toText(item.LanguageSource),
		Status:              SanitizeUTF8(item.Status),
		BulletTotalCount:    safeIntToInt32(item.BulletTotalCount),
		BulletIncludedCount: safeIntToInt32(item.BulletIncludedCount),
	})
	if err != nil {
		return fmt.Errorf("save item: %w", err)
	}

	item.ID = fromUUID(id)

	return nil
}

func (db *DB) SaveItemError(ctx context.Context, rawMsgID string, errJSON []byte) error {
	if err := db.Queries.SaveItemError(ctx, sqlc.SaveItemErrorParams{
		RawMessageID: toUUID(rawMsgID),
		ErrorJson:    errJSON,
	}); err != nil {
		return fmt.Errorf("save item error: %w", err)
	}

	return nil
}

func (db *DB) FindSimilarItem(ctx context.Context, embedding []float32, threshold float32, minCreatedAt time.Time) (string, error) {
	id, err := db.Queries.FindSimilarItem(ctx, sqlc.FindSimilarItemParams{
		Embedding:    pgvector.NewVector(embedding),
		Threshold:    float64(1.0 - threshold),
		MinCreatedAt: toTimestamptz(minCreatedAt),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}

		return "", fmt.Errorf("find similar item: %w", err)
	}

	return fromUUID(id), nil
}

func (db *DB) FindSimilarItemForChannel(ctx context.Context, embedding []float32, channelID string, threshold float32, minCreatedAt time.Time) (string, error) {
	var id pgtype.UUID

	err := db.Pool.QueryRow(ctx, `
		SELECT e.item_id
		FROM embeddings e
		JOIN items i ON i.id = e.item_id
		JOIN raw_messages rm ON rm.id = i.raw_message_id
		WHERE rm.channel_id = $1
		  AND e.created_at > $2
		  AND (e.embedding <=> $3::vector) < $4
		ORDER BY e.embedding <=> $3::vector
		LIMIT 1
	`, toUUID(channelID), minCreatedAt, pgvector.NewVector(embedding), float64(1.0-threshold)).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}

		return "", fmt.Errorf("find similar item for channel: %w", err)
	}

	return fromUUID(id), nil
}

func (db *DB) GetItemByCanonicalURL(ctx context.Context, canonicalURL, excludeRawMsgID string) (*CanonicalItem, error) {
	row, err := db.Queries.GetItemByCanonicalURL(ctx, sqlc.GetItemByCanonicalURLParams{
		CanonicalUrl: toText(canonicalURL),
		RawMessageID: toUUID(excludeRawMsgID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil //nolint:nilnil // intentional: not found is not an error
		}

		return nil, fmt.Errorf("get item by canonical url: %w", err)
	}

	return &CanonicalItem{
		ItemID:   fromUUID(row.ID),
		Summary:  row.Summary.String,
		Topic:    row.Topic.String,
		Language: row.Language.String,
	}, nil
}

func (db *DB) UpsertItemLinkDebug(ctx context.Context, itemID string, linkContextUsed bool, linkContentLen int, canonicalDetected bool) error {
	if err := db.Queries.UpsertItemLinkDebug(ctx, sqlc.UpsertItemLinkDebugParams{
		ItemID:                  toUUID(itemID),
		LinkContextUsed:         linkContextUsed,
		LinkContentLen:          safeIntToInt32(linkContentLen),
		CanonicalSourceDetected: canonicalDetected,
	}); err != nil {
		return fmt.Errorf("upsert item link debug: %w", err)
	}

	return nil
}

func (db *DB) AddItemLinkLangQueries(ctx context.Context, itemID string, count int) error {
	if err := db.Queries.AddItemLinkLangQueries(ctx, sqlc.AddItemLinkLangQueriesParams{
		ItemID:          toUUID(itemID),
		LinkLangQueries: safeIntToInt32(count),
	}); err != nil {
		return fmt.Errorf("add item link lang queries: %w", err)
	}

	return nil
}

func (db *DB) UpsertItemCanonicalLink(ctx context.Context, itemID, canonicalItemID, canonicalURL string, similarity float32) error {
	if err := db.Queries.UpsertItemCanonicalLink(ctx, sqlc.UpsertItemCanonicalLinkParams{
		ItemID:          toUUID(itemID),
		CanonicalItemID: toUUID(canonicalItemID),
		CanonicalUrl:    SanitizeUTF8(canonicalURL),
		Similarity:      toFloat4(similarity),
	}); err != nil {
		return fmt.Errorf("upsert item canonical link: %w", err)
	}

	return nil
}

func (db *DB) FindSimilarIrrelevantItem(ctx context.Context, embedding []float32, since time.Time) (*SimilarIrrelevantItem, error) {
	var (
		itemID     pgtype.UUID
		similarity float64
	)

	err := db.Pool.QueryRow(ctx, `
		SELECT e.item_id,
		       1 - (e.embedding <=> $1::vector) AS similarity
		FROM embeddings e
		WHERE EXISTS (
			SELECT 1
			FROM item_ratings ir
			WHERE ir.item_id = e.item_id
			  AND ir.rating = 'irrelevant'
			  AND ir.created_at >= $2
		)
		ORDER BY e.embedding <=> $1::vector
		LIMIT 1
	`, pgvector.NewVector(embedding), toTimestamptz(since)).Scan(&itemID, &similarity)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSimilarIrrelevantItemNotFound
		}

		return nil, fmt.Errorf("find similar irrelevant item: %w", err)
	}

	return &SimilarIrrelevantItem{
		ItemID:     fromUUID(itemID),
		Similarity: similarity,
	}, nil
}

func (db *DB) MarkItemsAsDigested(ctx context.Context, ids []string) error {
	uuids := make([]pgtype.UUID, len(ids))
	for i, id := range ids {
		uuids[i] = toUUID(id)
	}

	if err := db.Queries.MarkItemsAsDigested(ctx, uuids); err != nil {
		return fmt.Errorf("mark items as digested: %w", err)
	}

	return nil
}

func (db *DB) GetRecentErrors(ctx context.Context, limit int) ([]Item, error) {
	rows, err := db.Queries.GetRecentErrors(ctx, safeIntToInt32(limit))
	if err != nil {
		return nil, fmt.Errorf("get recent errors: %w", err)
	}

	items := make([]Item, len(rows))
	for i, r := range rows {
		items[i] = Item{
			ID:              fromUUID(r.ID),
			RawMessageID:    fromUUID(r.RawMessageID),
			ErrorJSON:       r.ErrorJson,
			CreatedAt:       r.CreatedAt.Time,
			SourceChannel:   r.ChannelUsername.String,
			SourceChannelID: r.ChannelPeerID,
			SourceMsgID:     r.SourceMsgID,
		}
	}

	return items, nil
}

func (db *DB) RetryFailedItems(ctx context.Context) error {
	if err := db.Queries.RetryFailedItems(ctx); err != nil {
		return fmt.Errorf("retry failed items: %w", err)
	}

	return nil
}

func (db *DB) RetryItem(ctx context.Context, id string) error {
	if err := db.Queries.RetryItem(ctx, toUUID(id)); err != nil {
		return fmt.Errorf("retry item: %w", err)
	}

	return nil
}

func (db *DB) GetItemByID(ctx context.Context, id string) (*Item, error) {
	r, err := db.Queries.GetItemByID(ctx, toUUID(id))
	if err != nil {
		return nil, fmt.Errorf("get item by id: %w", err)
	}

	return &Item{
		ID:              fromUUID(r.ID),
		RawMessageID:    fromUUID(r.RawMessageID),
		RelevanceScore:  r.RelevanceScore,
		ImportanceScore: r.ImportanceScore,
		Topic:           r.Topic.String,
		Summary:         r.Summary.String,
		Language:        r.Language.String,
		Status:          r.Status,
		ErrorJSON:       r.ErrorJson,
		CreatedAt:       r.CreatedAt.Time,
		FirstSeenAt:     r.FirstSeenAt.Time,
	}, nil
}

func (db *DB) SaveEmbedding(ctx context.Context, itemID string, embedding []float32) error {
	if err := db.Queries.SaveEmbedding(ctx, sqlc.SaveEmbeddingParams{
		ItemID:    toUUID(itemID),
		Embedding: pgvector.NewVector(embedding),
	}); err != nil {
		return fmt.Errorf("save embedding: %w", err)
	}

	return nil
}

func (db *DB) SaveItemRating(ctx context.Context, itemID string, userID int64, rating, feedback, source string) error {
	if err := db.Queries.SaveItemRating(ctx, sqlc.SaveItemRatingParams{
		ItemID:   toUUID(itemID),
		UserID:   userID,
		Rating:   rating,
		Feedback: toText(feedback),
		Source:   source,
	}); err != nil {
		return fmt.Errorf(errSaveItemRating, err)
	}

	return nil
}

func (db *DB) GetItemRatingsSince(ctx context.Context, since time.Time) ([]ItemRating, error) {
	rows, err := db.Queries.GetItemRatingsSince(ctx, toTimestamptz(since))
	if err != nil {
		return nil, fmt.Errorf("get item ratings since: %w", err)
	}

	ratings := make([]ItemRating, 0, len(rows))

	for _, row := range rows {
		if !row.CreatedAt.Valid {
			continue
		}

		ratings = append(ratings, ItemRating{
			ChannelID: fromUUID(row.ChannelID),
			Rating:    row.Rating,
			CreatedAt: row.CreatedAt.Time,
		})
	}

	return ratings, nil
}

func (db *DB) GetItemRatingsByItem(ctx context.Context, itemID string, limit int) ([]ItemRatingDetail, error) {
	rows, err := db.Queries.GetItemRatingsByItem(ctx, sqlc.GetItemRatingsByItemParams{
		ItemID: toUUID(itemID),
		Limit:  safeIntToInt32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("get item ratings by item: %w", err)
	}

	results := make([]ItemRatingDetail, 0, len(rows))
	for _, row := range rows {
		if !row.CreatedAt.Valid {
			continue
		}

		results = append(results, ItemRatingDetail{
			UserID:    row.UserID,
			Rating:    row.Rating,
			Feedback:  row.Feedback.String,
			Source:    row.Source,
			CreatedAt: row.CreatedAt.Time,
		})
	}

	return results, nil
}

func (db *DB) GetItemEmbedding(ctx context.Context, itemID string) ([]float32, error) {
	embeddingStr, err := db.Queries.GetItemEmbedding(ctx, toUUID(itemID))
	if err != nil {
		return nil, fmt.Errorf("get item embedding: %w", err)
	}

	var v pgvector.Vector
	if err := v.Parse(embeddingStr); err != nil {
		return nil, fmt.Errorf("parse embedding vector: %w", err)
	}

	return v.Slice(), nil
}
