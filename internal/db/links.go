package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/lueurxax/telegram-digest-bot/internal/db/sqlc"
)

type ResolvedLink struct {
	ID              string
	URL             string
	Domain          string
	LinkType        string
	Title           string
	Content         string
	Author          string
	PublishedAt     time.Time
	Description     string
	ImageURL        string
	WordCount       int
	ChannelUsername string
	ChannelTitle    string
	ChannelID       int64
	MessageID       int64
	Views           int
	Forwards        int
	HasMedia        bool
	MediaType       string
	Status          string
	ErrorMessage    string
	Language        string
	ResolvedAt      time.Time
	CreatedAt       time.Time
	ExpiresAt       time.Time
}

func (db *DB) GetLinkCache(ctx context.Context, url string) (*ResolvedLink, error) {
	c, err := db.Queries.GetLinkCache(ctx, url)
	if err != nil {
		return nil, err
	}
	return &ResolvedLink{
		ID:              fromUUID(c.ID),
		URL:             c.Url,
		Domain:          c.Domain,
		LinkType:        c.LinkType,
		Title:           c.Title.String,
		Content:         c.Content.String,
		Author:          c.Author.String,
		PublishedAt:     c.PublishedAt.Time,
		Description:     c.Description.String,
		ImageURL:        c.ImageUrl.String,
		WordCount:       int(c.WordCount.Int32),
		ChannelUsername: c.ChannelUsername.String,
		ChannelTitle:    c.ChannelTitle.String,
		ChannelID:       c.ChannelID.Int64,
		MessageID:       c.MessageID.Int64,
		Views:           int(c.Views.Int32),
		Forwards:        int(c.Forwards.Int32),
		HasMedia:        c.HasMedia.Bool,
		MediaType:       c.MediaType.String,
		Status:          c.Status,
		ErrorMessage:    c.ErrorMessage.String,
		Language:        c.Language.String,
		ResolvedAt:      c.ResolvedAt.Time,
		CreatedAt:       c.CreatedAt.Time,
		ExpiresAt:       c.ExpiresAt.Time,
	}, nil
}

func (db *DB) SaveLinkCache(ctx context.Context, link *ResolvedLink) (string, error) {
	id, err := db.Queries.SaveLinkCache(ctx, sqlc.SaveLinkCacheParams{
		Url:             link.URL,
		Domain:          link.Domain,
		LinkType:        link.LinkType,
		Title:           toText(link.Title),
		Content:         toText(link.Content),
		Author:          toText(link.Author),
		PublishedAt:     toTimestamptz(link.PublishedAt),
		Description:     toText(link.Description),
		ImageUrl:        toText(link.ImageURL),
		WordCount:       pgtype.Int4{Int32: int32(link.WordCount), Valid: link.WordCount != 0},
		ChannelUsername: toText(link.ChannelUsername),
		ChannelTitle:    toText(link.ChannelTitle),
		ChannelID:       toInt8(link.ChannelID),
		MessageID:       toInt8(link.MessageID),
		Views:           pgtype.Int4{Int32: int32(link.Views), Valid: link.Views != 0},
		Forwards:        pgtype.Int4{Int32: int32(link.Forwards), Valid: link.Forwards != 0},
		HasMedia:        pgtype.Bool{Bool: link.HasMedia, Valid: true},
		MediaType:       toText(link.MediaType),
		Status:          link.Status,
		ErrorMessage:    toText(link.ErrorMessage),
		Language:        toText(link.Language),
		ResolvedAt:      toTimestamptz(link.ResolvedAt),
		ExpiresAt:       toTimestamptz(link.ExpiresAt),
	})
	if err != nil {
		return "", err
	}
	return fromUUID(id), nil
}

func (db *DB) LinkMessageToLink(ctx context.Context, rawMsgID, linkCacheID string, position int) error {
	return db.Queries.LinkMessageToLink(ctx, sqlc.LinkMessageToLinkParams{
		RawMessageID: toUUID(rawMsgID),
		LinkCacheID:  toUUID(linkCacheID),
		Position:     pgtype.Int4{Int32: int32(position), Valid: true},
	})
}

func (db *DB) GetLinksForMessage(ctx context.Context, rawMsgID string) ([]ResolvedLink, error) {
	sqlcLinks, err := db.Queries.GetLinksForMessage(ctx, toUUID(rawMsgID))
	if err != nil {
		return nil, err
	}

	links := make([]ResolvedLink, len(sqlcLinks))
	for i, c := range sqlcLinks {
		links[i] = ResolvedLink{
			ID:              fromUUID(c.ID),
			URL:             c.Url,
			Domain:          c.Domain,
			LinkType:        c.LinkType,
			Title:           c.Title.String,
			Content:         c.Content.String,
			Author:          c.Author.String,
			PublishedAt:     c.PublishedAt.Time,
			Description:     c.Description.String,
			ImageURL:        c.ImageUrl.String,
			WordCount:       int(c.WordCount.Int32),
			ChannelUsername: c.ChannelUsername.String,
			ChannelTitle:    c.ChannelTitle.String,
			ChannelID:       c.ChannelID.Int64,
			MessageID:       c.MessageID.Int64,
			Views:           int(c.Views.Int32),
			Forwards:        int(c.Forwards.Int32),
			HasMedia:        c.HasMedia.Bool,
			MediaType:       c.MediaType.String,
			Status:          c.Status,
			ErrorMessage:    c.ErrorMessage.String,
			Language:        c.Language.String,
			ResolvedAt:      c.ResolvedAt.Time,
			CreatedAt:       c.CreatedAt.Time,
			ExpiresAt:       c.ExpiresAt.Time,
		}
	}
	return links, nil
}
