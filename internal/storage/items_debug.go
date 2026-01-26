package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ItemSearchResult is a lightweight view used for text search results.
type ItemSearchResult struct {
	ID              string
	Summary         string
	Text            string
	Topic           string
	Status          string
	TGDate          time.Time
	ChannelUsername string
	ChannelTitle    string
	ChannelPeerID   int64
	MessageID       int64
}

// ItemDebugDetail provides extended item context for enrichment debugging.
type ItemDebugDetail struct {
	ID              string
	RawMessageID    string
	Summary         string
	Topic           string
	Language        string
	LanguageSource  string
	Status          string
	RelevanceScore  float32
	ImportanceScore float32
	Text            string
	PreviewText     string
	TGDate          time.Time
	MessageID       int64
	ChannelID       string
	ChannelUsername string
	ChannelTitle    string
	ChannelDesc     string
	ChannelPeerID   int64
}

// SearchItemsByText looks for items with matching summary or raw text.
func (db *DB) SearchItemsByText(ctx context.Context, query string, limit int) ([]ItemSearchResult, error) {
	pattern := "%" + SanitizeUTF8(query) + "%"

	rows, err := db.Pool.Query(ctx, `
		SELECT i.id,
		       i.summary,
		       i.topic,
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
		WHERE i.summary ILIKE $1 OR rm.text ILIKE $1
		ORDER BY rm.tg_date DESC
		LIMIT $2
	`, pattern, safeIntToInt32(limit))
	if err != nil {
		return nil, fmt.Errorf("search items by text: %w", err)
	}
	defer rows.Close()

	results := make([]ItemSearchResult, 0, limit)

	for rows.Next() {
		var (
			itemID  pgtype.UUID
			summary pgtype.Text
			topic   pgtype.Text
			text    pgtype.Text
			user    pgtype.Text
			title   pgtype.Text
		)

		res := ItemSearchResult{}

		if err := rows.Scan(
			&itemID,
			&summary,
			&topic,
			&res.Status,
			&text,
			&res.TGDate,
			&res.MessageID,
			&user,
			&title,
			&res.ChannelPeerID,
		); err != nil {
			return nil, fmt.Errorf("scan item search row: %w", err)
		}

		res.ID = fromUUID(itemID)
		res.Summary = summary.String
		res.Topic = topic.String
		res.Text = text.String
		res.ChannelUsername = user.String
		res.ChannelTitle = title.String

		results = append(results, res)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate item search rows: %w", err)
	}

	return results, nil
}

// GetItemDebugDetail returns a single item with channel metadata for debug output.
func (db *DB) GetItemDebugDetail(ctx context.Context, id string) (*ItemDebugDetail, error) {
	row := db.Pool.QueryRow(ctx, `
		SELECT i.id,
		       i.raw_message_id,
		       i.summary,
		       i.topic,
		       i.language,
		       i.language_source,
		       i.status,
		       i.relevance_score,
		       i.importance_score,
		       rm.text,
		       rm.preview_text,
		       rm.tg_date,
		       rm.tg_message_id,
		       c.id,
		       c.username,
		       c.title,
		       c.description,
		       c.tg_peer_id
		FROM items i
		JOIN raw_messages rm ON i.raw_message_id = rm.id
		JOIN channels c ON rm.channel_id = c.id
		WHERE i.id = $1
	`, toUUID(id))

	var (
		itemID       pgtype.UUID
		rawMessageID pgtype.UUID
		summary      pgtype.Text
		topic        pgtype.Text
		language     pgtype.Text
		langSource   pgtype.Text
		text         pgtype.Text
		previewText  pgtype.Text
		channelID    pgtype.UUID
		user         pgtype.Text
		title        pgtype.Text
		desc         pgtype.Text
	)

	item := ItemDebugDetail{}

	if err := row.Scan(
		&itemID,
		&rawMessageID,
		&summary,
		&topic,
		&language,
		&langSource,
		&item.Status,
		&item.RelevanceScore,
		&item.ImportanceScore,
		&text,
		&previewText,
		&item.TGDate,
		&item.MessageID,
		&channelID,
		&user,
		&title,
		&desc,
		&item.ChannelPeerID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil //nolint:nilnil // not found
		}

		return nil, fmt.Errorf("get item debug detail: %w", err)
	}

	item.ID = fromUUID(itemID)
	item.RawMessageID = fromUUID(rawMessageID)
	item.Summary = summary.String
	item.Topic = topic.String
	item.Language = language.String
	item.LanguageSource = langSource.String
	item.Text = text.String
	item.PreviewText = previewText.String
	item.ChannelID = fromUUID(channelID)
	item.ChannelUsername = user.String
	item.ChannelTitle = title.String
	item.ChannelDesc = desc.String

	return &item, nil
}
