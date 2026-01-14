package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/lueurxax/telegram-digest-bot/internal/storage/sqlc"
)

const (
	AnnotationStatusPending  = "pending"
	AnnotationStatusAssigned = "assigned"
	AnnotationStatusLabeled  = "labeled"
	AnnotationStatusSkipped  = "skipped"
)

type AnnotationItem struct {
	ItemID          string
	Summary         string
	Text            string
	Topic           string
	Status          string
	RelevanceScore  float32
	ImportanceScore float32
	TGDate          time.Time
	ChannelUsername string
	ChannelTitle    string
	ChannelPeerID   int64
	MessageID       int64
}

type AnnotationExport struct {
	ID              string
	Label           string
	RelevanceScore  float32
	ImportanceScore float32
}

func (db *DB) EnqueueAnnotationItems(ctx context.Context, since time.Time, limit int) (int, error) {
	tag, err := db.Pool.Exec(ctx, `
		INSERT INTO annotation_queue (item_id)
		SELECT i.id
		FROM items i
		JOIN raw_messages rm ON i.raw_message_id = rm.id
		WHERE rm.tg_date >= $1
		  AND i.status IN ('ready', 'rejected')
		  AND rm.text IS NOT NULL
		  AND rm.text <> ''
		ORDER BY rm.tg_date DESC
		LIMIT $2
		ON CONFLICT (item_id) DO NOTHING
	`, since, limit)
	if err != nil {
		return 0, fmt.Errorf("enqueue annotation items: %w", err)
	}

	return int(tag.RowsAffected()), nil
}

func (db *DB) AssignNextAnnotation(ctx context.Context, userID int64) (*AnnotationItem, error) {
	item, err := db.GetAssignedAnnotation(ctx, userID)
	if err != nil || item != nil {
		return item, err
	}

	var itemID pgtype.UUID

	err = db.Pool.QueryRow(ctx, `
		WITH picked AS (
			SELECT id
			FROM annotation_queue
			WHERE status = $1
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE annotation_queue aq
		SET status = $2,
			assigned_to = $3,
			assigned_at = NOW(),
			updated_at = NOW()
		FROM picked
		WHERE aq.id = picked.id
		RETURNING aq.item_id
	`, AnnotationStatusPending, AnnotationStatusAssigned, userID).Scan(&itemID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil //nolint:nilnil // nil,nil indicates no pending annotation available
		}

		return nil, fmt.Errorf("assign next annotation: %w", err)
	}

	return db.getAnnotationItemByID(ctx, fromUUID(itemID))
}

func (db *DB) GetAssignedAnnotation(ctx context.Context, userID int64) (*AnnotationItem, error) {
	var itemID pgtype.UUID

	err := db.Pool.QueryRow(ctx, `
		SELECT item_id
		FROM annotation_queue
		WHERE assigned_to = $1
		  AND status = $2
		ORDER BY assigned_at DESC
		LIMIT 1
	`, userID, AnnotationStatusAssigned).Scan(&itemID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil //nolint:nilnil // nil,nil indicates no assigned annotation found
		}

		return nil, fmt.Errorf("get assigned annotation: %w", err)
	}

	return db.getAnnotationItemByID(ctx, fromUUID(itemID))
}

func (db *DB) LabelAssignedAnnotation(ctx context.Context, userID int64, label, comment string) (*AnnotationItem, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback(ctx) //nolint:errcheck // rollback after commit returns error, this is best-effort cleanup
	}()

	var itemID pgtype.UUID

	err = tx.QueryRow(ctx, `
		UPDATE annotation_queue
		SET status = $2,
			label = $3,
			comment = $4,
			updated_at = NOW()
		WHERE assigned_to = $1
		  AND status = $5
		RETURNING item_id
	`, userID, AnnotationStatusLabeled, label, toText(comment), AnnotationStatusAssigned).Scan(&itemID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil //nolint:nilnil // nil,nil indicates no annotation to label
		}

		return nil, fmt.Errorf("update annotation queue: %w", err)
	}

	err = db.Queries.WithTx(tx).SaveItemRating(ctx, sqlc.SaveItemRatingParams{
		ItemID:   itemID,
		UserID:   userID,
		Rating:   label,
		Feedback: toText(comment),
	})
	if err != nil {
		return nil, fmt.Errorf(errSaveItemRating, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return db.getAnnotationItemByID(ctx, fromUUID(itemID))
}

func (db *DB) SkipAssignedAnnotation(ctx context.Context, userID int64) (*AnnotationItem, error) {
	var itemID pgtype.UUID

	err := db.Pool.QueryRow(ctx, `
		UPDATE annotation_queue
		SET status = $2,
			updated_at = NOW()
		WHERE assigned_to = $1
		  AND status = $3
		RETURNING item_id
	`, userID, AnnotationStatusSkipped, AnnotationStatusAssigned).Scan(&itemID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil //nolint:nilnil // nil,nil indicates no annotation to skip
		}

		return nil, fmt.Errorf("skip assigned annotation: %w", err)
	}

	return db.getAnnotationItemByID(ctx, fromUUID(itemID))
}

