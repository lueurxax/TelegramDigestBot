package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/platform/settings"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const linkBackfillDefaultLimit = settings.DefaultLinkBackfillLimit

func (p *Pipeline) runLinkBackfill(ctx context.Context, logger zerolog.Logger) error {
	req, err := p.loadBackfillRequest(ctx, logger)
	if err != nil {
		return err
	}

	if req.Hours <= 0 {
		return nil
	}

	if p.linkResolver == nil {
		logger.Warn().Msg("link backfill skipped: link resolver not configured")

		return p.clearBackfillRequest(ctx)
	}

	return p.processBackfill(ctx, logger, req)
}

func (p *Pipeline) loadBackfillRequest(ctx context.Context, logger zerolog.Logger) (settings.LinkBackfillRequest, error) {
	var req settings.LinkBackfillRequest

	if err := p.database.GetSetting(ctx, settings.SettingLinkBackfillRequest, &req); err != nil {
		logger.Warn().Err(err).Msg("failed to load link backfill request")

		return req, fmt.Errorf("get link backfill setting: %w", err)
	}

	return req, nil
}

func (p *Pipeline) clearBackfillRequest(ctx context.Context) error {
	if err := p.database.DeleteSetting(ctx, settings.SettingLinkBackfillRequest); err != nil {
		return fmt.Errorf("delete link backfill setting: %w", err)
	}

	return nil
}

func (p *Pipeline) processBackfill(ctx context.Context, logger zerolog.Logger, req settings.LinkBackfillRequest) error {
	limit := req.Limit
	if limit <= 0 {
		limit = linkBackfillDefaultLimit
	}

	maxLinks := p.cfg.MaxLinksPerMessage
	p.getSetting(ctx, SettingMaxLinksPerMessage, &maxLinks, logger)

	webTTL := p.getDurationSetting(ctx, SettingLinkCacheTTL, p.cfg.LinkCacheTTL, logger)
	tgTTL := p.getDurationSetting(ctx, SettingTgLinkCacheTTL, p.cfg.TelegramLinkCacheTTL, logger)

	since := time.Now().UTC().Add(-time.Duration(req.Hours) * time.Hour)

	messages, err := p.database.GetRawMessagesForLinkBackfill(ctx, since, limit)
	if err != nil {
		logger.Warn().Err(err).Int(LogFieldHours, req.Hours).Int(LogFieldLimit, limit).Msg("failed to load link backfill candidates")

		return fmt.Errorf("get raw messages for link backfill: %w", err)
	}

	stats := p.resolveBackfillMessages(ctx, logger, messages, maxLinks, webTTL, tgTTL)

	logger.Info().
		Int("candidates", len(messages)).
		Int("resolved_links", stats.resolved).
		Int("linked", stats.linked).
		Int("skipped", stats.skipped).
		Int("errors", stats.failures).
		Int(LogFieldHours, req.Hours).
		Int(LogFieldLimit, limit).
		Msg("link backfill completed")

	if err := p.database.DeleteSetting(ctx, settings.SettingLinkBackfillRequest); err != nil {
		logger.Warn().Err(err).Msg("failed to clear link backfill request")
	}

	return nil
}

type backfillStats struct {
	skipped  int
	resolved int
	linked   int
	failures int
}

func (p *Pipeline) resolveBackfillMessages(ctx context.Context, logger zerolog.Logger, messages []db.RawMessage, maxLinks int, webTTL, tgTTL time.Duration) backfillStats {
	var stats backfillStats

	for _, msg := range messages {
		resolutionText := p.buildLinkResolutionText(msg.Text, msg.EntitiesJSON, msg.MediaJSON)
		if strings.TrimSpace(resolutionText) == "" {
			stats.skipped++

			continue
		}

		links, err := p.linkResolver.ResolveLinks(ctx, resolutionText, maxLinks, webTTL, tgTTL)
		if err != nil {
			stats.failures++

			logger.Warn().Err(err).Str(LogFieldMsgID, msg.ID).Msg("link backfill resolve failed")

			continue
		}

		if len(links) == 0 {
			continue
		}

		stats.resolved += len(links)

		for i, link := range links {
			if link.ID == "" {
				continue
			}

			if err := p.database.LinkMessageToLink(ctx, msg.ID, link.ID, i); err != nil {
				stats.failures++

				logger.Warn().Err(err).Str(LogFieldMsgID, msg.ID).Str(LogFieldLinkID, link.ID).Msg("failed to link backfilled message")

				continue
			}

			stats.linked++
		}
	}

	return stats
}
