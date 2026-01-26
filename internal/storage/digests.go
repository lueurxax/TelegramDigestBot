package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/lueurxax/telegram-digest-bot/internal/storage/sqlc"
)

type DigestEntry struct {
	Title   string
	Body    string
	Sources []DigestSource
}

type DigestSource struct {
	Channel string `json:"channel"`
	MsgID   int64  `json:"msg_id"`
}

func (db *DB) DigestExists(ctx context.Context, start, end time.Time) (bool, error) {
	exists, err := db.Queries.DigestExists(ctx, sqlc.DigestExistsParams{
		WindowStart: toTimestamptz(start),
		WindowEnd:   toTimestamptz(end),
	})
	if err != nil {
		return false, fmt.Errorf("check digest exists: %w", err)
	}

	return exists, nil
}

func (db *DB) GetItemsForWindow(ctx context.Context, start, end time.Time, importanceThreshold float32, limit int) ([]Item, error) {
	sqlcItems, err := db.Queries.GetItemsForWindow(ctx, sqlc.GetItemsForWindowParams{
		TgDate:          toTimestamptz(start),
		TgDate_2:        toTimestamptz(end),
		ImportanceScore: importanceThreshold,
		Limit:           safeIntToInt32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("get items for window: %w", err)
	}

	items := make([]Item, len(sqlcItems))
	for i, item := range sqlcItems {
		items[i] = Item{
			ID:                 fromUUID(item.ID),
			RawMessageID:       fromUUID(item.RawMessageID),
			RelevanceScore:     item.RelevanceScore,
			ImportanceScore:    item.ImportanceScore,
			Topic:              item.Topic.String,
			Summary:            item.Summary.String,
			Language:           item.Language.String,
			Status:             item.Status,
			FirstSeenAt:        item.FirstSeenAt.Time,
			TGDate:             item.TgDate.Time,
			SourceChannel:      item.SourceChannel.String,
			SourceChannelTitle: item.SourceChannelTitle.String,
			SourceChannelID:    item.SourceChannelID,
			SourceMsgID:        item.SourceMsgID,
			Embedding:          item.Embedding.Slice(),
		}
	}

	return items, nil
}

// GetItemsForWindowWithMedia returns items including media data for inline image display.
func (db *DB) GetItemsForWindowWithMedia(ctx context.Context, start, end time.Time, importanceThreshold float32, limit int) ([]ItemWithMedia, error) {
	sqlcItems, err := db.Queries.GetItemsForWindowWithMedia(ctx, sqlc.GetItemsForWindowWithMediaParams{
		TgDate:          toTimestamptz(start),
		TgDate_2:        toTimestamptz(end),
		ImportanceScore: importanceThreshold,
		Limit:           safeIntToInt32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("get items for window with media: %w", err)
	}

	items := make([]ItemWithMedia, len(sqlcItems))
	for i, item := range sqlcItems {
		items[i] = ItemWithMedia{
			Item: Item{
				ID:                 fromUUID(item.ID),
				RawMessageID:       fromUUID(item.RawMessageID),
				RelevanceScore:     item.RelevanceScore,
				ImportanceScore:    item.ImportanceScore,
				Topic:              item.Topic.String,
				Summary:            item.Summary.String,
				Language:           item.Language.String,
				Status:             item.Status,
				FirstSeenAt:        item.FirstSeenAt.Time,
				TGDate:             item.TgDate.Time,
				SourceChannel:      item.SourceChannel.String,
				SourceChannelTitle: item.SourceChannelTitle.String,
				SourceChannelID:    item.SourceChannelID,
				SourceMsgID:        item.SourceMsgID,
				Embedding:          item.Embedding.Slice(),
			},
			MediaData: item.MediaData,
		}
	}

	return items, nil
}

func (db *DB) CountItemsInWindow(ctx context.Context, start, end time.Time) (int, error) {
	count, err := db.Queries.CountItemsInWindow(ctx, sqlc.CountItemsInWindowParams{
		TgDate:   toTimestamptz(start),
		TgDate_2: toTimestamptz(end),
	})
	if err != nil {
		return 0, fmt.Errorf("count items in window: %w", err)
	}

	return int(count), nil
}

func (db *DB) CountReadyItemsInWindow(ctx context.Context, start, end time.Time) (int, error) {
	count, err := db.Queries.CountReadyItemsInWindow(ctx, sqlc.CountReadyItemsInWindowParams{
		TgDate:   toTimestamptz(start),
		TgDate_2: toTimestamptz(end),
	})
	if err != nil {
		return 0, fmt.Errorf("count ready items in window: %w", err)
	}

	return int(count), nil
}

func (db *DB) SaveDigest(ctx context.Context, id string, start, end time.Time, chatID int64, msgID int64) (string, error) {
	newID, err := db.Queries.SaveDigest(ctx, sqlc.SaveDigestParams{
		ID:           toUUID(id),
		WindowStart:  toTimestamptz(start),
		WindowEnd:    toTimestamptz(end),
		PostedChatID: pgtype.Int8{Int64: chatID, Valid: true},
		PostedMsgID:  pgtype.Int8{Int64: msgID, Valid: true},
	})
	if err != nil {
		return "", fmt.Errorf("save digest: %w", err)
	}

	return fromUUID(newID), nil
}

func (db *DB) SaveDigestEntries(ctx context.Context, digestID string, entries []DigestEntry) error {
	for _, e := range entries {
		sourcesJSON, _ := json.Marshal(e.Sources)

		err := db.Queries.SaveDigestEntry(ctx, sqlc.SaveDigestEntryParams{
			DigestID:    toUUID(digestID),
			Title:       toText(e.Title),
			Body:        SanitizeUTF8(e.Body),
			SourcesJson: sourcesJSON,
		})
		if err != nil {
			return fmt.Errorf("save digest entry: %w", err)
		}
	}

	return nil
}

func (db *DB) SaveDigestError(ctx context.Context, start, end time.Time, chatID int64, err error) error {
	errJSON, _ := json.Marshal(map[string]string{"error": err.Error()})

	if err := db.Queries.SaveDigestError(ctx, sqlc.SaveDigestErrorParams{
		WindowStart:  toTimestamptz(start),
		WindowEnd:    toTimestamptz(end),
		PostedChatID: pgtype.Int8{Int64: chatID, Valid: true},
		ErrorJson:    errJSON,
	}); err != nil {
		return fmt.Errorf("save digest error: %w", err)
	}

	return nil
}

func (db *DB) ClearDigestErrors(ctx context.Context) error {
	if err := db.Queries.ClearDigestErrors(ctx); err != nil {
		return fmt.Errorf("clear digest errors: %w", err)
	}

	return nil
}

func (db *DB) SaveRating(ctx context.Context, digestID string, userID int64, rating int16, feedback string) error {
	if err := db.Queries.SaveRating(ctx, sqlc.SaveRatingParams{
		DigestID: toUUID(digestID),
		UserID:   userID,
		Rating:   rating,
		Feedback: toText(feedback),
	}); err != nil {
		return fmt.Errorf("save rating: %w", err)
	}

	return nil
}

func (db *DB) GetDigestCoverImage(ctx context.Context, start, end time.Time, importanceThreshold float32) ([]byte, error) {
	img, err := db.Queries.GetDigestCoverImage(ctx, sqlc.GetDigestCoverImageParams{
		TgDate:          toTimestamptz(start),
		TgDate_2:        toTimestamptz(end),
		ImportanceScore: importanceThreshold,
	})
	if err != nil {
		return nil, fmt.Errorf("get digest cover image: %w", err)
	}

	return img, nil
}
