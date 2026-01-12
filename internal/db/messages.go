package db

import (
	"context"
	"time"

	"github.com/lueurxax/telegram-digest-bot/internal/db/sqlc"
)

type RawMessage struct {
	ID                      string
	ChannelID               string
	ChannelTitle            string
	ChannelContext          string
	ChannelDescription      string
	ChannelCategory         string
	ChannelTone             string
	ChannelUpdateFreq       string
	RelevanceThreshold      float32
	ImportanceThreshold     float32
	ImportanceWeight        float32
	AutoRelevanceEnabled    bool
	RelevanceThresholdDelta float32
	TGMessageID             int64
	TGDate                  time.Time
	Text                    string
	EntitiesJSON            []byte
	MediaJSON               []byte
	MediaData               []byte
	CanonicalHash           string
	IsForward               bool
}

func (db *DB) SaveRawMessage(ctx context.Context, msg *RawMessage) error {
	return db.Queries.SaveRawMessage(ctx, sqlc.SaveRawMessageParams{
		ChannelID:     toUUID(msg.ChannelID),
		TgMessageID:   msg.TGMessageID,
		TgDate:        toTimestamptz(msg.TGDate),
		Text:          toText(msg.Text),
		EntitiesJson:  msg.EntitiesJSON,
		MediaJson:     msg.MediaJSON,
		MediaData:     msg.MediaData,
		CanonicalHash: msg.CanonicalHash,
		IsForward:     msg.IsForward,
	})
}

func (db *DB) GetUnprocessedMessages(ctx context.Context, limit int) ([]RawMessage, error) {
	sqlcMessages, err := db.Queries.GetUnprocessedMessages(ctx, int32(limit))
	if err != nil {
		return nil, err
	}

	messages := make([]RawMessage, len(sqlcMessages))

	for i, m := range sqlcMessages {
		// Default importance weight to 1.0 if not set
		weight := m.ChannelImportanceWeight.Float32
		if !m.ChannelImportanceWeight.Valid || weight == 0 {
			weight = 1.0
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
		Limit:     int32(limit),
	})
	if err != nil {
		return nil, err
	}

	res := make([]string, len(rows))
	for i, r := range rows {
		res[i] = r.Text.String
	}

	return res, nil
}

func (db *DB) MarkAsProcessed(ctx context.Context, id string) error {
	return db.Queries.MarkAsProcessed(ctx, toUUID(id))
}

func (db *DB) GetBacklogCount(ctx context.Context) (int, error) {
	count, err := db.Queries.GetBacklogCount(ctx)
	return int(count), err
}

func (db *DB) CheckStrictDuplicate(ctx context.Context, hash string, id string) (bool, error) {
	return db.Queries.CheckStrictDuplicate(ctx, sqlc.CheckStrictDuplicateParams{
		CanonicalHash: hash,
		ID:            toUUID(id),
	})
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

		return false, err
	}

	return true, nil
}