func (db *DB) GetAnnotationStats(ctx context.Context) (map[string]int, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT status, COUNT(*)::int
		FROM annotation_queue
		GROUP BY status
	`)
	if err != nil {
		return nil, fmt.Errorf("query annotation stats: %w", err)
	}
	defer rows.Close()

	stats := make(map[string]int)

	for rows.Next() {
		var (
			status string
			count  int
		)

		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("scan annotation stat row: %w", err)
		}

		stats[status] = count
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate annotation stats rows: %w", err)
	}

	return stats, nil
}

func (db *DB) GetLabeledAnnotations(ctx context.Context, limit int) ([]AnnotationExport, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT i.id, aq.label, i.relevance_score, i.importance_score
		FROM annotation_queue aq
		JOIN items i ON aq.item_id = i.id
		WHERE aq.status = $1
		  AND aq.label IS NOT NULL
		ORDER BY aq.updated_at DESC
		LIMIT $2
	`, AnnotationStatusLabeled, limit)
	if err != nil {
		return nil, fmt.Errorf("query labeled annotations: %w", err)
	}
	defer rows.Close()

	records := make([]AnnotationExport, 0, limit)

	for rows.Next() {
		var (
			itemID pgtype.UUID
			label  pgtype.Text
		)

		var rec AnnotationExport

		if err := rows.Scan(&itemID, &label, &rec.RelevanceScore, &rec.ImportanceScore); err != nil {
			return nil, fmt.Errorf("scan labeled annotation row: %w", err)
		}

		rec.ID = fromUUID(itemID)
		rec.Label = label.String

		records = append(records, rec)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate labeled annotations rows: %w", err)
	}

	return records, nil
}

func (db *DB) getAnnotationItemByID(ctx context.Context, id string) (*AnnotationItem, error) {
	row := db.Pool.QueryRow(ctx, `
		SELECT i.id,
		       i.summary,
		       i.topic,
		       i.relevance_score,
		       i.importance_score,
		       i.status,
		       rm.text,
		       rm.tg_date,
		       rm.tg_message_id,
		       c.username,
		       c.title,
		       c.tg_peer_id
		FROM items i
		JOIN raw_messages rm ON i.raw_message_id = rm.id
		JOIN channels c ON rm.channel_id = c.id
		WHERE i.id = $1
	`, toUUID(id))

	var (
		itemID  pgtype.UUID
		summary pgtype.Text
		topic   pgtype.Text
		text    pgtype.Text
		user    pgtype.Text
		title   pgtype.Text
	)

	item := AnnotationItem{}

	if err := row.Scan(
		&itemID,
		&summary,
		&topic,
		&item.RelevanceScore,
		&item.ImportanceScore,
		&item.Status,
		&text,
		&item.TGDate,
		&item.MessageID,
		&user,
		&title,
		&item.ChannelPeerID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil //nolint:nilnil // nil,nil indicates annotation item not found
		}

		return nil, fmt.Errorf("get annotation item by id: %w", err)
	}

	item.ItemID = fromUUID(itemID)
	item.Summary = summary.String
	item.Topic = topic.String
	item.Text = text.String
	item.ChannelUsername = user.String
	item.ChannelTitle = title.String

	return &item, nil
}
