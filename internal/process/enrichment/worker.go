package enrichment

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/observability"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const (
	maxEnrichmentAttempts            = 3
	defaultRetryDelay                = 10 * time.Minute
	defaultEnrichmentCacheTTL        = 7 * 24 * time.Hour
	defaultEnrichmentPollInterval    = 10 * time.Second
	defaultEnrichmentCleanupInterval = 6 * time.Hour
	defaultMaxResults                = 5
	defaultItemTimeout               = 60 * time.Second
	defaultMaxEvidencePerItem        = 5

	// Log field keys
	logKeyItemID  = "item_id"
	logKeyQuery   = "query"
	logKeyURL     = "url"
	logKeyDeleted = "deleted"
)

type Repository interface {
	ClaimNextEnrichment(ctx context.Context) (*db.EnrichmentQueueItem, error)
	UpdateEnrichmentStatus(ctx context.Context, queueID, status, errMsg string, retryAt *time.Time) error
	GetEvidenceSource(ctx context.Context, urlHash string) (*db.EvidenceSource, error)
	SaveEvidenceSource(ctx context.Context, src *db.EvidenceSource) (string, error)
	SaveEvidenceClaim(ctx context.Context, claim *db.EvidenceClaim) (string, error)
	SaveItemEvidence(ctx context.Context, ie *db.ItemEvidence) error
	UpdateItemFactCheckScore(ctx context.Context, itemID string, score float32, tier, notes string) error
	DeleteExpiredEvidenceSources(ctx context.Context) (int64, error)
	CleanupExcessEvidencePerItem(ctx context.Context, maxPerItem int) (int64, error)
	DeduplicateEvidenceClaims(ctx context.Context) (int64, error)
}

type Worker struct {
	cfg            *config.Config
	db             Repository
	registry       *ProviderRegistry
	extractor      *Extractor
	scorer         *Scorer
	queryGenerator *QueryGenerator
	domainFilter   *DomainFilter
	logger         *zerolog.Logger
}

func NewWorker(cfg *config.Config, database Repository, logger *zerolog.Logger) *Worker {
	registry := NewProviderRegistry()

	if cfg.YaCyEnabled && cfg.YaCyBaseURL != "" {
		yacy := NewYaCyProvider(YaCyConfig{
			Enabled: true,
			BaseURL: cfg.YaCyBaseURL,
			Timeout: cfg.YaCyTimeout,
		})
		registry.Register(yacy)
	}

	if cfg.GDELTEnabled {
		gdelt := NewGDELTProvider(GDELTConfig{
			Enabled:        true,
			RequestsPerMin: cfg.GDELTRequestsPerMin,
			Timeout:        cfg.GDELTTimeout,
		})
		registry.Register(gdelt)
	}

	return &Worker{
		cfg:            cfg,
		db:             database,
		registry:       registry,
		extractor:      NewExtractor(),
		scorer:         NewScorer(),
		queryGenerator: NewQueryGenerator(),
		domainFilter:   NewDomainFilter(cfg.EnrichmentAllowlistDomains, cfg.EnrichmentDenylistDomains),
		logger:         logger,
	}
}

func (w *Worker) Run(ctx context.Context) error {
	if !w.cfg.EnrichmentEnabled {
		w.logger.Info().Msg("enrichment worker disabled")
		return nil
	}

	available := w.registry.AvailableProviders()
	if len(available) == 0 {
		w.logger.Warn().Msg("enrichment worker: no providers available")
		return nil
	}

	w.logger.Info().Strs("providers", providerNamesToStrings(available)).Msg("enrichment worker starting")

	pollInterval := w.parsePollInterval()
	lastCleanup := time.Now()

	for {
		item, err := w.db.ClaimNextEnrichment(ctx)
		if err != nil {
			w.logger.Error().Err(err).Msg("failed to claim enrichment item")
		} else if item != nil {
			w.processItem(ctx, item)
		}

		if time.Since(lastCleanup) >= defaultEnrichmentCleanupInterval {
			w.cleanupCache(ctx)

			lastCleanup = time.Now()
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("enrichment worker: %w", ctx.Err())
		case <-time.After(pollInterval):
		}
	}
}

func (w *Worker) parsePollInterval() time.Duration {
	pollInterval, err := time.ParseDuration(w.cfg.WorkerPollInterval)
	if err != nil {
		w.logger.Warn().Err(err).Str("interval", w.cfg.WorkerPollInterval).Msg("invalid worker poll interval, using default")
		return defaultEnrichmentPollInterval
	}

	return pollInterval
}

func (w *Worker) processItem(ctx context.Context, item *db.EnrichmentQueueItem) {
	itemCtx, cancel := context.WithTimeout(ctx, defaultItemTimeout)
	defer cancel()

	if err := w.processWithProviders(itemCtx, item); err != nil {
		w.handleError(ctx, item, err)
		return
	}

	w.updateStatus(ctx, item.ID, db.EnrichmentStatusDone, "", nil)
}

