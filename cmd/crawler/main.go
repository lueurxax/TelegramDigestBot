package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/crawler"
)

func main() {
	// Setup logger
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	// Load configuration
	cfg, err := crawler.Load()
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to load configuration")
	}

	// Set log level
	setLogLevel(cfg.LogLevel)

	// Create context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		logger.Info().Str("signal", sig.String()).Msg("Received shutdown signal")
		cancel()
	}()

	// Create crawler
	c, err := crawler.New(cfg, &logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to create crawler")
	}

	// Start health server
	healthServer := crawler.NewHealthServer(c, cfg.HealthPort)

	go func() {
		logger.Info().Int("port", cfg.HealthPort).Msg("Starting health server")

		if err := healthServer.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error().Err(err).Msg("Health server error")
		}
	}()

	// Mark as ready after brief delay for initialization
	healthServer.SetReady(true)

	// Run crawler
	logger.Info().Msg("Starting crawler")

	if err := c.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Fatal().Err(err).Msg("Crawler error")
	}

	logger.Info().Msg("Crawler stopped")
}

// setLogLevel sets the global log level based on the configuration.
func setLogLevel(level string) {
	switch level {
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
}
