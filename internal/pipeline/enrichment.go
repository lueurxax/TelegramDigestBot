package pipeline

import (
	"context"
	"time"

	"github.com/lueurxax/telegram-digest-bot/internal/db"
)

func (p *Pipeline) enrichWithLinks(ctx context.Context, msg *db.RawMessage, enabled bool, maxLinks int, webTTL, tgTTL time.Duration) ([]db.ResolvedLink, error) {
	if !enabled || p.linkResolver == nil {
		return nil, nil
	}

	resolvedLinks, err := p.linkResolver.ResolveLinks(ctx, msg.Text, maxLinks, webTTL, tgTTL)
	if err != nil {
		return nil, err
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
