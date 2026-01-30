package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pgvector/pgvector-go"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/storage/sqlc"
)

// Conversion constants for time intervals.
const microsecondsPerHour = 3600 * 1000000 // 1 hour in microseconds

// Bullet is an alias for the domain type.
type Bullet = domain.Bullet

// BulletForDigest contains bullet data with source channel info for digest rendering.
type BulletForDigest struct {
	ID                 string
	ItemID             string
	BulletIndex        int
	Text               string
	Topic              string
	RelevanceScore     float32
	ImportanceScore    float32
	Status             string
	CreatedAt          time.Time
	SourceChannel      string
	SourceChannelTitle string
	TGDate             time.Time
	SourceCount        int // Number of corroborating sources (items with similar bullets)
}

// InsertBullet saves a new bullet to the database.
func (db *DB) InsertBullet(ctx context.Context, bullet *Bullet) error {
	id, err := db.Queries.InsertBullet(ctx, sqlc.InsertBulletParams{
		ItemID:          toUUID(bullet.ItemID),
		BulletIndex:     safeIntToInt32(bullet.BulletIndex),
		Text:            bullet.Text,
		Topic:           toText(bullet.Topic),
		RelevanceScore:  toFloat4(bullet.RelevanceScore),
		ImportanceScore: toFloat4(bullet.ImportanceScore),
		BulletHash:      toText(bullet.BulletHash),
		Status:          toText(bullet.Status),
	})
	if err != nil {
		return fmt.Errorf("insert bullet: %w", err)
	}

	bullet.ID = fromUUID(id)

	return nil
}

// UpdateBulletEmbedding updates the embedding for a bullet.
func (db *DB) UpdateBulletEmbedding(ctx context.Context, bulletID string, embedding []float32) error {
	if err := db.Queries.UpdateBulletEmbedding(ctx, sqlc.UpdateBulletEmbeddingParams{
		ID:        toUUID(bulletID),
		Embedding: pgvector.NewVector(embedding),
	}); err != nil {
		return fmt.Errorf("update bullet embedding: %w", err)
	}

	return nil
}

// GetBulletsForItem retrieves all bullets for a specific item.
func (db *DB) GetBulletsForItem(ctx context.Context, itemID string) ([]Bullet, error) {
	rows, err := db.Queries.GetBulletsForItem(ctx, toUUID(itemID))
	if err != nil {
		return nil, fmt.Errorf("get bullets for item: %w", err)
	}

	bullets := make([]Bullet, len(rows))
	for i, row := range rows {
		bullets[i] = convertItemBulletToDomain(row)
	}

	return bullets, nil
}

// GetBulletsForItems retrieves all ready bullets for multiple items.
func (db *DB) GetBulletsForItems(ctx context.Context, itemIDs []string) ([]Bullet, error) {
	uuids := make([]pgtype.UUID, len(itemIDs))
	for i, id := range itemIDs {
		uuids[i] = toUUID(id)
	}

	rows, err := db.Queries.GetBulletsForItems(ctx, uuids)
	if err != nil {
		return nil, fmt.Errorf("get bullets for items: %w", err)
	}

	bullets := make([]Bullet, len(rows))
	for i, row := range rows {
		bullets[i] = convertItemBulletToDomain(row)
	}

	return bullets, nil
}

// UpdateBulletStatus updates the status of a bullet.
func (db *DB) UpdateBulletStatus(ctx context.Context, bulletID, status string) error {
	if err := db.Queries.UpdateBulletStatus(ctx, sqlc.UpdateBulletStatusParams{
		ID:     toUUID(bulletID),
		Status: toText(status),
	}); err != nil {
		return fmt.Errorf("update bullet status: %w", err)
	}

	return nil
}

