package solr

import "errors"

// Error definitions for Solr client operations.
var (
	// ErrVersionConflict is returned when an optimistic locking update fails
	// due to a document version mismatch (HTTP 409 Conflict).
	ErrVersionConflict = errors.New("solr version conflict")

	// ErrNotFound is returned when a requested document does not exist.
	ErrNotFound = errors.New("solr document not found")

	// ErrBadRequest is returned when Solr rejects the request (HTTP 400).
	ErrBadRequest = errors.New("solr bad request")

	// ErrServerError is returned for Solr internal errors (HTTP 5xx).
	ErrServerError = errors.New("solr server error")

	// ErrClientDisabled is returned when operations are attempted on a disabled client.
	ErrClientDisabled = errors.New("solr client disabled")
)