// searchState tracks the state of search execution across multiple queries.
type searchState struct {
	allResults   []SearchResult
	seenURLs     map[string]bool
	lastProvider ProviderName
	lastErr      error
}

func (w *Worker) processWithProviders(ctx context.Context, item *db.EnrichmentQueueItem) error {
	maxResults := w.getMaxResults()
	queries := w.generateQueries(item)

	w.logger.Debug().
		Str(logKeyItemID, item.ItemID).
		Int("query_count", len(queries)).
		Msg("generated search queries")

	state := w.executeQueries(ctx, queries, maxResults)

	if len(state.allResults) == 0 {
		return w.handleNoResults(item.ItemID, state.lastErr)
	}

	return w.processSearchResults(ctx, item, state.allResults, state.lastProvider)
}

func (w *Worker) getMaxResults() int {
	if w.cfg.EnrichmentMaxResults <= 0 {
		return defaultMaxResults
	}

	return w.cfg.EnrichmentMaxResults
}

func (w *Worker) generateQueries(item *db.EnrichmentQueueItem) []GeneratedQuery {
	queries := w.queryGenerator.Generate(item.Summary, item.Topic)
	if len(queries) == 0 {
		return []GeneratedQuery{{Query: item.Summary, Strategy: "fallback"}}
	}

	return queries
}

func (w *Worker) executeQueries(ctx context.Context, queries []GeneratedQuery, maxResults int) *searchState {
	state := &searchState{
		allResults: make([]SearchResult, 0),
		seenURLs:   make(map[string]bool),
	}

	for _, gq := range queries {
		if ctx.Err() != nil {
			break
		}

		w.executeQuery(ctx, gq, maxResults, state)
	}

	return state
}

func (w *Worker) executeQuery(ctx context.Context, gq GeneratedQuery, maxResults int, state *searchState) {
	start := time.Now()
	results, provider, err := w.registry.SearchWithFallback(ctx, gq.Query, maxResults)
	state.lastProvider = provider

	observability.EnrichmentRequestDuration.WithLabelValues(string(provider)).Observe(time.Since(start).Seconds())

	if err != nil {
		observability.EnrichmentRequests.WithLabelValues("", "error").Inc()

		state.lastErr = err

		w.logger.Debug().Err(err).Str(logKeyQuery, gq.Query).Msg("query failed")

		return
	}

	observability.EnrichmentRequests.WithLabelValues(string(provider), "success").Inc()

	w.collectResults(results, state)
}

func (w *Worker) collectResults(results []SearchResult, state *searchState) {
	for _, result := range results {
		if state.seenURLs[result.URL] {
			continue
		}

		if !w.domainFilter.IsAllowed(result.Domain) {
			w.logger.Debug().Str("domain", result.Domain).Msg("domain filtered out")

			continue
		}

		state.seenURLs[result.URL] = true
		state.allResults = append(state.allResults, result)
	}
}

func (w *Worker) handleNoResults(itemID string, lastErr error) error {
	if lastErr != nil {
		return fmt.Errorf("search providers: %w", lastErr)
	}

	w.logger.Debug().Str(logKeyItemID, itemID).Msg("no search results found")

	return nil
}

func (w *Worker) processSearchResults(ctx context.Context, item *db.EnrichmentQueueItem, results []SearchResult, provider ProviderName) error {
	cacheTTL := w.getEvidenceCacheTTL()
	scores := []float32{}
	sourceCount := 0

	maxEvidence := w.cfg.EnrichmentMaxEvidenceItem
	if maxEvidence <= 0 {
		maxEvidence = defaultMaxEvidencePerItem
	}

	minAgreement := w.cfg.EnrichmentMinAgreement

	for _, result := range results {
		if ctx.Err() != nil {
			break
		}

		// Limit max evidence sources per item
		if sourceCount >= maxEvidence {
			w.logger.Debug().
				Str(logKeyItemID, item.ItemID).
				Int("max", maxEvidence).
				Msg("reached max evidence per item limit")

			break
		}

		evidence, err := w.processEvidenceSource(ctx, result, provider, cacheTTL)
		if err != nil {
			w.logger.Warn().Err(err).Str(logKeyURL, result.URL).Msg("failed to process evidence source")

			continue
		}

		scoringResult := w.scorer.Score(item.Summary, evidence)

		// Skip if agreement score is below minimum threshold
		if scoringResult.AgreementScore < minAgreement {
			w.logger.Debug().
				Str(logKeyURL, result.URL).
				Float32("score", scoringResult.AgreementScore).
				Float32("min", minAgreement).
				Msg("evidence below minimum agreement threshold")

			continue
		}

		if err := w.saveItemEvidence(ctx, item.ItemID, evidence, scoringResult); err != nil {
			w.logger.Warn().Err(err).Msg("failed to save item evidence")

			continue
		}

		scores = append(scores, scoringResult.AgreementScore)
		sourceCount++

		observability.EnrichmentMatches.Inc()
	}

	if sourceCount > 0 {
		avgScore := w.scorer.CalculateOverallScore(scores)
		tier := w.scorer.DetermineTier(sourceCount, avgScore)

		if err := w.db.UpdateItemFactCheckScore(ctx, item.ItemID, avgScore, tier, ""); err != nil {
			w.logger.Warn().Err(err).Msg("failed to update item fact check score")
		}
	}

	return nil
}

