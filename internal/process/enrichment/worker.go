package enrichment

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pgvector/pgvector-go"
	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	linkscore "github.com/lueurxax/telegram-digest-bot/internal/core/links"
	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
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
	defaultDedupSimilarity           = 0.98
	maxLogClaimLen                   = 100
	budgetCheckInterval              = 5 * time.Minute
	domainFilterReloadInterval       = 5 * time.Minute

	// Log field keys
	logKeyItemID   = "item_id"
	logKeyQuery    = "query"
	logKeyURL      = "url"
	logKeyDeleted  = "deleted"
	logKeyLanguage = "language"

	// Settings keys for domain lists
	settingEnrichmentAllowDomains = "enrichment_allow_domains"
	settingEnrichmentDenyDomains  = "enrichment_deny_domains"
)

const (
	costPerEventRegistryRequest = 0.005   // Estimation: $5 per 1k requests
	costPerNewsAPIRequest       = 0.002   // Estimation: $2 per 1k requests
	costPerEmbeddingRequest     = 0.00002 // Estimation
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
	FindSimilarClaim(ctx context.Context, evidenceID string, embedding []float32, similarity float32) (*db.EvidenceClaim, error)
	// Budget tracking
	GetDailyEnrichmentCount(ctx context.Context) (int, error)
	GetMonthlyEnrichmentCount(ctx context.Context) (int, error)
	GetDailyEnrichmentCost(ctx context.Context) (float64, error)
	GetMonthlyEnrichmentCost(ctx context.Context) (float64, error)
	IncrementEnrichmentUsage(ctx context.Context, provider string, cost float64) error
	IncrementEmbeddingUsage(ctx context.Context, cost float64) error
	GetLinksForMessage(ctx context.Context, msgID string) ([]domain.ResolvedLink, error)
	// Settings access for domain lists
	GetSetting(ctx context.Context, key string, target interface{}) error
}

// EmbeddingClient provides embedding generation for semantic deduplication.
type EmbeddingClient interface {
	GetEmbedding(ctx context.Context, text string) ([]float32, error)
}

// TranslationClient provides query translation for non-EN/RU languages.
type TranslationClient interface {
	TranslateToEnglish(ctx context.Context, text string) (string, error)
}

type Worker struct {
	cfg               *config.Config
	db                Repository
	embeddingClient   EmbeddingClient
	translationClient TranslationClient
	registry          *ProviderRegistry
	extractor         *Extractor
	scorer            *Scorer
	queryGenerator    *QueryGenerator
	domainFilter      *DomainFilter
	lastDomainReload  time.Time
	logger            *zerolog.Logger
}

func NewWorker(cfg *config.Config, database Repository, embeddingClient EmbeddingClient, logger *zerolog.Logger) *Worker {
	registry := NewProviderRegistry(cfg.EnrichmentProviderCooldown)
	registerProviders(cfg, registry)

	extractor := NewExtractor(logger)
	// The actual wiring of LLM client happens in app.go.

	return &Worker{
		cfg:             cfg,
		db:              database,
		embeddingClient: embeddingClient,
		registry:        registry,
		extractor:       extractor,
		scorer:          NewScorer(),
		queryGenerator:  NewQueryGenerator(),
		domainFilter:    NewDomainFilter(cfg.EnrichmentAllowlistDomains, cfg.EnrichmentDenylistDomains),
		logger:          logger,
	}
}

// SetTranslationClient sets the translation client for query translation.
func (w *Worker) SetTranslationClient(client TranslationClient) {
	w.translationClient = client
}

// EnableLLMExtraction enables optional LLM claim extraction.
func (w *Worker) EnableLLMExtraction(client llm.Client, model string) {
	w.extractor.SetLLMClient(client, model)
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

	return w.runLoop(ctx)
}

