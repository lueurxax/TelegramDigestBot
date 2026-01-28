package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
)

const (
	// pollInterval is the sleep duration between ticker checks to prevent busy-waiting.
	pollInterval   = 100 * time.Millisecond
	logFieldWorker = "worker"
	logFieldTask   = "task"

	// errFmtSingleTickerLoop is the error format for single ticker loop context errors.
	errFmtSingleTickerLoop = "single ticker loop %s: %w"
)

// TickerTask represents a task triggered by a ticker.
type TickerTask struct {
	Name     string
	Interval time.Duration
	Run      func(ctx context.Context)
}

// TickerConfig configures a ticker-based worker loop.
type TickerConfig struct {
	// Name identifies the worker for logging.
	Name string

	// Tasks are the ticker-triggered tasks to run.
	Tasks []TickerTask

	// OnStart is called once when the loop starts.
	OnStart func(ctx context.Context)

	// OnStop is called once when the loop exits.
	OnStop func()

	// Logger for the worker.
	Logger *zerolog.Logger
}

// TickerLoop runs a ticker-based worker loop.
// Each task runs on its own ticker at the configured interval.
// Returns a wrapped context error when the context is canceled.
func TickerLoop(ctx context.Context, cfg TickerConfig) error {
	logger := getLogger(cfg.Logger)
	logger.Info().Str(logFieldWorker, cfg.Name).Msg("starting ticker loop")

	runOnStart(ctx, cfg.OnStart)
	defer runOnStop(cfg.OnStop, logger, cfg.Name, "ticker loop stopped")

	if len(cfg.Tasks) == 0 {
		<-ctx.Done()

		return fmt.Errorf("ticker loop %s: %w", cfg.Name, ctx.Err())
	}

	tickers := createTickers(cfg.Tasks)
	defer stopTickers(tickers)

	runInitialTasks(ctx, cfg.Tasks, tickers, logger)

	return runTickerLoop(ctx, cfg.Tasks, tickers, logger)
}

// createTickers creates time.Ticker instances for each task with a positive interval.
func createTickers(tasks []TickerTask) []*time.Ticker {
	tickers := make([]*time.Ticker, len(tasks))

	for i, task := range tasks {
		if task.Interval > 0 {
			tickers[i] = time.NewTicker(task.Interval)
		}
	}

	return tickers
}

// stopTickers stops all non-nil tickers.
func stopTickers(tickers []*time.Ticker) {
	for _, t := range tickers {
		if t != nil {
			t.Stop()
		}
	}
}

// runInitialTasks runs all tasks with valid tickers immediately.
func runInitialTasks(ctx context.Context, tasks []TickerTask, tickers []*time.Ticker, logger *zerolog.Logger) {
	for i, task := range tasks {
		if task.Run != nil && tickers[i] != nil {
			logger.Debug().Str(logFieldTask, task.Name).Msg("running initial task")
			task.Run(ctx)
		}
	}
}

// runTickerLoop is the main loop that checks tickers and runs tasks.
func runTickerLoop(ctx context.Context, tasks []TickerTask, tickers []*time.Ticker, logger *zerolog.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("ticker loop: %w", ctx.Err())
		default:
		}

		checkAndRunTasks(ctx, tasks, tickers, logger)

		if err := Wait(ctx, pollInterval); err != nil {
			return err
		}
	}
}

// checkAndRunTasks checks each ticker and runs the task if fired.
func checkAndRunTasks(ctx context.Context, tasks []TickerTask, tickers []*time.Ticker, logger *zerolog.Logger) {
	for i, task := range tasks {
		if tickers[i] == nil || task.Run == nil {
			continue
		}

		select {
		case <-tickers[i].C:
			logger.Debug().Str(logFieldTask, task.Name).Msg("ticker fired")
			task.Run(ctx)
		default:
			// Non-blocking check
		}
	}
}

// SingleTickerLoop runs a simple loop with one main ticker and optional secondary tasks.
// This is the most common pattern used by the digest scheduler.
func SingleTickerLoop(ctx context.Context, cfg SingleTickerConfig) error {
	logger := getLogger(cfg.Logger)
	logger.Info().Str(logFieldWorker, cfg.Name).Msg("starting single ticker loop")

	runOnStart(ctx, cfg.OnStart)
	defer runOnStop(cfg.OnStop, logger, cfg.Name, "single ticker loop stopped")

	if cfg.RunOnStart && cfg.OnTick != nil {
		cfg.OnTick(ctx)
	}

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	if cfg.SecondaryInterval > 0 {
		return runDualTickerLoop(ctx, cfg, ticker)
	}

	return runSingleTickerMainLoop(ctx, cfg, ticker)
}

// runDualTickerLoop handles the loop when a secondary ticker is configured.
func runDualTickerLoop(ctx context.Context, cfg SingleTickerConfig, ticker *time.Ticker) error {
	secondaryTicker := time.NewTicker(cfg.SecondaryInterval)
	defer secondaryTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf(errFmtSingleTickerLoop, cfg.Name, ctx.Err())
		case <-ticker.C:
			if cfg.OnTick != nil {
				cfg.OnTick(ctx)
			}
		case <-secondaryTicker.C:
			if cfg.OnSecondaryTick != nil {
				cfg.OnSecondaryTick(ctx)
			}
		}
	}
}

// runSingleTickerMainLoop handles the loop with only a primary ticker.
func runSingleTickerMainLoop(ctx context.Context, cfg SingleTickerConfig, ticker *time.Ticker) error {
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf(errFmtSingleTickerLoop, cfg.Name, ctx.Err())
		case <-ticker.C:
			if cfg.OnTick != nil {
				cfg.OnTick(ctx)
			}
		}
	}
}

// SingleTickerConfig configures a single-ticker loop with optional secondary ticker.
type SingleTickerConfig struct {
	// Name identifies the worker for logging.
	Name string

	// Interval is the main ticker interval.
	Interval time.Duration

	// OnTick is called when the main ticker fires.
	OnTick func(ctx context.Context)

	// RunOnStart runs OnTick immediately when starting.
	RunOnStart bool

	// SecondaryInterval is the interval for secondary periodic tasks (0 to disable).
	SecondaryInterval time.Duration

	// OnSecondaryTick is called when the secondary ticker fires.
	OnSecondaryTick func(ctx context.Context)

	// OnStart is called once when the loop starts.
	OnStart func(ctx context.Context)

	// OnStop is called once when the loop exits.
	OnStop func()

	// Logger for the worker.
	Logger *zerolog.Logger
}

// getLogger returns the provided logger or a nop logger if nil.
func getLogger(logger *zerolog.Logger) *zerolog.Logger {
	if logger == nil {
		nop := zerolog.Nop()

		return &nop
	}

	return logger
}

// runOnStart calls the onStart callback if not nil.
func runOnStart(ctx context.Context, onStart func(ctx context.Context)) {
	if onStart != nil {
		onStart(ctx)
	}
}

// runOnStop calls the onStop callback and logs the stop message.
func runOnStop(onStop func(), logger *zerolog.Logger, name, msg string) {
	if onStop != nil {
		onStop()
	}

	logger.Info().Str(logFieldWorker, name).Msg(msg)
}
