package linkresolver

import (
	"context"
	"errors"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/config"
	"github.com/lueurxax/telegram-digest-bot/internal/db"
	"github.com/lueurxax/telegram-digest-bot/internal/linkextract"
)

const (
	logKeyURL                = "url"
	defaultTimeoutSeconds    = 30
	defaultWebCacheTTLHours  = 24
)

type Resolver struct {
	webFetcher *WebFetcher
	tgResolver *TelegramResolver
	database   *db.DB
	logger     *zerolog.Logger

	webCacheTTL time.Duration
	tgCacheTTL  time.Duration
	maxLinks    int
	maxLen      int
}

func New(cfg *config.Config, database *db.DB, tgClient *telegram.Client, logger *zerolog.Logger) *Resolver {
	// Set default RPS if not provided
	rps := cfg.WebFetchRPS

	if rps <= 0 {
		rps = 2
	}

	timeout := cfg.WebFetchTimeout

	if timeout <= 0 {
		timeout = defaultTimeoutSeconds * time.Second
	}

	// Set default cache TTL if not provided
	webTTL := cfg.LinkCacheTTL

	if webTTL <= 0 {
		webTTL = defaultWebCacheTTLHours * time.Hour
	}

	tgTTL := cfg.TelegramLinkCacheTTL

	if tgTTL <= 0 {
		tgTTL = 1 * time.Hour
	}

	maxLinks := cfg.MaxLinksPerMessage

	if maxLinks <= 0 {
		maxLinks = 3
	}

	maxLen := cfg.MaxContentLength

	if maxLen <= 0 {
		maxLen = 5000
	}

	return &Resolver{
		webFetcher:  NewWebFetcher(rps, timeout),
		tgResolver:  NewTelegramResolver(tgClient, database),
		database:    database,
		logger:      logger,
		webCacheTTL: webTTL,
		tgCacheTTL:  tgTTL,
		maxLinks:    maxLinks,
		maxLen:      maxLen,
	}
}

func (r *Resolver) ResolveLinks(ctx context.Context, text string, maxLinks int, webTTL, tgTTL time.Duration) ([]db.ResolvedLink, error) {
	links := linkextract.ExtractLinks(text)

	if len(links) == 0 {
		return nil, nil
	}

	if maxLinks <= 0 {
		maxLinks = r.maxLinks
	}

	if webTTL <= 0 {
		webTTL = r.webCacheTTL
	}

	if tgTTL <= 0 {
		tgTTL = r.tgCacheTTL
	}

	// Limit number of links
	if len(links) > maxLinks {
		links = links[:maxLinks]
	}

	var results []db.ResolvedLink

	for _, link := range links {
		// Check cache first
		cached, err := r.database.GetLinkCache(ctx, link.URL)
		if err == nil && cached != nil && time.Now().Before(cached.ExpiresAt) {
			results = append(results, *cached)
			continue
		}

		// Resolve based on type
		var resolved *db.ResolvedLink

		switch link.Type {
		case linkextract.LinkTypeWeb:
			resolved, err = r.resolveWebLink(ctx, &link, webTTL)
		case linkextract.LinkTypeTelegram:
			resolved, err = r.resolveTelegramLink(ctx, &link, tgTTL)
		case linkextract.LinkTypeBlocked:
			continue
		default:
			continue
		}

		if err != nil {
			if errors.Is(err, ErrClientNotInitialized) {
				r.logger.Debug().Str(logKeyURL, link.URL).Msg("skipping telegram link resolution: client not initialized")
				continue
			}

			r.logger.Warn().Err(err).Str(logKeyURL, link.URL).Msg("failed to resolve link")
			// Save error to cache to avoid retrying immediately
			_, _ = r.database.SaveLinkCache(ctx, &db.ResolvedLink{
				URL:          link.URL,
				Domain:       link.Domain,
				LinkType:     string(link.Type),
				Status:       db.LinkStatusFailed,
				ErrorMessage: err.Error(),
				ExpiresAt:    time.Now().Add(1 * time.Hour), // Don't retry for 1h
			})

			continue
		}

		if resolved != nil {
			// Save to cache
			id, err := r.database.SaveLinkCache(ctx, resolved)
			if err != nil {
				r.logger.Error().Err(err).Str(logKeyURL, link.URL).Msg("failed to save link to cache")
			} else {
				resolved.ID = id
			}

			results = append(results, *resolved)
		}
	}

	return results, nil
}

func (r *Resolver) resolveWebLink(ctx context.Context, link *linkextract.Link, ttl time.Duration) (*db.ResolvedLink, error) {
	htmlBytes, err := r.webFetcher.Fetch(ctx, link.URL)
	if err != nil {
		return nil, err
	}

	content, err := ExtractWebContent(htmlBytes, link.URL, r.maxLen)
	if err != nil {
		return nil, err
	}

	return &db.ResolvedLink{
		URL:         link.URL,
		Domain:      link.Domain,
		LinkType:    string(linkextract.LinkTypeWeb),
		Title:       content.Title,
		Content:     content.Content,
		Author:      content.Author,
		PublishedAt: content.PublishedAt,
		Description: content.Description,
		ImageURL:    content.ImageURL,
		WordCount:   content.WordCount,
		Status:      db.LinkStatusSuccess,
		ResolvedAt:  time.Now(),
		ExpiresAt:   time.Now().Add(ttl),
	}, nil
}

func (r *Resolver) resolveTelegramLink(ctx context.Context, link *linkextract.Link, ttl time.Duration) (*db.ResolvedLink, error) {
	content, err := r.tgResolver.Resolve(ctx, link)
	if err != nil {
		return nil, err
	}

	return &db.ResolvedLink{
		URL:             link.URL,
		Domain:          "t.me",
		LinkType:        string(linkextract.LinkTypeTelegram),
		ChannelTitle:    content.ChannelTitle,
		ChannelUsername: content.ChannelUsername,
		MessageID:       content.MessageID,
		Content:         content.Text,
		PublishedAt:     content.Date,
		Views:           content.Views,
		Forwards:        content.Forwards,
		HasMedia:        content.HasMedia,
		MediaType:       content.MediaType,
		Status:          db.LinkStatusSuccess,
		ResolvedAt:      time.Now(),
		ExpiresAt:       time.Now().Add(ttl),
	}, nil
}
