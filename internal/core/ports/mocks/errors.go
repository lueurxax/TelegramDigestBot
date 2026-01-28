package mocks

import "errors"

var (
	// ErrSettingNotFound is returned when a setting key doesn't exist.
	ErrSettingNotFound = errors.New("setting not found")

	// ErrItemNotFound is returned when an item doesn't exist.
	ErrItemNotFound = errors.New("item not found")

	// ErrMessageNotFound is returned when a message doesn't exist.
	ErrMessageNotFound = errors.New("message not found")

	// ErrCacheNotFound is returned when a cache entry doesn't exist.
	ErrCacheNotFound = errors.New("cache entry not found")
)
