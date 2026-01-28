package mocks

import (
	"context"
	"sync"
	"time"
)

// CacheRepository is a thread-safe in-memory implementation of ports.CacheRepository.
type CacheRepository struct {
	mu           sync.RWMutex
	translations map[string]translationEntry

	// GetTranslationFn allows overriding GetTranslation behavior.
	GetTranslationFn func(ctx context.Context, query, targetLang string) (string, error)

	// SaveTranslationFn allows overriding SaveTranslation behavior.
	SaveTranslationFn func(ctx context.Context, query, targetLang, translatedText string, ttl time.Duration) error
}

type translationEntry struct {
	text      string
	expiresAt time.Time
}

// NewCacheRepository creates a new mock cache repository.
func NewCacheRepository() *CacheRepository {
	return &CacheRepository{
		translations: make(map[string]translationEntry),
	}
}

// GetSummaryCache returns nil (not implemented in mock).
func (c *CacheRepository) GetSummaryCache(_ context.Context, _, _ string) (interface{}, error) {
	return nil, ErrCacheNotFound
}

// UpsertSummaryCache does nothing (not implemented in mock).
func (c *CacheRepository) UpsertSummaryCache(_ context.Context, _ interface{}) error {
	return nil
}

// GetTranslation retrieves a cached translation.
func (c *CacheRepository) GetTranslation(ctx context.Context, query, targetLang string) (string, error) {
	if c.GetTranslationFn != nil {
		return c.GetTranslationFn(ctx, query, targetLang)
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	key := query + ":" + targetLang
	entry, ok := c.translations[key]

	if !ok {
		return "", ErrCacheNotFound
	}

	if time.Now().After(entry.expiresAt) {
		return "", ErrCacheNotFound
	}

	return entry.text, nil
}

// SaveTranslation stores a translation in cache.
func (c *CacheRepository) SaveTranslation(ctx context.Context, query, targetLang, translatedText string, ttl time.Duration) error {
	if c.SaveTranslationFn != nil {
		return c.SaveTranslationFn(ctx, query, targetLang, translatedText, ttl)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	key := query + ":" + targetLang
	c.translations[key] = translationEntry{
		text:      translatedText,
		expiresAt: time.Now().Add(ttl),
	}

	return nil
}

// CleanupExpiredTranslations removes expired entries.
func (c *CacheRepository) CleanupExpiredTranslations(_ context.Context) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var count int64

	now := time.Now()

	for key, entry := range c.translations {
		if now.After(entry.expiresAt) {
			delete(c.translations, key)

			count++
		}
	}

	return count, nil
}

// Clear removes all cached entries.
func (c *CacheRepository) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.translations = make(map[string]translationEntry)
}
