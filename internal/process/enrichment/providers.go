package enrichment

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type ProviderName string

const (
	ProviderYaCy          ProviderName = "yacy"
	ProviderGDELT         ProviderName = "gdelt"
	ProviderSearxNG       ProviderName = "searxng"
	ProviderEventRegistry ProviderName = "eventregistry"
	ProviderNewsAPI       ProviderName = "newsapi"
)

var (
	errNoProvidersAvailable = errors.New("no providers available")
	errProviderNotFound     = errors.New("provider not found")
)

type SearchResult struct {
	URL         string
	Title       string
	Description string
	Domain      string
	PublishedAt time.Time
	Score       float64
}

type Provider interface {
	Name() ProviderName
	Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error)
	IsAvailable() bool
}

type ProviderRegistry struct {
	mu        sync.RWMutex
	providers map[ProviderName]Provider
	order     []ProviderName

	circuitBreakers map[ProviderName]*circuitBreaker
	cooldown        time.Duration
}

func NewProviderRegistry(cooldown time.Duration) *ProviderRegistry {
	return &ProviderRegistry{
		providers:       make(map[ProviderName]Provider),
		order:           []ProviderName{},
		circuitBreakers: make(map[ProviderName]*circuitBreaker),
		cooldown:        cooldown,
	}
}

func (r *ProviderRegistry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := p.Name()
	r.providers[name] = p
	r.order = append(r.order, name)
	r.circuitBreakers[name] = newCircuitBreaker(r.cooldown)
}

func (r *ProviderRegistry) Get(name ProviderName) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.providers[name]
	if !ok {
		return nil, errProviderNotFound
	}

	return p, nil
}

func (r *ProviderRegistry) SearchWithFallback(ctx context.Context, query string, maxResults int) ([]SearchResult, ProviderName, error) {
	r.mu.RLock()
	providers := make([]ProviderName, len(r.order))
	copy(providers, r.order)
	r.mu.RUnlock()

	var (
		lastErr   error
		attempted bool
	)

	for _, name := range providers {
		provider, err := r.Get(name)
		if err != nil {
			continue
		}

		if !provider.IsAvailable() {
			continue
		}

		cb := r.getCircuitBreaker(name)
		if !cb.canAttempt() {
			continue
		}

		attempted = true

		results, err := provider.Search(ctx, query, maxResults)
		if err != nil {
			cb.recordFailure()

			lastErr = fmt.Errorf("provider %s: %w", name, err)

			continue
		}

		cb.recordSuccess()

		return results, name, nil
	}

	if attempted && lastErr != nil {
		return nil, "", lastErr
	}

	return nil, "", errNoProvidersAvailable
}

func (r *ProviderRegistry) AvailableProviders() []ProviderName {
	r.mu.RLock()
	defer r.mu.RUnlock()

	available := []ProviderName{}

	for _, name := range r.order {
		p := r.providers[name]
		if p.IsAvailable() && r.circuitBreakers[name].canAttempt() {
			available = append(available, name)
		}
	}

	return available
}

func (r *ProviderRegistry) getCircuitBreaker(name ProviderName) *circuitBreaker {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.circuitBreakers[name]
}

const circuitBreakerThreshold = 3

const defaultCircuitBreakerResetAfter = 5 * time.Minute

type circuitBreaker struct {
	mu           sync.Mutex
	failures     int
	lastFailure  time.Time
	state        circuitState
	successCount int
	resetAfter   time.Duration
}

type circuitState int

const (
	circuitClosed circuitState = iota
	circuitOpen
	circuitHalfOpen
)

func newCircuitBreaker(resetAfter time.Duration) *circuitBreaker {
	if resetAfter <= 0 {
		resetAfter = defaultCircuitBreakerResetAfter
	}

	return &circuitBreaker{
		state:      circuitClosed,
		resetAfter: resetAfter,
	}
}

func (cb *circuitBreaker) canAttempt() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case circuitClosed:
		return true
	case circuitOpen:
		if time.Since(cb.lastFailure) > cb.resetAfter {
			cb.state = circuitHalfOpen
			cb.successCount = 0

			return true
		}

		return false
	case circuitHalfOpen:
		return true
	default:
		return false
	}
}

func (cb *circuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0

	if cb.state == circuitHalfOpen {
		cb.successCount++
		if cb.successCount >= 2 {
			cb.state = circuitClosed
		}
	}
}

func (cb *circuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailure = time.Now()

	if cb.failures >= circuitBreakerThreshold {
		cb.state = circuitOpen
	}
}
