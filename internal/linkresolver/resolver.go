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

// ErrUnsupportedLinkType indicates a link type that cannot be resolved.
var ErrUnsupportedLinkType = errors.New("unsupported link type")

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

	params := r.normalizeResolveParams(maxLinks, webTTL, tgTTL)
	if len(links) > params.maxLinks {
		links = links[:params.maxLinks]
	}

	var results []db.ResolvedLink

	for _, link := range links {
		if resolved := r.resolveSingleLink(ctx, link, params); resolved != nil {
			results = append(results, *resolved)
		}
	}

	return results, nil
}

type resolveParams struct {
	maxLinks int
	webTTL   time.Duration
	tgTTL    time.Duration
}

func (r *Resolver) normalizeResolveParams(maxLinks int, webTTL, tgTTL time.Duration) resolveParams {
	if maxLinks <= 0 {
		maxLinks = r.maxLinks
	}

	if webTTL <= 0 {
		webTTL = r.webCacheTTL
	}

	if tgTTL <= 0 {
		tgTTL = r.tgCacheTTL
	}

	return resolveParams{maxLinks: maxLinks, webTTL: webTTL, tgTTL: tgTTL}
}

func (r *Resolver) resolveSingleLink(ctx context.Context, link linkextract.Link, params resolveParams) *db.ResolvedLink {
	// Check cache first
	cached, err := r.database.GetLinkCache(ctx, link.URL)
	if err == nil && cached != nil && time.Now().Before(cached.ExpiresAt) {
		return cached
	}

	resolved, err := r.dispatchLinkResolution(ctx, &link, params)
	if err != nil {
		r.handleResolutionError(ctx, link, err)
		return nil
	}

	if resolved != nil {
		r.cacheResolvedLink(ctx, resolved, link.URL)
	}

	return resolved
}

func (r *Resolver) dispatchLinkResolution(ctx context.Context, link *linkextract.Link, params resolveParams) (*db.ResolvedLink, error) {
	switch link.Type {
	case linkextract.LinkTypeWeb:
		return r.resolveWebLink(ctx, link, params.webTTL)
	case linkextract.LinkTypeTelegram:
		return r.resolveTelegramLink(ctx, link, params.tgTTL)
	default:
		return nil, ErrUnsupportedLinkType
	}
}

func (r *Resolver) handleResolutionError(ctx context.Context, link linkextract.Link, err error) {
	if errors.Is(err, ErrClientNotInitialized) {
		r.logger.Debug().Str(logKeyURL, link.URL).Msg("skipping telegram link resolution: client not initialized")
		return
	}

	if errors.Is(err, ErrUnsupportedLinkType) {
		return
	}

	r.logger.Warn().Err(err).Str(logKeyURL, link.URL).Msg("failed to resolve link")
	_, _ = r.database.SaveLinkCache(ctx, &db.ResolvedLink{
		URL:          link.URL,
		Domain:       link.Domain,
		LinkType:     string(link.Type),
		Status:       db.LinkStatusFailed,
		ErrorMessage: err.Error(),
		ExpiresAt:    time.Now().Add(1 * time.Hour),
	})
}

func (r *Resolver) cacheResolvedLink(ctx context.Context, resolved *db.ResolvedLink, url string) {
	id, err := r.database.SaveLinkCache(ctx, resolved)
	if err != nil {
		r.logger.Error().Err(err).Str(logKeyURL, url).Msg("failed to save link to cache")
	} else {
		resolved.ID = id
	}
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