// GetBulletsForDigest retrieves bullets with source channel info for digest rendering.
func (db *DB) GetBulletsForDigest(ctx context.Context, itemIDs []string) ([]BulletForDigest, error) {
	uuids := make([]pgtype.UUID, len(itemIDs))
	for i, id := range itemIDs {
		uuids[i] = toUUID(id)
	}

	rows, err := db.Queries.GetBulletsForDigest(ctx, uuids)
	if err != nil {
		return nil, fmt.Errorf("get bullets for digest: %w", err)
	}

	bullets := make([]BulletForDigest, len(rows))
	for i, row := range rows {
		bullets[i] = BulletForDigest{
			ID:                 fromUUID(row.ID),
			ItemID:             fromUUID(row.ItemID),
			BulletIndex:        int(row.BulletIndex),
			Text:               row.Text,
			Topic:              fromText(row.Topic),
			RelevanceScore:     fromFloat4(row.RelevanceScore),
			ImportanceScore:    fromFloat4(row.ImportanceScore),
			Status:             row.Status, // Already a string from SQL literal
			CreatedAt:          fromTimestamptz(row.CreatedAt),
			SourceChannel:      fromText(row.SourceChannel),
			SourceChannelTitle: fromText(row.SourceChannelTitle),
			TGDate:             fromTimestamptz(row.TgDate),
			SourceCount:        int(row.SourceCount),
		}
	}

	return bullets, nil
}

// MarkDuplicateBullets marks multiple bullets as duplicates.
func (db *DB) MarkDuplicateBullets(ctx context.Context, bulletIDs []string) error {
	uuids := make([]pgtype.UUID, len(bulletIDs))
	for i, id := range bulletIDs {
		uuids[i] = toUUID(id)
	}

	if err := db.Queries.MarkDuplicateBullets(ctx, uuids); err != nil {
		return fmt.Errorf("mark duplicate bullets: %w", err)
	}

	return nil
}

// MarkBulletAsDuplicateOf marks a bullet as duplicate and links it to its canonical bullet.
func (db *DB) MarkBulletAsDuplicateOf(ctx context.Context, bulletID, canonicalID string) error {
	if err := db.Queries.MarkBulletAsDuplicateOf(ctx, sqlc.MarkBulletAsDuplicateOfParams{
		ID:              toUUID(bulletID),
		BulletClusterID: toUUID(canonicalID),
	}); err != nil {
		return fmt.Errorf("mark bullet as duplicate of: %w", err)
	}

	return nil
}

// MarkBulletAsCanonical marks a bullet as ready and sets its cluster ID to itself.
func (db *DB) MarkBulletAsCanonical(ctx context.Context, bulletID string) error {
	if err := db.Queries.MarkBulletAsCanonical(ctx, toUUID(bulletID)); err != nil {
		return fmt.Errorf("mark bullet as canonical: %w", err)
	}

	return nil
}

// PendingBulletForDedup contains bullet data needed for deduplication.
type PendingBulletForDedup struct {
	ID              string
	Text            string
	Embedding       []float32
	ItemID          string
	ImportanceScore float32
	Status          string // "pending" or "ready" (for global dedup pool)
}

// GetPendingBulletsForDedup retrieves pending bullets plus recent ready bullets for deduplication.
// lookbackHours specifies how far back to include ready bullets for global dedup.
func (db *DB) GetPendingBulletsForDedup(ctx context.Context, lookbackHours int) ([]PendingBulletForDedup, error) {
	interval := pgtype.Interval{
		Microseconds: int64(lookbackHours) * microsecondsPerHour,
		Valid:        true,
	}

	rows, err := db.Queries.GetPendingBulletsForDedup(ctx, interval)
	if err != nil {
		return nil, fmt.Errorf("get pending bullets for dedup: %w", err)
	}

	bullets := make([]PendingBulletForDedup, len(rows))
	for i, row := range rows {
		bullets[i] = PendingBulletForDedup{
			ID:              fromUUID(row.ID),
			Text:            row.Text,
			Embedding:       row.Embedding.Slice(),
			ItemID:          fromUUID(row.ItemID),
			ImportanceScore: fromFloat4(row.ImportanceScore),
			Status:          fromText(row.Status),
		}
	}

	return bullets, nil
}

// convertItemBulletToDomain converts a sqlc ItemBullet to a domain Bullet.
func convertItemBulletToDomain(row sqlc.ItemBullet) Bullet {
	return Bullet{
		ID:              fromUUID(row.ID),
		ItemID:          fromUUID(row.ItemID),
		BulletIndex:     int(row.BulletIndex),
		Text:            row.Text,
		Topic:           fromText(row.Topic),
		RelevanceScore:  fromFloat4(row.RelevanceScore),
		ImportanceScore: fromFloat4(row.ImportanceScore),
		Embedding:       row.Embedding.Slice(),
		BulletHash:      fromText(row.BulletHash),
		Status:          fromText(row.Status),
		CreatedAt:       fromTimestamptz(row.CreatedAt),
	}
}
