package db

import (
	"context"
	"fmt"
	"time"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/storage/sqlc"
)

// RawMessage is an alias for the domain type.
type RawMessage = domain.RawMessage

func (db *DB) SaveRawMessage(ctx context.Context, msg *RawMessage) error {
	if err := db.Queries.SaveRawMessage(ctx, sqlc.SaveRawMessageParams{
		ChannelID:     toUUID(msg.ChannelID),
		TgMessageID:   msg.TGMessageID,
		TgDate:        toTimestamptz(msg.TGDate),
		Text:          toText(msg.Text),
		EntitiesJson:  msg.EntitiesJSON,
		MediaJson:     msg.MediaJSON,
		MediaData:     msg.MediaData,
		CanonicalHash: msg.CanonicalHash,
		IsForward:     msg.IsForward,
	}); err != nil {
		return fmt.Errorf("save raw message: %w", err)
	}

	return nil
}

func (db *DB) GetUnprocessedMessages(ctx context.Context, limit int) ([]RawMessage, error) {
	sqlcMessages, err := db.Queries.GetUnprocessedMessages(ctx, safeIntToInt32(limit))
	if err != nil {
		return nil, fmt.Errorf("get unprocessed messages: %w", err)
	}

	messages := make([]RawMessage, len(sqlcMessages))

	for i, m := range sqlcMessages {
		// Default importance weight if not set
		weight := m.ChannelImportanceWeight.Float32
		if !m.ChannelImportanceWeight.Valid || weight == 0 {
			weight = DefaultImportanceWeight
		}

		messages[i] = RawMessage{
			ID:                      fromUUID(m.ID),
			ChannelID:               fromUUID(m.ChannelID),
			ChannelTitle:            m.ChannelTitle.String,
			ChannelContext:          m.ChannelContext.String,
			ChannelDescription:      m.ChannelDescription.String,
			ChannelCategory:         m.ChannelCategory.String,
			ChannelTone:             m.ChannelTone.String,
			ChannelUpdateFreq:       m.ChannelUpdateFreq.String,
			RelevanceThreshold:      m.ChannelRelevanceThreshold.Float32,
			ImportanceThreshold:     m.ChannelImportanceThreshold.Float32,
			ImportanceWeight:        weight,
			AutoRelevanceEnabled:    m.ChannelAutoRelevanceEnabled.Bool,
			RelevanceThresholdDelta: m.ChannelRelevanceThresholdDelta.Float32,
			TGMessageID:             m.TgMessageID,
			TGDate:                  m.TgDate.Time,
			Text:                    m.Text.String,
			EntitiesJSON:            m.EntitiesJson,
			MediaJSON:               m.MediaJson,
			MediaData:               m.MediaData,
			CanonicalHash:           m.CanonicalHash,
			IsForward:               m.IsForward,
		}
	}

	return messages, nil
}

func (db *DB) GetRecentMessagesForChannel(ctx context.Context, channelID string, before time.Time, limit int) ([]string, error) {
	rows, err := db.Queries.GetRecentMessagesForChannel(ctx, sqlc.GetRecentMessagesForChannelParams{
		ChannelID: toUUID(channelID),
		TgDate:    toTimestamptz(before),
		Limit:     safeIntToInt32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("get recent messages for channel: %w", err)
	}

	res := make([]string, len(rows))
	for i, r := range rows {
		res[i] = r.Text.String
	}

	return res, nil
}

func (db *DB) MarkAsProcessed(ctx context.Context, id string) error {
	if err := db.Queries.MarkAsProcessed(ctx, toUUID(id)); err != nil {
		return fmt.Errorf("mark as processed: %w", err)
	}

	return nil
}

func (db *DB) GetBacklogCount(ctx context.Context) (int, error) {
	count, err := db.Queries.GetBacklogCount(ctx)
	if err != nil {
		return 0, fmt.Errorf("get backlog count: %w", err)
	}

	return int(count), nil
}

func (db *DB) CheckStrictDuplicate(ctx context.Context, hash string, id string) (bool, error) {
	isDuplicate, err := db.Queries.CheckStrictDuplicate(ctx, sqlc.CheckStrictDuplicateParams{
		CanonicalHash: hash,
		ID:            toUUID(id),
	})
	if err != nil {
		return false, fmt.Errorf("check strict duplicate: %w", err)
	}

	return isDuplicate, nil
}

// CheckAndMarkDiscoveriesExtracted atomically checks if discoveries have been extracted
// for a message and marks it as extracted. Returns true if this is the first extraction.
func (db *DB) CheckAndMarkDiscoveriesExtracted(ctx context.Context, channelID string, tgMessageID int64) (bool, error) {
	_, err := db.Queries.CheckAndMarkDiscoveriesExtracted(ctx, sqlc.CheckAndMarkDiscoveriesExtractedParams{
		ChannelID:   toUUID(channelID),
		TgMessageID: tgMessageID,
	})
	if err != nil {
		// pgx returns ErrNoRows if no row was updated (already extracted)
		if err.Error() == "no rows in result set" {
			return false, nil
		}

		return false, fmt.Errorf("check and mark discoveries extracted: %w", err)
	}

	return true, nil
}
