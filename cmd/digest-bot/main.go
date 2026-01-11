package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"time"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/config"
	"github.com/lueurxax/telegram-digest-bot/internal/db"
	"github.com/lueurxax/telegram-digest-bot/internal/digest"
	"github.com/lueurxax/telegram-digest-bot/internal/linkresolver"
	"github.com/lueurxax/telegram-digest-bot/internal/llm"
	"github.com/lueurxax/telegram-digest-bot/internal/observability"
	"github.com/lueurxax/telegram-digest-bot/internal/pipeline"
	"github.com/lueurxax/telegram-digest-bot/internal/telegrambot"
	"github.com/lueurxax/telegram-digest-bot/internal/telegramreader"
)

func main() {
	mode := flag.String("mode", "", "Service mode (bot, reader, worker, digest)")
	once := flag.Bool("once", false, "Run once and exit (for digest mode)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	var logger zerolog.Logger
	if cfg.AppEnv == "local" {
		logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).With().Timestamp().Logger()
	} else {
		logger = zerolog.New(os.Stderr).With().Timestamp().Logger()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := db.New(ctx, cfg.PostgresDSN)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer database.Close()

	if err := database.Migrate(ctx); err != nil {
		logger.Fatal().Err(err).Msg("failed to run migrations")
	}

	go func() {
		healthSrv := observability.NewServer(database, cfg.HealthPort, &logger)
		if err := healthSrv.Start(ctx); err != nil {
			logger.Error().Err(err).Msg("health check server error")
		}
	}()

	switch *mode {
	case "bot":
		runBot(ctx, cfg, database, logger)
	case "reader":
		runReader(ctx, cfg, database, logger)
	case "worker":
		runWorker(ctx, cfg, database, logger)
	case "digest":
		runDigest(ctx, cfg, database, logger, *once)
	default:
		fmt.Printf("Usage: %s --mode=[bot|reader|worker|digest]\n", os.Args[0])
		os.Exit(1)
	}
}

func runBot(ctx context.Context, cfg *config.Config, database *db.DB, logger zerolog.Logger) {
	logger.Info().Msg("Starting bot mode")
	llmClient := llm.New(cfg, database, &logger)
	bot, err := telegrambot.New(cfg, database, llmClient, &logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("bot initialization failed")
	}
	if err := bot.Run(ctx); err != nil {
		logger.Fatal().Err(err).Msg("bot failed")
	}
}

func runReader(ctx context.Context, cfg *config.Config, database *db.DB, logger zerolog.Logger) {
	logger.Info().Msg("Starting reader mode")
	reader := telegramreader.New(cfg, database, &logger)
	if err := reader.Run(ctx); err != nil {
		logger.Fatal().Err(err).Msg("reader failed")
	}
}

func runWorker(ctx context.Context, cfg *config.Config, database *db.DB, logger zerolog.Logger) {
	logger.Info().Msg("Starting worker mode")

	llmClient := llm.New(cfg, database, &logger)
	resolver := linkresolver.New(cfg, database, nil, &logger)
	p := pipeline.New(cfg, database, llmClient, resolver, &logger)
	if err := p.Run(ctx); err != nil {
		logger.Fatal().Err(err).Msg("pipeline failed")
	}
}

func runDigest(ctx context.Context, cfg *config.Config, database *db.DB, logger zerolog.Logger, once bool) {
	logger.Info().Bool("once", once).Msg("Starting digest mode")
	llmClient := llm.New(cfg, database, &logger)
	bot, err := telegrambot.New(cfg, database, llmClient, &logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("bot initialization failed")
	}
	s := digest.New(cfg, database, bot, llmClient, &logger)
	if once {
		if err := s.RunOnce(ctx); err != nil {
			logger.Fatal().Err(err).Msg("digest run once failed")
		}
		return
	}
	if err := s.Run(ctx); err != nil {
		logger.Fatal().Err(err).Msg("digest scheduler failed")
	}
}
