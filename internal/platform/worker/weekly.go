package worker

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

const (
	// HoursPerDay is used for weekly task scheduling calculations.
	HoursPerDay = 24
	// defaultWeeklyGracePeriod is 6 days - prevents duplicate runs within same week.
	defaultWeeklyGracePeriod = 6 * HoursPerDay * time.Hour
)

// WeeklyTask represents a task that runs once per week at a specific time.
type WeeklyTask struct {
	// Name identifies the task for logging.
	Name string

	// Day is the day of the week to run (default: Sunday).
	Day time.Weekday

	// Hour is the hour of the day to run (0-23, default: 0).
	Hour int

	// GracePeriod prevents duplicate runs within this duration (default: 6 days).
	// Task won't run if less than this duration has passed since last run.
	GracePeriod time.Duration

	// IsEnabled returns whether the task is currently enabled.
	// If nil, task is always enabled.
	IsEnabled func(ctx context.Context) bool

	// Run executes the task.
	Run func(ctx context.Context, logger *zerolog.Logger) error

	// OnError is called when Run returns an error.
	// If nil, errors are only logged.
	OnError func(err error)

	// lastRun tracks when the task last executed successfully.
	lastRun time.Time
}

// WeeklyScheduler manages a collection of weekly tasks.
type WeeklyScheduler struct {
	tasks  []*WeeklyTask
	logger *zerolog.Logger
}

// NewWeeklyScheduler creates a new weekly task scheduler.
func NewWeeklyScheduler(logger *zerolog.Logger) *WeeklyScheduler {
	return &WeeklyScheduler{
		tasks:  make([]*WeeklyTask, 0),
		logger: logger,
	}
}

// AddTask adds a task to the scheduler.
func (ws *WeeklyScheduler) AddTask(task *WeeklyTask) {
	if task.GracePeriod == 0 {
		task.GracePeriod = defaultWeeklyGracePeriod
	}

	ws.tasks = append(ws.tasks, task)
}

// CheckAndRun checks all tasks and runs any that are due.
// Call this from your main scheduler loop.
func (ws *WeeklyScheduler) CheckAndRun(ctx context.Context) {
	for _, task := range ws.tasks {
		ws.checkAndRunTask(ctx, task)
	}
}

// checkAndRunTask checks if a single task should run and executes it if so.
func (ws *WeeklyScheduler) checkAndRunTask(ctx context.Context, task *WeeklyTask) {
	// Check if enabled
	if task.IsEnabled != nil && !task.IsEnabled(ctx) {
		return
	}

	now := time.Now()

	// Check if it's the right day
	if now.Weekday() != task.Day {
		return
	}

	// Check if it's the right hour
	if now.Hour() != task.Hour {
		return
	}

	// Check grace period (not run this week)
	if !task.lastRun.IsZero() && now.Sub(task.lastRun) <= task.GracePeriod {
		return
	}

	// Run the task
	logger := ws.logger.With().Str(logFieldTask, task.Name).Logger()
	logger.Info().Msgf("Starting weekly %s", task.Name)

	if err := task.Run(ctx, &logger); err != nil {
		logger.Error().Err(err).Msgf("failed to run weekly %s", task.Name)

		if task.OnError != nil {
			task.OnError(err)
		}
	} else {
		task.lastRun = now
	}
}

// SetLastRun allows setting the last run time for a task (e.g., from persisted state).
func (ws *WeeklyScheduler) SetLastRun(taskName string, lastRun time.Time) {
	for _, task := range ws.tasks {
		if task.Name == taskName {
			task.lastRun = lastRun
			return
		}
	}
}

// GetLastRun returns the last run time for a task.
func (ws *WeeklyScheduler) GetLastRun(taskName string) (time.Time, bool) {
	for _, task := range ws.tasks {
		if task.Name == taskName {
			return task.lastRun, true
		}
	}

	return time.Time{}, false
}

// ShouldRunWeekly is a standalone helper function to check if a weekly task should run.
// This is useful for code that doesn't want to use the full WeeklyScheduler.
func ShouldRunWeekly(
	now time.Time,
	day time.Weekday,
	hour int,
	lastRun time.Time,
	gracePeriod time.Duration,
) bool {
	if now.Weekday() != day {
		return false
	}

	if now.Hour() != hour {
		return false
	}

	if gracePeriod == 0 {
		gracePeriod = defaultWeeklyGracePeriod
	}

	if !lastRun.IsZero() && now.Sub(lastRun) <= gracePeriod {
		return false
	}

	return true
}

// ShouldRunSundayMidnight is a convenience function for the common pattern
// of running tasks on Sunday at midnight with 6-day grace period.
func ShouldRunSundayMidnight(now time.Time, lastRun time.Time) bool {
	return ShouldRunWeekly(now, time.Sunday, 0, lastRun, defaultWeeklyGracePeriod)
}
