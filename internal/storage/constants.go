package db

import (
	"time"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
)

// Discovery status constants
const (
	DiscoveryStatusAdded    = "added"
	DiscoveryStatusRejected = "rejected"
	DiscoveryStatusPending  = "pending"
)

// Link resolution status constants (aliased from domain)
const (
	LinkStatusSuccess = domain.LinkStatusSuccess
	LinkStatusFailed  = domain.LinkStatusFailed
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
	// maxConnectionRetries is the number of retries for initial connection
	maxConnectionRetries = 10
)

// Database pool default constants
const (
	defaultMaxConns          int32         = 25
	defaultMinConns          int32         = 5
	defaultMaxConnIdleTime   time.Duration = 30 * time.Minute
	defaultMaxConnLifetime   time.Duration = time.Hour
	defaultHealthCheckPeriod time.Duration = time.Minute
)
