package enrichment

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lueurxax/telegram-digest-bot/internal/platform/observability"
)

type ProviderName string

const (
	ProviderYaCy          ProviderName = "yacy"
	ProviderGDELT         ProviderName = "gdelt"
	ProviderSearxNG       ProviderName = "searxng"
	ProviderEventRegistry ProviderName = "eventregistry"
	ProviderNewsAPI       ProviderName = "newsapi"
	ProviderOpenSearch    ProviderName = "opensearch"

	PriorityHighSelfHosted = 100
	PriorityHighFree       = 90
	PriorityHighMeta       = 80
	PriorityMedium         = 70
	PriorityMediumFallback = 60
	PriorityLow            = 50
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
	IsAvailable(ctx context.Context) bool
	Priority() int
}

type ProviderRegistry struct {
	mu        sync.RWMutex
	providers map[ProviderName]Provider
	order     []ProviderName

	circuitBreakers map[ProviderName]*circuitBreaker
	cooldown        time.Duration
	gracePeriod     time.Duration
}

func NewProviderRegistry(cooldown time.Duration) *ProviderRegistry {
	return &ProviderRegistry{
		providers:       make(map[ProviderName]Provider),
		order:           []ProviderName{},
		circuitBreakers: make(map[ProviderName]*circuitBreaker),
		cooldown:        cooldown,
		gracePeriod:     0,
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

func (r *ProviderRegistry) SetGracePeriod(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if d < 0 {
		d = 0
	}

	r.gracePeriod = d
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

type fanOutResult struct {
	results  []SearchResult
	name     ProviderName
	err      error
	priority int
}

func (r *ProviderRegistry) SearchWithFallback(ctx context.Context, query string, maxResults int) ([]SearchResult, ProviderName, error) {
	activeProviders := r.getActiveProviders(ctx)
	if len(activeProviders) == 0 {
		return nil, "", errNoProvidersAvailable
	}

	resultsChan := make(chan fanOutResult, len(activeProviders))

	var wg sync.WaitGroup

	for _, p := range activeProviders {
		wg.Add(1)

		go func(provider Provider) {
			defer wg.Done()

			results, err := provider.Search(ctx, query, maxResults)
			if err != nil {
				r.getCircuitBreaker(provider.Name()).recordFailure(provider.Name())
			} else {
				r.getCircuitBreaker(provider.Name()).recordSuccess()
			}

			resultsChan <- fanOutResult{
				results:  results,
				name:     provider.Name(),
				err:      err,
				priority: provider.Priority(),
			}
		}(p)
	}

	// Close resultsChan when all goroutines are done
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	return r.selectBestResult(ctx, resultsChan)
}

func (r *ProviderRegistry) getActiveProviders(ctx context.Context) []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	active := make([]Provider, 0, len(r.providers))

	for _, p := range r.providers {
		if p.IsAvailable(ctx) && r.getCircuitBreaker(p.Name()).canAttempt() {
			active = append(active, p)
		}
	}

	return active
}

//nolint:gocyclo
func (r *ProviderRegistry) selectBestResult(ctx context.Context, resultsChan chan fanOutResult) ([]SearchResult, ProviderName, error) {
	var (
		bestResults  []SearchResult
		bestProvider ProviderName
		bestPriority = -1
		lastErr      error
		graceTimer   *time.Timer
		graceC       <-chan time.Time
	)

	for {
		select {
		case <-graceC:
			if bestResults != nil {
				return bestResults, bestProvider, nil
			}

			graceC = nil
		case <-ctx.Done():
			if bestResults != nil {
				return bestResults, bestProvider, nil
			}

			return nil, "", fmt.Errorf("search context canceled: %w", ctx.Err())
		case res, ok := <-resultsChan:
			if !ok {
				return r.handleChanClosed(bestResults, bestProvider, lastErr)
			}

			if res.err != nil {
				lastErr = res.err

				continue
			}

			if len(res.results) > 0 && res.priority > bestPriority {
				bestResults = res.results
				bestProvider = res.name
				bestPriority = res.priority

				if bestPriority >= PriorityHighSelfHosted {
					return bestResults, bestProvider, nil
				}

				if graceTimer == nil {
					graceC = r.initGraceTimer(&graceTimer)
				}
			}
		}
	}
}

func (r *ProviderRegistry) initGraceTimer(timer **time.Timer) <-chan time.Time {
	r.mu.RLock()
	grace := r.gracePeriod
	r.mu.RUnlock()

	if grace > 0 {
		*timer = time.NewTimer(grace)
		return (*timer).C
	}

	return nil
}

func (r *ProviderRegistry) handleChanClosed(bestResults []SearchResult, bestProvider ProviderName, lastErr error) ([]SearchResult, ProviderName, error) {
	if bestResults != nil {
		return bestResults, bestProvider, nil
	}

	if lastErr != nil {
		return nil, "", lastErr
	}

	return nil, "", errNoProvidersAvailable
}

func (r *ProviderRegistry) AvailableProviders(ctx context.Context) []ProviderName {
	r.mu.RLock()
	defer r.mu.RUnlock()

	available := []ProviderName{}

	for _, name := range r.order {
		p := r.providers[name]
		if p.IsAvailable(ctx) && r.circuitBreakers[name].canAttempt() {
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

func (cb *circuitBreaker) recordFailure(name ProviderName) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailure = time.Now()

	if cb.failures >= circuitBreakerThreshold {
		if cb.state != circuitOpen {
			observability.EnrichmentCircuitBreakerOpens.WithLabelValues(string(name)).Inc()
		}

		cb.state = circuitOpen
	}
}
