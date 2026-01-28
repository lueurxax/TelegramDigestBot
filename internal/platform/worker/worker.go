// Package worker provides a generic worker loop abstraction for background processing.
// It encapsulates common patterns like poll-based loops, periodic tasks, context cancellation,
// and recovery mechanisms found across enrichment, pipeline, and factcheck workers.
package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
)

// ProcessFunc is called each iteration to process work items.
// It should return quickly if no work is available.
type ProcessFunc func(ctx context.Context) error

// PeriodicTask represents a task that runs at regular intervals.
type PeriodicTask struct {
	Name     string
	Interval time.Duration
	Run      func(ctx context.Context)
	lastRun  time.Time
}

// Config configures the worker loop behavior.
type Config struct {
	// Name identifies the worker for logging.
	Name string

	// PollInterval is the time between process iterations.
	PollInterval time.Duration

	// Process is called each iteration to do the main work.
	Process ProcessFunc

	// PeriodicTasks are run at their configured intervals.
	PeriodicTasks []PeriodicTask

	// OnStart is called once when the loop starts.
	OnStart func(ctx context.Context)

	// OnStop is called once when the loop exits.
	OnStop func()

	// OnError is called when Process returns an error.
	// Return true to continue, false to exit the loop.
	OnError func(err error) bool

	// Logger for the worker.
	Logger *zerolog.Logger
}

// Loop runs a worker loop with the given configuration.
// It handles context cancellation, periodic tasks, and error recovery.
// Returns ctx.Err() when the context is canceled, or the first fatal error.
func Loop(ctx context.Context, cfg Config) error {
	logger := cfg.Logger
	if logger == nil {
		nop := zerolog.Nop()
		logger = &nop
	}

	logger.Info().Str(logFieldWorker, cfg.Name).Msg("starting worker loop")

	if cfg.OnStart != nil {
		cfg.OnStart(ctx)
	}

	defer func() {
		if cfg.OnStop != nil {
			cfg.OnStop()
		}

		logger.Info().Str(logFieldWorker, cfg.Name).Msg("worker loop stopped")
	}()

	// Initialize periodic task timestamps
	periodicTasks := make([]PeriodicTask, len(cfg.PeriodicTasks))
	copy(periodicTasks, cfg.PeriodicTasks)

	for {
		if err := checkCanceled(ctx, cfg.Name); err != nil {
			return err
		}

		// Run periodic tasks that are due
		runPeriodicTasks(ctx, periodicTasks, logger)

		if err := runProcessStep(ctx, cfg, logger); err != nil {
			return err
		}

		// Wait before next iteration
		if err := Wait(ctx, cfg.PollInterval); err != nil {
			return err
		}
	}
}

// runPeriodicTasks runs any periodic tasks that are due.
func runPeriodicTasks(ctx context.Context, tasks []PeriodicTask, logger *zerolog.Logger) {
	now := time.Now()

	for i := range tasks {
		task := &tasks[i]
		if task.Interval <= 0 || task.Run == nil {
			continue
		}

		if now.Sub(task.lastRun) >= task.Interval {
			logger.Debug().Str("task", task.Name).Msg("running periodic task")
			task.Run(ctx)
			task.lastRun = now
		}
	}
}

func runProcessStep(ctx context.Context, cfg Config, logger *zerolog.Logger) error {
	if cfg.Process == nil {
		return nil
	}

	if err := cfg.Process(ctx); err != nil {
		if cfg.OnError != nil {
			if !cfg.OnError(err) {
				return err
			}

			return nil
		}

		logger.Error().Err(err).Str(logFieldWorker, cfg.Name).Msg("process error")
	}

	return nil
}

func checkCanceled(ctx context.Context, name string) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("worker loop %s: %w", name, ctx.Err())
	default:
		return nil
	}
}

// Wait blocks until duration elapses or context is canceled.
// Returns a wrapped context error if context is canceled.
func Wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("wait interrupted: %w", ctx.Err())
	case <-time.After(d):
		return nil
	}
}

// WaitUntil blocks until the specified time or context is canceled.
// Returns ctx.Err() if context is canceled.
func WaitUntil(ctx context.Context, t time.Time) error {
	d := time.Until(t)
	if d <= 0 {
		return nil
	}

	return Wait(ctx, d)
}

// RunWithTimeout runs fn with a timeout derived from the parent context.
// The function receives a context that will be canceled after timeout.
func RunWithTimeout(ctx context.Context, timeout time.Duration, fn func(ctx context.Context) error) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return fn(timeoutCtx)
}

// RecoverPanic recovers from panics and logs them.
// Use as: defer worker.RecoverPanic(logger, "operation name")
func RecoverPanic(logger *zerolog.Logger, operation string) {
	if r := recover(); r != nil {
		logger.Error().
			Interface("panic", r).
			Str("operation", operation).
			Msg("recovered from panic")
	}
}