func (w *Worker) runLoop(ctx context.Context) error {
	pollInterval := w.parsePollInterval()
	lastCleanup := time.Now()
	lastBudgetCheck := time.Time{}

	// Initial domain filter reload from settings
	w.reloadDomainFilter(ctx)

	for {
		paused, err := w.handleBudget(ctx, &lastBudgetCheck)
		if err != nil {
			return err
		}

		if paused {
			continue
		}

		// Reload domain filter periodically
		if time.Since(w.lastDomainReload) >= domainFilterReloadInterval {
			w.reloadDomainFilter(ctx)
		}

		w.processNextItem(ctx)

		if time.Since(lastCleanup) >= defaultEnrichmentCleanupInterval {
			w.cleanupCache(ctx)

			lastCleanup = time.Now()
		}

		if err := w.wait(ctx, pollInterval); err != nil {
			return err
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

func (w *Worker) handleBudget(ctx context.Context, lastBudgetCheck *time.Time) (bool, error) {
	if !w.shouldCheckBudget(*lastBudgetCheck) {
		return false, nil
	}

	exceeded, reason := w.checkBudgetLimits(ctx)
	if !exceeded {
		*lastBudgetCheck = time.Now()
		return false, nil
	}

	w.logger.Warn().Str("reason", reason).Msg("budget limit exceeded, pausing enrichment")

	*lastBudgetCheck = time.Now()

	if err := w.wait(ctx, budgetCheckInterval); err != nil {
		return true, err
	}

	return true, nil
}

func (w *Worker) processNextItem(ctx context.Context) {
	item, err := w.db.ClaimNextEnrichment(ctx)
	if err != nil {
		w.logger.Error().Err(err).Msg("failed to claim enrichment item")
		return
	}

	if item != nil {
		w.processItem(ctx, item)
	}
}

func (w *Worker) wait(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck
	case <-time.After(d):
		return nil
	}
}

func (w *Worker) processItem(ctx context.Context, item *db.EnrichmentQueueItem) {
	itemCtx, cancel := context.WithTimeout(ctx, w.getItemTimeout())
	defer cancel()

	if err := w.processWithProviders(itemCtx, item); err != nil {
		w.handleError(ctx, item, err)
		return
	}

	w.updateStatus(ctx, item.ID, db.EnrichmentStatusDone, "", nil)
}

// searchState tracks the state of search execution across multiple queries.
type searchState struct {
	mu           sync.Mutex
	allResults   []SearchResult
	seenURLs     map[string]bool
	lastProvider ProviderName
	lastErr      error
}

func (w *Worker) processWithProviders(ctx context.Context, item *db.EnrichmentQueueItem) error {
	maxResults := w.getMaxResults()

	var resolvedLinks []domain.ResolvedLink

	if item.RawMessageID != "" && w.cfg.LinkEnrichmentEnabled && strings.Contains(w.cfg.LinkEnrichmentScope, domain.ScopeQueries) {
		var err error

		resolvedLinks, err = w.db.GetLinksForMessage(ctx, item.RawMessageID)
		if err != nil {
			w.logger.Warn().Err(err).Str(logKeyItemID, item.ItemID).Msg("failed to fetch links for query generation")
		}
	}

	resolvedLinks = w.filterLinksForQueries(item, resolvedLinks)
	queries := w.generateQueries(item, resolvedLinks)

	// Translate queries if enabled and language is not EN/RU
	queries = w.translateQueriesIfNeeded(ctx, queries)

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

func (w *Worker) generateQueries(item *db.EnrichmentQueueItem, links []domain.ResolvedLink) []GeneratedQuery {
	queries := w.queryGenerator.Generate(item.Summary, item.Topic, item.ChannelTitle, links)
	if len(queries) == 0 {
		lang := w.queryGenerator.DetectLanguage(item.Summary)

		query := item.Summary
		if item.ChannelTitle != "" {
			query = item.ChannelTitle + " " + item.Summary
		}

		return []GeneratedQuery{{Query: TruncateQuery(query), Strategy: "fallback", Language: lang}}
	}

	return queries
}

func (w *Worker) filterLinksForQueries(item *db.EnrichmentQueueItem, links []domain.ResolvedLink) []domain.ResolvedLink {
	if len(links) == 0 {
		return links
	}

	msgLang := linkscore.DetectLanguage(item.Summary)
	filtered := make([]domain.ResolvedLink, 0, len(links))

	for _, link := range links {
		if len(strings.Fields(link.Content)) < w.cfg.LinkMinWords {
			continue
		}

		if msgLang != "" && link.Language != "" && msgLang != link.Language {
			continue
		}

		filtered = append(filtered, link)
	}

	return filtered
}

// translateQueriesIfNeeded translates queries if translation is enabled and language is not EN/RU.
func (w *Worker) translateQueriesIfNeeded(ctx context.Context, queries []GeneratedQuery) []GeneratedQuery {
	if !w.cfg.EnrichmentQueryTranslate || w.translationClient == nil {
		return queries
	}

	// Capacity for original + potentially translated queries
	result := make([]GeneratedQuery, 0, len(queries)+len(queries))

	for _, q := range queries {
		// Always include original query
		result = append(result, q)

		// For non-EN queries, also add English translation
		if !isEnglish(q.Language) {
			translated, err := w.translationClient.TranslateToEnglish(ctx, q.Query)
			if err != nil {
				w.logger.Debug().
					Err(err).
					Str("query", q.Query).
					Str(logKeyLanguage, q.Language).
					Msg("failed to translate query")

				continue
			}

			if translated != "" && translated != q.Query {
				result = append(result, GeneratedQuery{
					Query:    translated,
					Strategy: q.Strategy + "_translated",
					Language: "en",
				})
			}
		}
	}

	return result
}

func (w *Worker) executeQueries(ctx context.Context, queries []GeneratedQuery, maxResults int) *searchState {
	state := &searchState{
		allResults: make([]SearchResult, 0),
		seenURLs:   make(map[string]bool),
	}

	var wg sync.WaitGroup

	for _, gq := range queries {
		if ctx.Err() != nil {
			break
		}

		wg.Add(1)

		go func(q GeneratedQuery) {
			defer wg.Done()

			w.executeQuery(ctx, q, maxResults, state)
		}(gq)
	}

	wg.Wait()

	return state
}

func (w *Worker) executeQuery(ctx context.Context, gq GeneratedQuery, maxResults int, state *searchState) {
	start := time.Now()
	results, provider, err := w.registry.SearchWithFallback(ctx, gq.Query, maxResults)

	state.mu.Lock()
	state.lastProvider = provider
	state.mu.Unlock()

	observability.EnrichmentRequestDuration.WithLabelValues(string(provider)).Observe(time.Since(start).Seconds())

	if err != nil {
		observability.EnrichmentRequests.WithLabelValues("", "error").Inc()

		state.mu.Lock()
		state.lastErr = err
		state.mu.Unlock()

		w.logger.Debug().Err(err).Str(logKeyQuery, gq.Query).Msg("query failed")

		return
	}

	observability.EnrichmentRequests.WithLabelValues(string(provider), "success").Inc()

	// Track usage for budget controls
	w.trackUsage(ctx, provider)

	w.collectResults(results, state)
}

func (w *Worker) collectResults(results []SearchResult, state *searchState) {
	state.mu.Lock()
	defer state.mu.Unlock()

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

func registerProviders(cfg *config.Config, registry *ProviderRegistry) {
	for _, name := range providerOrder(cfg.EnrichmentProviders) {
		registerProvider(cfg, registry, name)
	}
}

func registerProvider(cfg *config.Config, registry *ProviderRegistry, name ProviderName) {
	switch name {
	case ProviderYaCy:
		registerYaCy(cfg, registry)
	case ProviderGDELT:
		registerGDELT(cfg, registry)
	case ProviderSearxNG:
		registerSearxNG(cfg, registry)
	case ProviderEventRegistry:
		registerEventRegistry(cfg, registry)
	case ProviderNewsAPI:
		registerNewsAPI(cfg, registry)
	case ProviderOpenSearch:
		registerOpenSearch(cfg, registry)
	}
}

func registerYaCy(cfg *config.Config, registry *ProviderRegistry) {
	if cfg.YaCyEnabled && cfg.YaCyBaseURL != "" {
		yacy := NewYaCyProvider(YaCyConfig{
			Enabled:  true,
			BaseURL:  cfg.YaCyBaseURL,
			Timeout:  cfg.YaCyTimeout,
			Username: cfg.YaCyUser,
			Password: cfg.YaCyPassword,
		})
		registry.Register(yacy)
	}
}

func registerGDELT(cfg *config.Config, registry *ProviderRegistry) {
	if cfg.GDELTEnabled {
		gdelt := NewGDELTProvider(GDELTConfig{
			Enabled:        true,
			RequestsPerMin: cfg.GDELTRequestsPerMin,
			Timeout:        cfg.GDELTTimeout,
		})
		registry.Register(gdelt)
	}
}

func registerSearxNG(cfg *config.Config, registry *ProviderRegistry) {
	if cfg.SearxNGEnabled && cfg.SearxNGBaseURL != "" {
		searxng := NewSearxNGProvider(SearxNGConfig{
			Enabled: true,
			BaseURL: cfg.SearxNGBaseURL,
			Timeout: cfg.SearxNGTimeout,
			Engines: parseEngineList(cfg.SearxNGEngines),
		})
		registry.Register(searxng)
	}
}

func registerEventRegistry(cfg *config.Config, registry *ProviderRegistry) {
	if cfg.EventRegistryEnabled && cfg.EventRegistryAPIKey != "" {
		er := NewEventRegistryProvider(EventRegistryConfig{
			Enabled:        true,
			APIKey:         cfg.EventRegistryAPIKey,
			RequestsPerMin: cfg.EventRegistryRequestsPerMin,
			Timeout:        cfg.EventRegistryTimeout,
		})
		registry.Register(er)
	}
}

func registerNewsAPI(cfg *config.Config, registry *ProviderRegistry) {
	if cfg.NewsAPIEnabled && cfg.NewsAPIKey != "" {
		newsapi := NewNewsAPIProvider(NewsAPIConfig{
			Enabled:        true,
			APIKey:         cfg.NewsAPIKey,
			RequestsPerMin: cfg.NewsAPIRequestsPerMin,
			Timeout:        cfg.NewsAPITimeout,
		})
		registry.Register(newsapi)
	}
}

func registerOpenSearch(cfg *config.Config, registry *ProviderRegistry) {
	if cfg.OpenSearchEnabled && cfg.OpenSearchBaseURL != "" {
		opensearch := NewOpenSearchProvider(OpenSearchConfig{
			Enabled:        true,
			BaseURL:        cfg.OpenSearchBaseURL,
			Index:          cfg.OpenSearchIndex,
			RequestsPerMin: cfg.OpenSearchRequestsPerMin,
			Timeout:        cfg.OpenSearchTimeout,
		})
		registry.Register(opensearch)
	}
}

// defaultProviderOrder is the fallback order per the proposal:
// YaCy → GDELT → Event Registry → NewsAPI → SearxNG → OpenSearch
var defaultProviderOrder = []ProviderName{
	ProviderYaCy,
	ProviderGDELT,
	ProviderEventRegistry,
	ProviderNewsAPI,
	ProviderSearxNG,
	ProviderOpenSearch,
}

func providerOrder(raw string) []ProviderName {
	if strings.TrimSpace(raw) == "" {
		return defaultProviderOrder
	}

	seen := make(map[ProviderName]bool)
	order := []ProviderName{}

	for _, entry := range strings.Split(raw, ",") {
		name := ProviderName(strings.TrimSpace(strings.ToLower(entry)))
		if name == "" {
			continue
		}

		switch name {
		case ProviderYaCy, ProviderGDELT, ProviderSearxNG, ProviderEventRegistry, ProviderNewsAPI, ProviderOpenSearch:
			if seen[name] {
				continue
			}

			seen[name] = true
			order = append(order, name)
		}
	}

	if len(order) == 0 {
		return defaultProviderOrder
	}

	return order
}

// parseEngineList parses a comma-separated list of search engines.
func parseEngineList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	engines := []string{}

	for _, engine := range strings.Split(raw, ",") {
		engine = strings.TrimSpace(engine)
		if engine != "" {
			engines = append(engines, engine)
		}
	}

	return engines
}

func (w *Worker) getItemTimeout() time.Duration {
	if w.cfg.EnrichmentMaxSeconds > 0 {
		return time.Duration(w.cfg.EnrichmentMaxSeconds) * time.Second
	}

	return defaultItemTimeout
}

func (w *Worker) handleNoResults(itemID string, lastErr error) error {
	if lastErr != nil {
		return fmt.Errorf("search providers: %w", lastErr)
	}

	w.logger.Info().Str(logKeyItemID, itemID).Msg("no search results found")

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

	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)

	for i, result := range results {
		if ctx.Err() != nil {
			break
		}

		// Limit to processing at most maxEvidence * 2 results to find enough high-quality matches
		if i >= maxEvidence*2 {
			break
		}

		wg.Add(1)

		go func(res SearchResult) {
			defer wg.Done()

			score, ok := w.processSingleResult(ctx, item, res, provider, cacheTTL, minAgreement)
			if !ok {
				return
			}

			mu.Lock()
			defer mu.Unlock()

			if sourceCount >= maxEvidence {
				return
			}

			scores = append(scores, score)
			sourceCount++

			observability.EnrichmentMatches.Inc()
			observability.EnrichmentCorroborationScore.Observe(float64(score))
		}(result)
	}

	wg.Wait()

	if sourceCount > 0 {
		avgScore := w.scorer.CalculateOverallScore(scores)
		tier := w.scorer.DetermineTier(sourceCount, avgScore)

		if err := w.db.UpdateItemFactCheckScore(ctx, item.ItemID, avgScore, tier, ""); err != nil {
			w.logger.Warn().Err(err).Msg("failed to update item fact check score")
		}
	}

	return nil
}

func (w *Worker) processSingleResult(
	ctx context.Context,
	item *db.EnrichmentQueueItem,
	result SearchResult,
	provider ProviderName,
	cacheTTL time.Duration,
	minAgreement float32,
) (float32, bool) {
	evidence, err := w.processEvidenceSource(ctx, result, provider, cacheTTL)
	if err != nil {
		w.logger.Warn().Err(err).Str(logKeyURL, result.URL).Msg("failed to process evidence source")

		return 0, false
	}

	if evidence.Source.ExtractionFailed {
		return 0, false
	}

	scoringResult := w.scorer.Score(item.Summary, evidence)

	// Skip if agreement score is below minimum threshold
	w.logger.Info().
		Str(logKeyURL, result.URL).
		Float32("score", scoringResult.AgreementScore).
		Float32("min", minAgreement).
		Int("matched_claims", len(scoringResult.MatchedClaims)).
		Msg("processed evidence source matching")

	if scoringResult.AgreementScore < minAgreement {
		return 0, false
	}

	if err := w.saveItemEvidence(ctx, item.ItemID, evidence, scoringResult); err != nil {
		w.logger.Warn().Err(err).Msg("failed to save item evidence")

		return 0, false
	}

	return scoringResult.AgreementScore, true
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

	w.saveClaimsWithDedup(ctx, sourceID, evidence.Claims)

	return evidence, nil
}

// saveClaimsWithDedup saves claims with embedding-based deduplication.
func (w *Worker) saveClaimsWithDedup(ctx context.Context, sourceID string, claims []ExtractedClaim) {
	similarity := w.cfg.EnrichmentDedupSimilarity
	if similarity <= 0 {
		similarity = defaultDedupSimilarity
	}

	for _, claim := range claims {
		embedding := w.generateClaimEmbedding(ctx, claim.Text)

		// Check for similar existing claim if embedding was generated
		if len(embedding) > 0 {
			existing, err := w.db.FindSimilarClaim(ctx, sourceID, embedding, similarity)
			if err != nil {
				w.logger.Warn().Err(err).Msg("failed to check for similar claim")
			} else if existing != nil {
				w.logger.Debug().
					Str("existing_id", existing.ID).
					Str("claim_text", truncateText(claim.Text, maxLogClaimLen)).
					Msg("skipping duplicate claim")

				continue
			}
		}

		dbClaim := &db.EvidenceClaim{
			EvidenceID:  sourceID,
			ClaimText:   claim.Text,
			EntitiesRaw: claim.EntitiesJSON(),
			Embedding:   pgvector.NewVector(embedding),
		}

		if _, err := w.db.SaveEvidenceClaim(ctx, dbClaim); err != nil {
			w.logger.Warn().Err(err).Msg("failed to save evidence claim")
		}
	}
}

// generateClaimEmbedding generates an embedding for a claim text.
// Returns nil if embedding client is not available or generation fails.
func (w *Worker) generateClaimEmbedding(ctx context.Context, text string) []float32 {
	if w.embeddingClient == nil {
		return nil
	}

	embedding, err := w.embeddingClient.GetEmbedding(ctx, text)
	if err != nil {
		w.logger.Warn().Err(err).Msg("failed to generate claim embedding")

		return nil
	}

	if err := w.db.IncrementEmbeddingUsage(ctx, costPerEmbeddingRequest); err != nil {
		w.logger.Warn().Err(err).Msg("failed to track embedding usage")
	}

	return embedding
}

func truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}

	return text[:maxLen] + "..."
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

// shouldCheckBudget returns true if enough time has passed since the last budget check.
func (w *Worker) shouldCheckBudget(lastCheck time.Time) bool {
	// If limits are not configured, skip budget checks
	if w.cfg.EnrichmentDailyLimit <= 0 && w.cfg.EnrichmentMonthlyLimit <= 0 &&
		w.cfg.EnrichmentDailyBudgetUSD <= 0 && w.cfg.EnrichmentMonthlyCapUSD <= 0 {
		return false
	}

	return time.Since(lastCheck) >= budgetCheckInterval
}

// checkBudgetLimits checks if daily or monthly limits have been exceeded.
// Returns true and a reason string if exceeded.
func (w *Worker) checkBudgetLimits(ctx context.Context) (exceeded bool, reason string) {
	if exceeded, reason := w.checkDailyLimits(ctx); exceeded {
		return true, reason
	}

	return w.checkMonthlyLimits(ctx)
}

func (w *Worker) checkDailyLimits(ctx context.Context) (exceeded bool, reason string) {
	if w.cfg.EnrichmentDailyLimit > 0 {
		daily, err := w.db.GetDailyEnrichmentCount(ctx)
		if err != nil {
			w.logger.Warn().Err(err).Msg("failed to get daily enrichment count")
		} else if daily >= w.cfg.EnrichmentDailyLimit {
			return true, fmt.Sprintf("daily request limit reached (%d/%d)", daily, w.cfg.EnrichmentDailyLimit)
		}
	}

	if w.cfg.EnrichmentDailyBudgetUSD > 0 {
		dailyCost, err := w.db.GetDailyEnrichmentCost(ctx)
		if err != nil {
			w.logger.Warn().Err(err).Msg("failed to get daily enrichment cost")
		} else if dailyCost >= w.cfg.EnrichmentDailyBudgetUSD {
			return true, fmt.Sprintf("daily budget reached ($%.2f/$%.2f)", dailyCost, w.cfg.EnrichmentDailyBudgetUSD)
		}
	}

	return false, ""
}

func (w *Worker) checkMonthlyLimits(ctx context.Context) (exceeded bool, reason string) {
	if w.cfg.EnrichmentMonthlyLimit > 0 {
		monthly, err := w.db.GetMonthlyEnrichmentCount(ctx)
		if err != nil {
			w.logger.Warn().Err(err).Msg("failed to get monthly enrichment count")
		} else if monthly >= w.cfg.EnrichmentMonthlyLimit {
			return true, fmt.Sprintf("monthly request limit reached (%d/%d)", monthly, w.cfg.EnrichmentMonthlyLimit)
		}
	}

	if w.cfg.EnrichmentMonthlyCapUSD > 0 {
		monthlyCost, err := w.db.GetMonthlyEnrichmentCost(ctx)
		if err != nil {
			w.logger.Warn().Err(err).Msg("failed to get monthly enrichment cost")
		} else if monthlyCost >= w.cfg.EnrichmentMonthlyCapUSD {
			return true, fmt.Sprintf("monthly budget cap reached ($%.2f/$%.2f)", monthlyCost, w.cfg.EnrichmentMonthlyCapUSD)
		}
	}

	return false, ""
}

// trackUsage records the enrichment request for budget tracking.
func (w *Worker) trackUsage(ctx context.Context, provider ProviderName) {
	cost := w.estimateCost(provider)

	if err := w.db.IncrementEnrichmentUsage(ctx, string(provider), cost); err != nil {
		w.logger.Warn().Err(err).Msg("failed to track enrichment usage")
	}
}

func (w *Worker) estimateCost(provider ProviderName) float64 {
	switch provider {
	case ProviderEventRegistry:
		return costPerEventRegistryRequest
	case ProviderNewsAPI:
		return costPerNewsAPIRequest
	default:
		return 0
	}
}

// reloadDomainFilter reloads domain filter settings from the database.
// Settings override config values if set.
func (w *Worker) reloadDomainFilter(ctx context.Context) {
	allowDomains := w.loadDomainSetting(ctx, settingEnrichmentAllowDomains, w.cfg.EnrichmentAllowlistDomains)
	denyDomains := w.loadDomainSetting(ctx, settingEnrichmentDenyDomains, w.cfg.EnrichmentDenylistDomains)

	w.domainFilter = NewDomainFilter(allowDomains, denyDomains)
	w.lastDomainReload = time.Now()
}

// loadDomainSetting loads a domain list from settings, falling back to config default.
func (w *Worker) loadDomainSetting(ctx context.Context, settingKey, configDefault string) string {
	var domains []string

	if err := w.db.GetSetting(ctx, settingKey, &domains); err == nil && len(domains) > 0 {
		return strings.Join(domains, ",")
	}

	return configDefault
}
