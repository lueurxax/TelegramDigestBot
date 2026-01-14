package app

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/bot"
	"github.com/lueurxax/telegram-digest-bot/internal/core/links"
	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
	"github.com/lueurxax/telegram-digest-bot/internal/ingest/reader"
	"github.com/lueurxax/telegram-digest-bot/internal/output/digest"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/observability"
	"github.com/lueurxax/telegram-digest-bot/internal/process/pipeline"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const errBotInit = "bot initialization failed: %w"

// App holds the application dependencies and provides methods to run different modes.
type App struct {
	cfg      *config.Config
	database *db.DB
	logger   *zerolog.Logger
}

// New creates a new App instance with the given dependencies.
func New(cfg *config.Config, database *db.DB, logger *zerolog.Logger) *App {
	return &App{
		cfg:      cfg,
		database: database,
		logger:   logger,
	}
}

// StartHealthServer starts the health check and metrics server.
func (a *App) StartHealthServer(ctx context.Context) error {
	srv := observability.NewServer(a.database, a.cfg.HealthPort, a.logger)

	if err := srv.Start(ctx); err != nil {
		return fmt.Errorf("health server start: %w", err)
	}

	return nil
}

// RunBot runs the bot mode.
func (a *App) RunBot(ctx context.Context) error {
	a.logger.Info().Msg("Starting bot mode")

	llmClient := a.newLLMClient()

	// Create a digest scheduler for preview commands (nil poster since we only need BuildDigest)
	digestBuilder := digest.New(a.cfg, a.database, nil, llmClient, a.logger)

	b, err := bot.New(a.cfg, a.database, digestBuilder, llmClient, a.logger)
	if err != nil {
		return fmt.Errorf(errBotInit, err)
	}

	if err := b.Run(ctx); err != nil {
		return fmt.Errorf("bot run: %w", err)
	}

	return nil
}

// RunReader runs the reader mode.
func (a *App) RunReader(ctx context.Context) error {
	a.logger.Info().Msg("Starting reader mode")

	channelRepo := db.NewChannelRepoAdapter(a.database)
	r := reader.New(a.cfg, a.database, a.database, channelRepo, a.logger)

	if err := r.Run(ctx); err != nil {
		return fmt.Errorf("reader run: %w", err)
	}

	return nil
}

// RunWorker runs the worker mode.
func (a *App) RunWorker(ctx context.Context) error {
	a.logger.Info().Msg("Starting worker mode")

	llmClient := a.newLLMClient()
	resolver := a.newLinkResolver()

	p := pipeline.New(a.cfg, a.database, llmClient, resolver, a.logger)

	if err := p.Run(ctx); err != nil {
		return fmt.Errorf("pipeline run: %w", err)
	}

	return nil
}

// RunDigest runs the digest mode.
func (a *App) RunDigest(ctx context.Context, once bool) error {
	a.logger.Info().Bool("once", once).Msg("Starting digest mode")

	llmClient := a.newLLMClient()

	// Create bot as DigestPoster only (nil digestBuilder since bot won't process commands)
	b, err := bot.New(a.cfg, a.database, nil, llmClient, a.logger)
	if err != nil {
		return fmt.Errorf(errBotInit, err)
	}

	s := digest.New(a.cfg, a.database, b, llmClient, a.logger)

	if once {
		if err := s.RunOnce(ctx); err != nil {
			return fmt.Errorf("digest run once: %w", err)
		}

		return nil
	}

	if err := s.Run(ctx); err != nil {
		return fmt.Errorf("digest run: %w", err)
	}

	return nil
}

// newLLMClient creates a new LLM client.
func (a *App) newLLMClient() llm.Client {
	return llm.New(a.cfg, a.database, a.logger)
}

// newLinkResolver creates a new link resolver.
func (a *App) newLinkResolver() *links.Resolver {
	return links.New(a.cfg, a.database, db.NewChannelRepoAdapter(a.database), nil, a.logger)
}
