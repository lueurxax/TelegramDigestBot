package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/lueurxax/telegram-digest-bot/internal/db/sqlc"
	"github.com/pgvector/pgvector-go"
)

type Item struct {
	ID                 string
	RawMessageID       string
	RelevanceScore     float32
	ImportanceScore    float32
	Topic              string
	Summary            string
	Language           string
	Status             string
	ErrorJSON          []byte
	CreatedAt          time.Time
	TGDate             time.Time
	SourceChannel      string
	SourceChannelTitle string
	SourceChannelID    int64
	SourceMsgID        int64
	Embedding          []float32
}

type ItemRating struct {
	ChannelID string
	Rating    string
	CreatedAt time.Time
}

func (db *DB) SaveItem(ctx context.Context, item *Item) error {
	id, err := db.Queries.SaveItem(ctx, sqlc.SaveItemParams{
		RawMessageID:    toUUID(item.RawMessageID),
		RelevanceScore:  item.RelevanceScore,
		ImportanceScore: item.ImportanceScore,
		Topic:           toText(item.Topic),
		Summary:         toText(item.Summary),
		Language:        toText(item.Language),
		Status:          item.Status,
	})
	if err != nil {
		return err
	}

	item.ID = fromUUID(id)

	return nil
}

func (db *DB) SaveItemError(ctx context.Context, rawMsgID string, errJSON []byte) error {
	return db.Queries.SaveItemError(ctx, sqlc.SaveItemErrorParams{
		RawMessageID: toUUID(rawMsgID),
		ErrorJson:    errJSON,
	})
}

func (db *DB) FindSimilarItem(ctx context.Context, embedding []float32, threshold float32) (string, error) {
	id, err := db.Queries.FindSimilarItem(ctx, sqlc.FindSimilarItemParams{
		Embedding:    pgvector.NewVector(embedding),
		Threshold:    float64(1.0 - threshold),
		MinCreatedAt: toTimestamptz(time.Now().Add(-7 * 24 * time.Hour)),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}

		return "", err
	}

	return fromUUID(id), nil
}

func (db *DB) MarkItemsAsDigested(ctx context.Context, ids []string) error {
	uuids := make([]pgtype.UUID, len(ids))
	for i, id := range ids {
		uuids[i] = toUUID(id)
	}

	return db.Queries.MarkItemsAsDigested(ctx, uuids)
}

func (db *DB) GetRecentErrors(ctx context.Context, limit int) ([]Item, error) {
	rows, err := db.Queries.GetRecentErrors(ctx, safeIntToInt32(limit))
	if err != nil {
		return nil, err
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
	return db.Queries.RetryFailedItems(ctx)
}

func (db *DB) RetryItem(ctx context.Context, id string) error {
	return db.Queries.RetryItem(ctx, toUUID(id))
}

func (db *DB) GetItemByID(ctx context.Context, id string) (*Item, error) {
	r, err := db.Queries.GetItemByID(ctx, toUUID(id))
	if err != nil {
		return nil, err
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
	}, nil
}

func (db *DB) SaveEmbedding(ctx context.Context, itemID string, embedding []float32) error {
	return db.Queries.SaveEmbedding(ctx, sqlc.SaveEmbeddingParams{
		ItemID:    toUUID(itemID),
		Embedding: pgvector.NewVector(embedding),
	})
}

func (db *DB) SaveItemRating(ctx context.Context, itemID string, userID int64, rating, feedback string) error {
	return db.Queries.SaveItemRating(ctx, sqlc.SaveItemRatingParams{
		ItemID:   toUUID(itemID),
		UserID:   userID,
		Rating:   rating,
		Feedback: toText(feedback),
	})
}

func (db *DB) GetItemRatingsSince(ctx context.Context, since time.Time) ([]ItemRating, error) {
	rows, err := db.Queries.GetItemRatingsSince(ctx, toTimestamptz(since))
	if err != nil {
		return nil, err
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

func (db *DB) GetItemEmbedding(ctx context.Context, itemID string) ([]float32, error) {
	embeddingStr, err := db.Queries.GetItemEmbedding(ctx, toUUID(itemID))
	if err != nil {
		return nil, err
	}

	var v pgvector.Vector
	if err := v.Parse(embeddingStr); err != nil {
		return nil, err
	}

	return v.Slice(), nil
}
