package solr

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	defaultMaxRetries   = 3
	defaultInitialDelay = 100 * time.Millisecond
	delayMultiplier     = 2
)

// RetryConfig configures retry behavior for Solr operations.
type RetryConfig struct {
	MaxRetries   int
	InitialDelay time.Duration
}

// DefaultRetryConfig returns the default retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:   defaultMaxRetries,
		InitialDelay: defaultInitialDelay,
	}
}

// AtomicUpdateWithRetry performs an atomic update with retry logic for transient failures.
// Retries on ErrVersionConflict and ErrServerError with exponential backoff.
func (c *Client) AtomicUpdateWithRetry(ctx context.Context, id string, fields map[string]interface{}, cfg RetryConfig) error {
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = defaultMaxRetries
	}

	if cfg.InitialDelay <= 0 {
		cfg.InitialDelay = defaultInitialDelay
	}

	var lastErr error

	delay := cfg.InitialDelay

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("retry interrupted: %w", ctx.Err())
			case <-time.After(delay):
				delay *= delayMultiplier
			}
		}

		lastErr = c.AtomicUpdate(ctx, id, fields)
		if lastErr == nil {
			return nil
		}

		if !isRetryableError(lastErr) {
			return lastErr
		}
	}

	return lastErr
}

// isRetryableError returns true if the error is retryable.
func isRetryableError(err error) bool {
	return errors.Is(err, ErrVersionConflict) || errors.Is(err, ErrServerError)
}
