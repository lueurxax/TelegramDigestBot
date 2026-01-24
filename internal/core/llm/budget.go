package llm

import (
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Budget threshold percentages.
const (
	BudgetThresholdWarning  = 0.8
	BudgetThresholdCritical = 1.0
)

// Date format for daily budget reset tracking.
const dateFormatYMD = "2006-01-02"

// BudgetAlert represents an alert triggered by budget thresholds.
type BudgetAlert struct {
	Level       string // "warning" or "critical"
	DailyTokens int64
	BudgetLimit int64
	Percentage  float64
	Timestamp   time.Time
}

// BudgetTracker tracks daily LLM token usage and triggers alerts.
type BudgetTracker struct {
	mu            sync.RWMutex
	dailyTokens   int64
	dailyLimit    int64
	lastResetDate string
	warningFired  bool
	criticalFired bool
	alertCallback func(alert BudgetAlert)
	logger        *zerolog.Logger
}

// NewBudgetTracker creates a new budget tracker.
func NewBudgetTracker(dailyLimit int64, logger *zerolog.Logger) *BudgetTracker {
	return &BudgetTracker{
		dailyLimit:    dailyLimit,
		lastResetDate: time.Now().UTC().Format(dateFormatYMD),
		logger:        logger,
	}
}

// SetAlertCallback sets the callback function for budget alerts.
func (bt *BudgetTracker) SetAlertCallback(callback func(alert BudgetAlert)) {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	bt.alertCallback = callback
}

// SetDailyLimit updates the daily token budget limit.
func (bt *BudgetTracker) SetDailyLimit(limit int64) {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	bt.dailyLimit = limit
}

// GetDailyLimit returns the current daily token budget limit.
func (bt *BudgetTracker) GetDailyLimit() int64 {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	return bt.dailyLimit
}

// GetDailyUsage returns the current daily token usage.
func (bt *BudgetTracker) GetDailyUsage() int64 {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	bt.checkDateReset()

	return bt.dailyTokens
}

// RecordTokens adds tokens to the daily count and checks budget thresholds.
func (bt *BudgetTracker) RecordTokens(tokens int) {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	bt.checkDateResetLocked()

	bt.dailyTokens += int64(tokens)

	if bt.dailyLimit <= 0 || bt.alertCallback == nil {
		return
	}

	percentage := float64(bt.dailyTokens) / float64(bt.dailyLimit)

	// Check critical threshold
	if !bt.criticalFired && percentage >= BudgetThresholdCritical {
		bt.criticalFired = true
		bt.fireAlert("critical", percentage)

		return
	}

	// Check warning threshold
	if !bt.warningFired && percentage >= BudgetThresholdWarning {
		bt.warningFired = true
		bt.fireAlert("warning", percentage)
	}
}

// fireAlert sends an alert through the callback.
func (bt *BudgetTracker) fireAlert(level string, percentage float64) {
	alert := BudgetAlert{
		Level:       level,
		DailyTokens: bt.dailyTokens,
		BudgetLimit: bt.dailyLimit,
		Percentage:  percentage,
		Timestamp:   time.Now().UTC(),
	}

	if bt.logger != nil {
		bt.logger.Warn().
			Str("level", level).
			Int64("daily_tokens", bt.dailyTokens).
			Int64("budget_limit", bt.dailyLimit).
			Float64("percentage", percentage).
			Msg("LLM budget threshold reached")
	}

	// Fire callback in goroutine to avoid blocking
	go bt.alertCallback(alert)
}

// checkDateReset checks if we need to reset daily counters (non-locked version).
func (bt *BudgetTracker) checkDateReset() {
	today := time.Now().UTC().Format(dateFormatYMD)
	if bt.lastResetDate != today {
		bt.mu.RUnlock()
		bt.mu.Lock()
		bt.checkDateResetLocked()
		bt.mu.Unlock()
		bt.mu.RLock()
	}
}

// checkDateResetLocked checks if we need to reset daily counters (assumes lock held).
func (bt *BudgetTracker) checkDateResetLocked() {
	today := time.Now().UTC().Format(dateFormatYMD)
	if bt.lastResetDate != today {
		bt.dailyTokens = 0
		bt.warningFired = false
		bt.criticalFired = false
		bt.lastResetDate = today

		if bt.logger != nil {
			bt.logger.Info().
				Str("date", today).
				Msg("LLM budget tracker reset for new day")
		}
	}
}

// GetStatus returns the current budget status.
func (bt *BudgetTracker) GetStatus() (dailyTokens, dailyLimit int64, percentage float64) {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	bt.checkDateReset()

	dailyTokens = bt.dailyTokens
	dailyLimit = bt.dailyLimit

	if dailyLimit > 0 {
		percentage = float64(dailyTokens) / float64(dailyLimit)
	}

	return dailyTokens, dailyLimit, percentage
}
