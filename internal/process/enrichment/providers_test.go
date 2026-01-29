package enrichment

import (
	"context"
	"errors"
	"testing"
	"time"
)

var errTestProvider = errors.New("provider error")

// mockProvider is a test implementation of Provider.
type mockProvider struct {
	name        ProviderName
	available   bool
	results     []SearchResult
	err         error
	searchCalls int
}

func (m *mockProvider) Name() ProviderName {
	return m.name
}

func (m *mockProvider) Search(_ context.Context, _ string, _ int) ([]SearchResult, error) {
	m.searchCalls++
	return m.results, m.err
}

func (m *mockProvider) IsAvailable(_ context.Context) bool {
	return m.available
}

func (m *mockProvider) Priority() int {
	return 0
}

func TestProviderRegistry_Register(t *testing.T) {
	registry := NewProviderRegistry(defaultCircuitBreakerResetAfter)

	mock := &mockProvider{name: ProviderSolr, available: true}
	registry.Register(mock)

	if len(registry.providers) != 1 {
		t.Errorf("providers count: got %d, want 1", len(registry.providers))
	}

	if len(registry.order) != 1 {
		t.Errorf("order count: got %d, want 1", len(registry.order))
	}

	if registry.circuitBreakers[ProviderSolr] == nil {
		t.Error("circuit breaker not created")
	}
}

func TestProviderRegistry_Get(t *testing.T) {
	registry := NewProviderRegistry(defaultCircuitBreakerResetAfter)
	mock := &mockProvider{name: ProviderSolr, available: true}
	registry.Register(mock)

	p, err := registry.Get(ProviderSolr)
	if err != nil {
		t.Fatalf("Get(Solr): %v", err)
	}

	if p == nil {
		t.Error("expected provider, got nil")
	}

	_, err = registry.Get(ProviderGDELT)
	if !errors.Is(err, errProviderNotFound) {
		t.Errorf("Get(GDELT): got %v, want errProviderNotFound", err)
	}
}

func TestProviderRegistry_SearchWithFallback_FirstProvider(t *testing.T) {
	ctx := context.Background()
	registry := NewProviderRegistry(defaultCircuitBreakerResetAfter)
	mock := &mockProvider{
		name:      ProviderSolr,
		available: true,
		results:   []SearchResult{{URL: "http://example.com", Title: "Test"}},
	}
	registry.Register(mock)

	results, provider, err := registry.SearchWithFallback(ctx, testQueryFull, "", 5)
	if err != nil {
		t.Fatalf("first provider search failed: %v", err)
	}

	if provider != ProviderSolr {
		t.Errorf("provider: got %v, want Solr", provider)
	}

	if len(results) != 1 {
		t.Errorf("first provider results: got %d, want 1", len(results))
	}
}

func TestProviderRegistry_SearchWithFallback_Fallback(t *testing.T) {
	ctx := context.Background()
	registry := NewProviderRegistry(defaultCircuitBreakerResetAfter)

	failing := &mockProvider{
		name:      ProviderSolr,
		available: true,
		err:       errTestProvider,
	}
	registry.Register(failing)

	working := &mockProvider{
		name:      ProviderGDELT,
		available: true,
		results:   []SearchResult{{URL: "http://example.com"}},
	}
	registry.Register(working)

	results, provider, err := registry.SearchWithFallback(ctx, "fallback query", "", 5)
	if err != nil {
		t.Fatalf("fallback search failed: %v", err)
	}

	if provider != ProviderGDELT {
		t.Errorf("fallback provider: got %v, want GDELT", provider)
	}

	if len(results) != 1 {
		t.Errorf("fallback results: got %d, want 1", len(results))
	}
}

func TestProviderRegistry_SearchWithFallback_SkipsUnavailable(t *testing.T) {
	ctx := context.Background()
	registry := NewProviderRegistry(defaultCircuitBreakerResetAfter)

	unavailable := &mockProvider{
		name:      ProviderSolr,
		available: false,
	}
	registry.Register(unavailable)

	available := &mockProvider{
		name:      ProviderGDELT,
		available: true,
		results:   []SearchResult{{URL: "http://example.com"}},
	}
	registry.Register(available)

	_, provider, err := registry.SearchWithFallback(ctx, "skip query", "", 5)
	if err != nil {
		t.Fatalf("skip unavailable search failed: %v", err)
	}

	if provider != ProviderGDELT {
		t.Errorf("skipped provider: got %v, want GDELT", provider)
	}

	if unavailable.searchCalls != 0 {
		t.Error("unavailable provider should not be called")
	}
}

func TestProviderRegistry_SearchWithFallback_NoProviders(t *testing.T) {
	ctx := context.Background()
	registry := NewProviderRegistry(defaultCircuitBreakerResetAfter)

	_, _, err := registry.SearchWithFallback(ctx, "empty query", "", 5)
	if !errors.Is(err, errNoProvidersAvailable) {
		t.Errorf("error: got %v, want errNoProvidersAvailable", err)
	}
}

