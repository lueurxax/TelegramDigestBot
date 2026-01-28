package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/app"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

func main() {
	mode := flag.String("mode", "", "Service mode (bot, reader, worker, digest)")
	once := flag.Bool("once", false, "Run once and exit (for digest mode)")

	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	logger := newLogger(cfg.AppEnv)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	poolOpts := db.PoolOptions{
		MaxConns:          cfg.DBMaxConnections,
		MinConns:          cfg.DBMinConnections,
		MaxConnIdleTime:   cfg.DBMaxConnIdleTime,
		MaxConnLifetime:   cfg.DBMaxConnLifetime,
		HealthCheckPeriod: cfg.DBHealthCheckPeriod,
	}

	database, err := db.NewWithOptions(ctx, cfg.PostgresDSN, poolOpts)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer database.Close()

	if err := database.Migrate(ctx); err != nil {
		logger.Fatal().Err(err).Msg("failed to run migrations")
	}

	application := app.New(cfg, database, &logger)

	// Start health server in background
	go func() {
		if err := application.StartHealthServer(ctx); err != nil {
			logger.Error().Err(err).Msg("health check server error")
		}
	}()

	if err := runMode(ctx, application, *mode, *once); err != nil {
		if errors.Is(err, context.Canceled) {
			logger.Info().Msg("application stopped")
			return
		}

		logger.Fatal().Err(err).Msg("application error")
	}
}

func newLogger(appEnv string) zerolog.Logger {
	if appEnv == "local" {
		return zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).With().Timestamp().Logger()
	}

	return zerolog.New(os.Stderr).With().Timestamp().Logger()
}

func runMode(ctx context.Context, application *app.App, mode string, once bool) error {
	switch mode {
	case "bot":
		return application.RunBot(ctx)
	case "reader":
		return application.RunReader(ctx)
	case "worker":
		return application.RunWorker(ctx)
	case "digest":
		return application.RunDigest(ctx, once)
	default:
		log.Fatalf("Usage: %s --mode=[bot|reader|worker|digest]", os.Args[0])

		return nil
	}
}
