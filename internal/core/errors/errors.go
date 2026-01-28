// Package errors provides centralized error definitions for the application.
// Errors are organized by domain to avoid duplication and provide consistent naming.
//
// Naming conventions:
//   - Exported errors (Err*): Use for errors that callers need to check with errors.Is
//   - Unexported errors (err*): Use for internal package errors
//   - All sentinel errors should be defined as variables, not inline errors.New calls
//   - Use fmt.Errorf with %w to wrap sentinel errors with context
package errors

import "errors"

// Circuit breaker errors.
var (
	// ErrCircuitBreakerOpen indicates the circuit breaker has tripped and requests are blocked.
	ErrCircuitBreakerOpen = errors.New("circuit breaker is open")
)

// Channel and entity resolution errors.
var (
	// ErrChannelNotFound indicates a channel could not be found.
	ErrChannelNotFound = errors.New("channel not found")

	// ErrNotAChannel indicates the entity is not a channel type.
	ErrNotAChannel = errors.New("entity is not a channel")

	// ErrMessageNotFound indicates a message could not be found.
	ErrMessageNotFound = errors.New("message not found")

	// ErrNotFound is a generic not found error.
	ErrNotFound = errors.New("not found")
)

// Client and connection errors.
var (
	// ErrClientNotInitialized indicates a client has not been initialized.
	ErrClientNotInitialized = errors.New("client not initialized")

	// ErrClientDisabled indicates a client or feature is disabled.
	ErrClientDisabled = errors.New("client disabled")
)

// Response and parsing errors.
var (
	// ErrEmptyResponse indicates an empty response was received.
	ErrEmptyResponse = errors.New("empty response")

	// ErrNoResults indicates no results were found.
	ErrNoResults = errors.New("no results")

	// ErrUnexpectedType indicates an unexpected type was encountered.
	ErrUnexpectedType = errors.New("unexpected type")
)

// Validation errors.
var (
	// ErrInvalidInput indicates invalid input was provided.
	ErrInvalidInput = errors.New("invalid input")

	// ErrInvalidID indicates an invalid identifier.
	ErrInvalidID = errors.New("invalid id")
)

// Rate limiting and throttling errors.
var (
	// ErrRateLimited indicates rate limiting was triggered.
	ErrRateLimited = errors.New("rate limited")

	// ErrTooManyRequests indicates too many requests were made.
	ErrTooManyRequests = errors.New("too many requests")
)

// Cache errors.
var (
	// ErrCacheNotFound indicates a cache entry was not found.
	ErrCacheNotFound = errors.New("cache entry not found")

	// ErrCacheExpired indicates a cache entry has expired.
	ErrCacheExpired = errors.New("cache entry expired")
)

// Is is a convenience wrapper around errors.Is.
func Is(err, target error) bool {
	return errors.Is(err, target)
}

// As is a convenience wrapper around errors.As.
func As(err error, target interface{}) bool {
	return errors.As(err, target)
}
