package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/storage/sqlc"
)

// ResolvedLink is an alias for the domain type.
type ResolvedLink = domain.ResolvedLink

func resolvedLinkFromRow(
	id pgtype.UUID,
	url, domain, linkType string,
	title, content, author pgtype.Text,
	publishedAt pgtype.Timestamptz,
	description, imageUrl pgtype.Text,
	wordCount pgtype.Int4,
	channelUsername, channelTitle pgtype.Text,
	channelID, messageID pgtype.Int8,
	views, forwards pgtype.Int4,
	hasMedia pgtype.Bool,
	mediaType pgtype.Text,
	status string,
	errorMessage, language pgtype.Text,
	resolvedAt, createdAt, expiresAt pgtype.Timestamptz,
) ResolvedLink {
	return ResolvedLink{
		ID:              fromUUID(id),
		URL:             url,
		Domain:          domain,
		LinkType:        linkType,
		Title:           title.String,
		Content:         content.String,
		Author:          author.String,
		PublishedAt:     publishedAt.Time,
		Description:     description.String,
		ImageURL:        imageUrl.String,
		WordCount:       int(wordCount.Int32),
		ChannelUsername: channelUsername.String,
		ChannelTitle:    channelTitle.String,
		ChannelID:       channelID.Int64,
		MessageID:       messageID.Int64,
		Views:           int(views.Int32),
		Forwards:        int(forwards.Int32),
		HasMedia:        hasMedia.Bool,
		MediaType:       mediaType.String,
		Status:          status,
		ErrorMessage:    errorMessage.String,
		Language:        language.String,
		ResolvedAt:      resolvedAt.Time,
		CreatedAt:       createdAt.Time,
		ExpiresAt:       expiresAt.Time,
	}
}

func (db *DB) GetLinkCache(ctx context.Context, url string) (*ResolvedLink, error) {
	c, err := db.Queries.GetLinkCache(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("get link cache: %w", err)
	}

	link := resolvedLinkFromRow(
		c.ID, c.Url, c.Domain, c.LinkType,
		c.Title, c.Content, c.Author,
		c.PublishedAt,
		c.Description, c.ImageUrl,
		c.WordCount,
		c.ChannelUsername, c.ChannelTitle,
		c.ChannelID, c.MessageID,
		c.Views, c.Forwards,
		c.HasMedia,
		c.MediaType,
		c.Status,
		c.ErrorMessage, c.Language,
		c.ResolvedAt, c.CreatedAt, c.ExpiresAt,
	)

	return &link, nil
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
		WordCount:       pgtype.Int4{Int32: safeIntToInt32(link.WordCount), Valid: link.WordCount != 0},
		ChannelUsername: toText(link.ChannelUsername),
		ChannelTitle:    toText(link.ChannelTitle),
		ChannelID:       toInt8(link.ChannelID),
		MessageID:       toInt8(link.MessageID),
		Views:           pgtype.Int4{Int32: safeIntToInt32(link.Views), Valid: link.Views != 0},
		Forwards:        pgtype.Int4{Int32: safeIntToInt32(link.Forwards), Valid: link.Forwards != 0},
		HasMedia:        pgtype.Bool{Bool: link.HasMedia, Valid: true},
		MediaType:       toText(link.MediaType),
		Status:          link.Status,
		ErrorMessage:    toText(link.ErrorMessage),
		Language:        toText(link.Language),
		ResolvedAt:      toTimestamptz(link.ResolvedAt),
		ExpiresAt:       toTimestamptz(link.ExpiresAt),
	})
	if err != nil {
		return "", fmt.Errorf("save link cache: %w", err)
	}

	return fromUUID(id), nil
}

func (db *DB) LinkMessageToLink(ctx context.Context, rawMsgID, linkCacheID string, position int) error {
	if err := db.Queries.LinkMessageToLink(ctx, sqlc.LinkMessageToLinkParams{
		RawMessageID: toUUID(rawMsgID),
		LinkCacheID:  toUUID(linkCacheID),
		Position:     pgtype.Int4{Int32: safeIntToInt32(position), Valid: true},
	}); err != nil {
		return fmt.Errorf("link message to link: %w", err)
	}

	return nil
}

func (db *DB) GetLinksForMessage(ctx context.Context, rawMsgID string) ([]ResolvedLink, error) {
	sqlcLinks, err := db.Queries.GetLinksForMessage(ctx, toUUID(rawMsgID))
	if err != nil {
		return nil, fmt.Errorf("get links for message: %w", err)
	}

	links := make([]ResolvedLink, len(sqlcLinks))

	for i, c := range sqlcLinks {
		links[i] = resolvedLinkFromRow(
			c.ID, c.Url, c.Domain, c.LinkType,
			c.Title, c.Content, c.Author,
			c.PublishedAt,
			c.Description, c.ImageUrl,
			c.WordCount,
			c.ChannelUsername, c.ChannelTitle,
			c.ChannelID, c.MessageID,
			c.Views, c.Forwards,
			c.HasMedia,
			c.MediaType,
			c.Status,
			c.ErrorMessage, c.Language,
			c.ResolvedAt, c.CreatedAt, c.ExpiresAt,
		)
	}

	return links, nil
}
