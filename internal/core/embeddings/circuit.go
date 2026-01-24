package embeddings

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// ErrCircuitBreakerOpen indicates the circuit breaker is open.
var ErrCircuitBreakerOpen = errors.New("circuit breaker is open")

// CircuitBreaker implements the circuit breaker pattern for embedding providers.
type CircuitBreaker struct {
	threshold           int
	resetAfter          time.Duration
	consecutiveFailures int
	openUntil           time.Time
	mu                  sync.Mutex
	logger              *zerolog.Logger
}

// NewCircuitBreaker creates a new circuit breaker with the given configuration.
func NewCircuitBreaker(cfg CircuitBreakerConfig, logger *zerolog.Logger) *CircuitBreaker {
	return &CircuitBreaker{
		threshold:  cfg.Threshold,
		resetAfter: cfg.ResetAfter,
		logger:     logger,
	}
}

// CanAttempt returns true if the circuit allows an attempt.
func (cb *CircuitBreaker) CanAttempt() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	return time.Now().After(cb.openUntil)
}

// CheckCircuit returns an error if the circuit is open.
func (cb *CircuitBreaker) CheckCircuit() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if time.Now().Before(cb.openUntil) {
		return fmt.Errorf("%w until %v", ErrCircuitBreakerOpen, cb.openUntil)
	}

	return nil
}

// RecordSuccess records a successful call and resets the failure count.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveFailures = 0
}

// RecordFailure records a failed call and opens the circuit if threshold is reached.
func (cb *CircuitBreaker) RecordFailure(providerName ProviderName) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveFailures++

	if cb.consecutiveFailures >= cb.threshold {
		cb.openUntil = time.Now().Add(cb.resetAfter)

		if cb.logger != nil {
			cb.logger.Warn().
				Str("provider", string(providerName)).
				Int("consecutive_failures", cb.consecutiveFailures).
				Time("open_until", cb.openUntil).
				Msg("embedding circuit breaker opened")
		}
	}
}

// IsOpen returns true if the circuit is currently open.
func (cb *CircuitBreaker) IsOpen() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	return time.Now().Before(cb.openUntil)
}

// Reset resets the circuit breaker state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveFailures = 0
	cb.openUntil = time.Time{}
}