func (w *Worker) processEvidenceSource(ctx context.Context, result SearchResult, provider ProviderName, cacheTTL time.Duration) (*ExtractedEvidence, error) {
	urlHash := db.URLHash(result.URL)

	cached, err := w.db.GetEvidenceSource(ctx, urlHash)
	if err != nil {
		w.logger.Warn().Err(err).Msg("evidence source cache lookup failed")
	}

	if cached != nil && time.Now().Before(cached.ExpiresAt) {
		observability.EnrichmentCacheHits.Inc()

		return &ExtractedEvidence{
			Source: cached,
			Claims: []ExtractedClaim{},
		}, nil
	}

	observability.EnrichmentCacheMisses.Inc()

	evidence, err := w.extractor.Extract(ctx, result, provider, cacheTTL)
	if err != nil {
		return nil, err
	}

	sourceID, err := w.db.SaveEvidenceSource(ctx, evidence.Source)
	if err != nil {
		return nil, fmt.Errorf("save evidence source: %w", err)
	}

	evidence.Source.ID = sourceID

	for _, claim := range evidence.Claims {
		dbClaim := &db.EvidenceClaim{
			EvidenceID:  sourceID,
			ClaimText:   claim.Text,
			EntitiesRaw: claim.EntitiesJSON(),
		}

		if _, err := w.db.SaveEvidenceClaim(ctx, dbClaim); err != nil {
			w.logger.Warn().Err(err).Msg("failed to save evidence claim")
		}
	}

	return evidence, nil
}

func (w *Worker) saveItemEvidence(ctx context.Context, itemID string, evidence *ExtractedEvidence, scoringResult ScoringResult) error {
	ie := &db.ItemEvidence{
		ItemID:            itemID,
		EvidenceID:        evidence.Source.ID,
		AgreementScore:    scoringResult.AgreementScore,
		IsContradiction:   scoringResult.IsContradiction,
		MatchedClaimsJSON: w.scorer.MarshalMatchedClaims(scoringResult.MatchedClaims),
		MatchedAt:         time.Now(),
	}

	if err := w.db.SaveItemEvidence(ctx, ie); err != nil {
		return fmt.Errorf("save item evidence: %w", err)
	}

	return nil
}

func (w *Worker) getEvidenceCacheTTL() time.Duration {
	ttl := time.Duration(w.cfg.EnrichmentCacheTTLHours) * time.Hour
	if ttl <= 0 {
		ttl = defaultEnrichmentCacheTTL
	}

	return ttl
}

func (w *Worker) handleError(ctx context.Context, item *db.EnrichmentQueueItem, err error) {
	if item.AttemptCount >= maxEnrichmentAttempts {
		w.updateStatus(ctx, item.ID, db.EnrichmentStatusError, err.Error(), nil)
		return
	}

	retryAt := time.Now().Add(defaultRetryDelay)
	w.updateStatus(ctx, item.ID, db.EnrichmentStatusPending, err.Error(), &retryAt)
}

func (w *Worker) updateStatus(ctx context.Context, queueID, status, errMsg string, retryAt *time.Time) {
	if err := w.db.UpdateEnrichmentStatus(ctx, queueID, status, errMsg, retryAt); err != nil {
		w.logger.Warn().Err(err).Msg("failed to update enrichment status")
	}
}

func (w *Worker) cleanupCache(ctx context.Context) {
	// Clean expired evidence sources
	deleted, err := w.db.DeleteExpiredEvidenceSources(ctx)
	if err != nil {
		w.logger.Warn().Err(err).Msg("failed to clean expired evidence sources")
	} else if deleted > 0 {
		w.logger.Info().Int64(logKeyDeleted, deleted).Msg("cleaned expired evidence sources")
	}

	// Clean excess evidence per item
	maxEvidence := w.cfg.EnrichmentMaxEvidenceItem
	if maxEvidence <= 0 {
		maxEvidence = defaultMaxEvidencePerItem
	}

	excessDeleted, err := w.db.CleanupExcessEvidencePerItem(ctx, maxEvidence)
	if err != nil {
		w.logger.Warn().Err(err).Msg("failed to clean excess evidence per item")
	} else if excessDeleted > 0 {
		w.logger.Info().Int64(logKeyDeleted, excessDeleted).Msg("cleaned excess evidence per item")
	}

	// Deduplicate evidence claims
	deduped, err := w.db.DeduplicateEvidenceClaims(ctx)
	if err != nil {
		w.logger.Warn().Err(err).Msg("failed to deduplicate evidence claims")
	} else if deduped > 0 {
		w.logger.Info().Int64("deduped", deduped).Msg("deduplicated evidence claims")
	}
}

func providerNamesToStrings(names []ProviderName) []string {
	strs := make([]string, len(names))
	for i, name := range names {
		strs[i] = string(name)
	}

	return strs
}