type mockLanguageProvider struct {
	mockProvider
	lastLanguage string
}

func (m *mockLanguageProvider) SearchWithLanguage(ctx context.Context, query, language string, maxResults int) ([]SearchResult, error) {
	m.lastLanguage = language
	return m.Search(ctx, query, maxResults)
}

func TestProviderRegistry_SearchWithFallback_UsesLanguageProvider(t *testing.T) {
	ctx := context.Background()
	registry := NewProviderRegistry(defaultCircuitBreakerResetAfter)
	mock := &mockLanguageProvider{
		mockProvider: mockProvider{
			name:      ProviderSolr,
			available: true,
			results:   []SearchResult{{URL: "http://example.com"}},
		},
	}

	registry.Register(mock)

	_, _, err := registry.SearchWithFallback(ctx, "test query", "ru", 5)
	if err != nil {
		t.Fatalf("language provider search failed: %v", err)
	}

	if mock.lastLanguage != "ru" {
		t.Errorf("language: got %q, want %q", mock.lastLanguage, "ru")
	}
}

func TestProviderRegistry_AvailableProviders(t *testing.T) {
	registry := NewProviderRegistry(defaultCircuitBreakerResetAfter)

	available := &mockProvider{name: ProviderSolr, available: true}
	unavailable := &mockProvider{name: ProviderGDELT, available: false}

	registry.Register(available)
	registry.Register(unavailable)

	providers := registry.AvailableProviders(context.Background())
	if len(providers) != 1 {
		t.Errorf("available count: got %d, want 1", len(providers))
	}

	if providers[0] != ProviderSolr {
		t.Errorf("provider[0]: got %v, want Solr", providers[0])
	}
}

func TestCircuitBreaker_canAttempt(t *testing.T) {
	t.Run("closed circuit allows attempts", func(t *testing.T) {
		cb := newCircuitBreaker(defaultCircuitBreakerResetAfter)
		if !cb.canAttempt() {
			t.Error("closed circuit should allow attempts")
		}
	})

	t.Run("open circuit blocks attempts", func(t *testing.T) {
		cb := newCircuitBreaker(defaultCircuitBreakerResetAfter)
		cb.state = circuitOpen
		cb.lastFailure = time.Now()

		if cb.canAttempt() {
			t.Error("open circuit should block attempts")
		}
	})

	t.Run("open circuit transitions to half-open after reset period", func(t *testing.T) {
		cb := newCircuitBreaker(defaultCircuitBreakerResetAfter)
		cb.state = circuitOpen
		cb.lastFailure = time.Now().Add(-6 * time.Minute) // Past reset period

		if !cb.canAttempt() {
			t.Error("should allow attempt after reset period")
		}

		if cb.state != circuitHalfOpen {
			t.Errorf("state: got %v, want circuitHalfOpen", cb.state)
		}
	})

	t.Run("half-open circuit allows attempts", func(t *testing.T) {
		cb := newCircuitBreaker(defaultCircuitBreakerResetAfter)
		cb.state = circuitHalfOpen

		if !cb.canAttempt() {
			t.Error("half-open circuit should allow attempts")
		}
	})
}

func TestCircuitBreaker_recordSuccess(t *testing.T) {
	t.Run("resets failure count", func(t *testing.T) {
		cb := newCircuitBreaker(defaultCircuitBreakerResetAfter)
		cb.failures = 2
		cb.recordSuccess()

		if cb.failures != 0 {
			t.Errorf("failures: got %d, want 0", cb.failures)
		}
	})

	t.Run("transitions half-open to closed after successes", func(t *testing.T) {
		cb := newCircuitBreaker(defaultCircuitBreakerResetAfter)
		cb.state = circuitHalfOpen

		cb.recordSuccess()

		if cb.state != circuitHalfOpen {
			t.Error("should still be half-open after 1 success")
		}

		cb.recordSuccess()

		if cb.state != circuitClosed {
			t.Errorf("state after 2 successes: got %v, want circuitClosed", cb.state)
		}
	})
}

func TestCircuitBreaker_recordFailure(t *testing.T) {
	t.Run("increments failure count", func(t *testing.T) {
		cb := newCircuitBreaker(defaultCircuitBreakerResetAfter)
		cb.recordFailure(ProviderSolr)

		if cb.failures != 1 {
			t.Errorf("failures: got %d, want 1", cb.failures)
		}
	})

	t.Run("opens circuit after threshold", func(t *testing.T) {
		cb := newCircuitBreaker(defaultCircuitBreakerResetAfter)

		for i := 0; i < circuitBreakerThreshold; i++ {
			cb.recordFailure(ProviderSolr)
		}

		if cb.state != circuitOpen {
			t.Errorf("state: got %v, want circuitOpen", cb.state)
		}
	})
}
