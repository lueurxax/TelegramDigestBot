package db

import "time"

// Discovery status constants
const (
	DiscoveryStatusAdded    = "added"
	DiscoveryStatusRejected = "rejected"
	DiscoveryStatusPending  = "pending"
)

// Link resolution status constants
const (
	LinkStatusSuccess = "success"
	LinkStatusFailed  = "failed"
)

// Default values for channel configuration
const (
	// DefaultImportanceWeight is the default weight for channels when not explicitly set
	DefaultImportanceWeight float32 = 1.0
)

// Time duration constants
const (
	// HoursPerDay is the number of hours in a day
	HoursPerDay = 24 * time.Hour
)

// Database connection constants
const (
	// ConnectionRetrySleep is the sleep duration between connection retries
	ConnectionRetrySleep = 2 * time.Second
)
