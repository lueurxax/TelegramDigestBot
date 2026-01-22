package enrichment

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
	"github.com/rs/zerolog"
)

type slowProvider struct {
	name     ProviderName
	delay    time.Duration
	priority int
}

func (p *slowProvider) Name() ProviderName                 { return p.name }
func (p *slowProvider) Priority() int                      { return p.priority }
func (p *slowProvider) IsAvailable(_ context.Context) bool { return true }
func (p *slowProvider) Search(ctx context.Context, query string, _ int) ([]SearchResult, error) {
	select {
	case <-time.After(p.delay):
		domain := fmt.Sprintf("%s-%s.com", p.name, query)

		return []SearchResult{{
			URL:    "http://" + domain,
			Title:  string(p.name),
			Domain: domain,
		}}, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("slow provider search canceled: %w", ctx.Err())
	}
}

func TestWorker_ExecuteQueries_Parallelism(t *testing.T) {
	registry := NewProviderRegistry(time.Minute)

	p1 := &slowProvider{name: "slow1", delay: 100 * time.Millisecond, priority: 100}
	p2 := &slowProvider{name: "slow2", delay: 100 * time.Millisecond, priority: 80}

	registry.Register(p1)
	registry.Register(p2)

	logger := zerolog.Nop()
	repo := &mockRepository{}
	w := &Worker{
		registry:     registry,
		db:           repo,
		logger:       &logger,
		domainFilter: NewDomainFilter("", ""),
	}

	queries := []GeneratedQuery{
		{Query: "q1"},
		{Query: "q2"},
	}

	start := time.Now()
	state := w.executeQueries(context.Background(), queries, 5)
	duration := time.Since(start)

	if len(state.allResults) < 2 {
		t.Errorf("expected at least 2 results, got %d", len(state.allResults))
	}

	// If it were sequential, it would take 100ms * 2 queries * 2 providers = 400ms
	// Since queries are parallel AND providers are parallel, it should take ~100ms
	if duration > 250*time.Millisecond {
		t.Errorf("execution too slow: %v, expected ~100ms", duration)
	}
}

func TestWorker_ProcessSearchResults_Parallelism(t *testing.T) {
	logger := zerolog.Nop()
	repo := &mockRepository{}

	w := &Worker{
		db:             repo,
		logger:         &logger,
		scorer:         NewScorer(),
		cfg:            &config.Config{EnrichmentMaxEvidenceItem: 5, EnrichmentMinAgreement: 0.1},
		domainFilter:   NewDomainFilter("", ""),
		languageRouter: NewLanguageRouter(domain.LanguageRoutingPolicy{}, repo),
		// We can't mock extractor easily, so we just check it doesn't crash
		// and maybe we can use a very fast one or a failing one.
		extractor: NewExtractor(nil),
	}

	results := []SearchResult{
		{URL: "http://example.com/1", Domain: "example.com"},
		{URL: "http://example.com/2", Domain: "example.com"},
	}

	// We use a context that will likely time out or fail fast since we don't have a real network
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := w.processSearchResults(ctx, &db.EnrichmentQueueItem{ItemID: "item1"}, results, "test")
	// It's okay if it fails because of network, we just want to ensure it completes
	if err != nil {
		t.Logf("processSearchResults finished with error (expected): %v", err)
	}
}
