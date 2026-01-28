// Package mocks provides test doubles for repository interfaces.
package mocks

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// SettingsStore is a thread-safe in-memory implementation of ports.SettingsStore.
type SettingsStore struct {
	mu       sync.RWMutex
	settings map[string][]byte

	// GetSettingFn allows overriding GetSetting behavior.
	GetSettingFn func(ctx context.Context, key string, target interface{}) error

	// SaveSettingWithHistoryFn allows overriding SaveSettingWithHistory behavior.
	SaveSettingWithHistoryFn func(ctx context.Context, key string, value interface{}, userID int64) error
}

// NewSettingsStore creates a new mock settings store.
func NewSettingsStore() *SettingsStore {
	return &SettingsStore{
		settings: make(map[string][]byte),
	}
}

// GetSetting retrieves a setting value.
func (s *SettingsStore) GetSetting(ctx context.Context, key string, target interface{}) error {
	if s.GetSettingFn != nil {
		return s.GetSettingFn(ctx, key, target)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	data, ok := s.settings[key]
	if !ok {
		return ErrSettingNotFound
	}

	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("unmarshal setting: %w", err)
	}

	return nil
}

// SaveSettingWithHistory saves a setting value.
func (s *SettingsStore) SaveSettingWithHistory(ctx context.Context, key string, value interface{}, userID int64) error {
	if s.SaveSettingWithHistoryFn != nil {
		return s.SaveSettingWithHistoryFn(ctx, key, value, userID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal setting: %w", err)
	}

	s.settings[key] = data

	return nil
}

// DeleteSettingWithHistory deletes a setting.
func (s *SettingsStore) DeleteSettingWithHistory(_ context.Context, key string, _ int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.settings, key)

	return nil
}

// Set is a convenience method for tests to set values directly.
func (s *SettingsStore) Set(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(value)
	if err != nil {
		return
	}

	s.settings[key] = data
}

// Clear removes all settings.
func (s *SettingsStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.settings = make(map[string][]byte)
}
