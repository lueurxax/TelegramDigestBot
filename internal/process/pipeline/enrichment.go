package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/links/linkextract"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

func (p *Pipeline) enrichWithLinks(ctx context.Context, msg *db.RawMessage, enabled bool, maxLinks int, webTTL, tgTTL time.Duration) ([]db.ResolvedLink, error) {
	if !enabled || p.linkResolver == nil {
		return nil, nil
	}

	resolutionText := p.buildLinkResolutionText(msg.Text, msg.EntitiesJSON, msg.MediaJSON)

	resolvedLinks, err := p.linkResolver.ResolveLinks(ctx, resolutionText, maxLinks, webTTL, tgTTL)
	if err != nil {
		return nil, fmt.Errorf("resolve links: %w", err)
	}

	// Persist message-link associations
	for i, link := range resolvedLinks {
		if link.ID != "" {
			if err := p.database.LinkMessageToLink(ctx, msg.ID, link.ID, i); err != nil {
				p.logger.Error().Err(err).Str(LogFieldMsgID, msg.ID).Str("link_id", link.ID).Msg("failed to link message to link")
			}
		}
	}

	return resolvedLinks, nil
}

func (p *Pipeline) buildLinkResolutionText(text string, entitiesJSON, mediaJSON []byte) string {
	urls := linkextract.ExtractURLsFromJSON(entitiesJSON, mediaJSON)
	if len(urls) == 0 {
		return text
	}

	if strings.TrimSpace(text) == "" {
		return strings.Join(urls, " ")
	}

	return strings.TrimSpace(text + " " + strings.Join(urls, " "))
}

// seedLinksForCrawler extracts URLs from a message and seeds them into the crawler queue.
// This is a non-blocking, opportunistic operation - errors are logged but don't affect processing.
func (p *Pipeline) seedLinksForCrawler(ctx context.Context, logger zerolog.Logger, m db.RawMessage) {
	if p.linkSeeder == nil {
		return
	}

	// Extract URLs from entities and media
	urls := linkextract.ExtractURLsFromJSON(m.EntitiesJSON, m.MediaJSON)
	if len(urls) == 0 {
		return
	}

	result := p.linkSeeder.SeedLinks(ctx, LinkSeedInput{
		ChannelID: m.ChannelID,
		MessageID: m.TGMessageID,
		URLs:      urls,
	})

	if result.Enqueued > 0 {
		logger.Debug().
			Str(LogFieldMsgID, m.ID).
			Int("extracted", result.Extracted).
			Int("enqueued", result.Enqueued).
			Msg("seeded links for crawling")
	}
}
