package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pgvector/pgvector-go"
	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	linkscore "github.com/lueurxax/telegram-digest-bot/internal/core/links"
	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
	"github.com/lueurxax/telegram-digest-bot/internal/core/solr"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/observability"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const (
	maxEnrichmentAttempts            = 3
	defaultRetryDelay                = 10 * time.Minute
	defaultEnrichmentCacheTTL        = 7 * 24 * time.Hour
	defaultTranslationCacheTTL       = 24 * time.Hour
	defaultEnrichmentPollInterval    = 10 * time.Second
	defaultEnrichmentCleanupInterval = 6 * time.Hour
	// recoveryCheckInterval is how often to check for and recover stuck items.
	// More frequent than cleanup to catch stuck items quickly.
	recoveryCheckInterval       = 5 * time.Minute
	defaultMaxResults           = 5
	defaultMaxQueriesPerItem    = 5
	defaultItemTimeout          = 180 * time.Second
	defaultMaxEvidencePerItem   = 5
	defaultMaxConcurrentResults = 3
	defaultMaxConcurrentQueries = 3
	defaultDedupSimilarity      = 0.98
	// defaultDBTimeout is the independent timeout for database operations.
	// This prevents DB operations from failing when item context is near expiry.
	defaultDBTimeout = 30 * time.Second
	// resultProcessingMultiplier determines how many results to process to find enough matches.
	// We process maxEvidence * multiplier results because not all results will match
	// (some fail extraction, some have low agreement scores, some have language mismatches).
	// A multiplier of 2 assumes roughly 50% success rate.
	resultProcessingMultiplier = 2
	maxLogClaimLen             = 100
	logFieldClaimText          = "claim_text"
	budgetCheckInterval        = 5 * time.Minute
	domainFilterReloadInterval = 5 * time.Minute
	llmQuerySummaryLimit       = 400
	llmQueryTextLimit          = 800
	llmQueryLinksLimit         = 3
	// stuckProcessingThreshold is the duration after which a "processing" item
	// is considered stuck and should be recovered. Set to 2x item timeout.
	stuckProcessingThreshold = 2 * defaultItemTimeout
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
	CleanupExpiredTranslations(ctx context.Context) (int64, error)
	FindSimilarClaim(ctx context.Context, evidenceID string, embedding []float32, similarity float32) (*db.EvidenceClaim, error)
	RecoverStuckEnrichmentItems(ctx context.Context, stuckThreshold time.Duration) (int64, error)
	// Budget tracking
	GetDailyEnrichmentCount(ctx context.Context) (int, error)
	GetMonthlyEnrichmentCount(ctx context.Context) (int, error)
	GetDailyEnrichmentCost(ctx context.Context) (float64, error)
	GetMonthlyEnrichmentCost(ctx context.Context) (float64, error)
	IncrementEnrichmentUsage(ctx context.Context, provider string, cost float64) error
	IncrementEmbeddingUsage(ctx context.Context, cost float64) error
	GetLinksForMessage(ctx context.Context, msgID string) ([]domain.ResolvedLink, error)
	// Settings access
	GetSetting(ctx context.Context, key string, target interface{}) error
	// Translation cache
	GetTranslation(ctx context.Context, query, targetLang string) (string, error)
	SaveTranslation(ctx context.Context, query, targetLang, translatedText string, ttl time.Duration) error
	// History for context detection
	GetRecentMessagesForChannel(ctx context.Context, channelID string, before time.Time, limit int) ([]string, error)
	// Claims retrieval for cached sources
	GetClaimsForSource(ctx context.Context, sourceID string) ([]db.EvidenceClaim, error)
}

// EmbeddingClient provides embedding generation for semantic deduplication.
type EmbeddingClient interface {
	GetEmbedding(ctx context.Context, text string) ([]float32, error)
}

// TranslationClient provides query translation for target languages.
type TranslationClient interface {
	Translate(ctx context.Context, text string, targetLanguage string) (string, error)
}

type Worker struct {
	cfg               *config.Config
	db                Repository
	embeddingClient   EmbeddingClient
	translationClient TranslationClient
	queryLLM          llm.Client
	queryLLMModel     string
	queryExpander     *QueryExpander
	registry          *ProviderRegistry
	extractor         *Extractor
	scorer            *Scorer
	queryGenerator    *QueryGenerator
	languageRouter    *LanguageRouter
	domainFilter      *DomainFilter
	urlFilter         *URLFilter
	filterMu          sync.RWMutex // protects domainFilter, languageRouter, and urlFilter
	lastDomainReload  time.Time
	lastPolicyReload  time.Time
	logger            *zerolog.Logger
	// Solr client for updating Telegram document language (optional, nil if disabled)
	solrClient *solr.Client
}

func NewWorker(cfg *config.Config, database Repository, embeddingClient EmbeddingClient, logger *zerolog.Logger) *Worker {
	registry := NewProviderRegistry(cfg.EnrichmentProviderCooldown)
	registry.SetGracePeriod(cfg.EnrichmentProviderGrace)
	registerProviders(cfg, registry)

	extractor := NewExtractor(logger)
	// The actual wiring of LLM client happens in app.go.

	w := &Worker{
		cfg:             cfg,
		db:              database,
		embeddingClient: embeddingClient,
		registry:        registry,
		extractor:       extractor,
		scorer:          NewScorer(),
		queryGenerator:  NewQueryGenerator(),
		languageRouter:  NewLanguageRouter(domain.LanguageRoutingPolicy{Default: []string{"en"}}, database),
		domainFilter:    NewDomainFilterWithOptions(cfg.EnrichmentAllowlistDomains, cfg.EnrichmentDenylistDomains, cfg.EnrichmentSkipSocialMedia),
		urlFilter:       NewURLFilter(cfg.EnrichmentSkipNavigationPages),
		logger:          logger,
	}

	// Initialize Solr client for language updates if enabled
	if cfg.SolrEnabled && cfg.SolrBaseURL != "" {
		w.solrClient = solr.New(solr.Config{
			Enabled:    true,
			BaseURL:    cfg.SolrBaseURL,
			Timeout:    cfg.SolrTimeout,
			MaxResults: cfg.SolrMaxResults,
		})
		logger.Info().Str("solr_url", cfg.SolrBaseURL).Msg("Solr language update enabled in enrichment worker")
	}

	return w
}

// SetTranslationClient sets the translation client for query translation.
func (w *Worker) SetTranslationClient(client TranslationClient) {
	w.translationClient = client
	w.queryExpander = NewQueryExpander(client, w.db, w.logger)
}

// EnableLLMQueryGeneration enables LLM-based query generation.
func (w *Worker) EnableLLMQueryGeneration(client llm.Client, model string) {
	if client == nil {
		return
	}

	w.queryLLM = client
	w.queryLLMModel = model
}

// EnableLLMExtraction enables optional LLM claim extraction.
func (w *Worker) EnableLLMExtraction(client llm.Client, model string) {
	w.extractor.SetLLMClient(client, model)
	w.extractor.SetLLMTimeout(w.cfg.EnrichmentLLMTimeout)
}

func (w *Worker) Run(ctx context.Context) error {
	if !w.cfg.EnrichmentEnabled {
		w.logger.Info().Msg("enrichment worker disabled")
		return nil
	}

	available := w.registry.AvailableProviders(ctx)
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
	lastRecovery := time.Now()
	lastBudgetCheck := time.Time{}

	// Initial domain filter reload from settings
	w.reloadDomainFilter(ctx)
	w.reloadLanguagePolicy(ctx)

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

		// Reload language policy periodically
		if time.Since(w.lastPolicyReload) >= domainFilterReloadInterval {
			w.reloadLanguagePolicy(ctx)
		}

		// Recover stuck items more frequently than full cleanup
		if time.Since(lastRecovery) >= recoveryCheckInterval {
			w.recoverStuckItems(ctx)

			lastRecovery = time.Now()
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

	w.logger.Warn().Str(logKeyReason, reason).Msg("budget limit exceeded, pausing enrichment")

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

	// Update Telegram document language in Solr (fire-and-forget)
	w.updateTelegramLanguage(ctx, item)
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
	queries := w.generateQueries(ctx, item, resolvedLinks)

	// Route queries to target languages
	queries = w.expandQueriesWithRouting(ctx, item, queries)

	w.logGeneratedQueries(item.ItemID, queries)

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

func (w *Worker) generateQueries(ctx context.Context, item *db.EnrichmentQueueItem, links []domain.ResolvedLink) []GeneratedQuery {
	if w.cfg.EnrichmentQueryLLM && w.queryLLM != nil {
		if queries := w.generateQueriesWithLLM(ctx, item, links); len(queries) > 0 {
			return queries
		}
	}

	return w.generateQueriesHeuristic(item, links)
}

func (w *Worker) generateQueriesHeuristic(item *db.EnrichmentQueueItem, links []domain.ResolvedLink) []GeneratedQuery {
	queries := w.queryGenerator.Generate(item.Summary, item.Text, item.Topic, item.ChannelTitle, links)
	if len(queries) == 0 {
		return w.buildFallbackQuery(item)
	}

	return queries
}

func (w *Worker) buildFallbackQuery(item *db.EnrichmentQueueItem) []GeneratedQuery {
	source := strings.TrimSpace(item.Summary)

	if source == "" {
		source = strings.TrimSpace(item.Text)
	}

	if source == "" {
		return nil
	}

	lang := w.queryGenerator.DetectLanguage(source)

	query := source
	if item.ChannelTitle != "" {
		query = item.ChannelTitle + " " + source
	}

	return []GeneratedQuery{{Query: TruncateQuery(query), Strategy: "fallback", Language: lang}}
}

func (w *Worker) generateQueriesWithLLM(ctx context.Context, item *db.EnrichmentQueueItem, links []domain.ResolvedLink) []GeneratedQuery {
	if w.queryLLM == nil {
		return nil
	}

	source := strings.TrimSpace(item.Summary)

	if source == "" {
		source = strings.TrimSpace(item.Text)
	}

	if source == "" {
		return nil
	}

	prompt := w.buildLLMQueryPrompt(item, links)
	if prompt == "" {
		return nil
	}

	model := w.queryLLMModel
	if model == "" {
		model = w.cfg.LLMModel
	}

	resp, err := w.queryLLM.CompleteText(ctx, prompt, model)
	if err != nil {
		w.logger.Debug().Err(err).Str(logKeyItemID, item.ItemID).Msg("LLM query generation failed")

		return nil
	}

	rawQueries := parseLLMQueryOutput(resp)
	if len(rawQueries) == 0 {
		w.logger.Debug().Str(logKeyItemID, item.ItemID).Msg("LLM query generation returned no queries")

		return nil
	}

	fallbackLang := w.queryGenerator.DetectLanguage(source)

	return buildLLMGeneratedQueries(rawQueries, fallbackLang)
}

func (w *Worker) buildLLMQueryPrompt(item *db.EnrichmentQueueItem, links []domain.ResolvedLink) string {
	summary := strings.TrimSpace(item.Summary)
	text := strings.TrimSpace(item.Text)
	topic := strings.TrimSpace(item.Topic)
	channel := strings.TrimSpace(item.ChannelTitle)

	if summary == "" && text == "" {
		return ""
	}

	var sb strings.Builder

	sb.WriteString("Generate web search queries to corroborate the news item below.\n")
	sb.WriteString("Return a JSON array of 2-4 concise queries (3-8 words each).\n")
	sb.WriteString("Use the original language of the item.\n")
	sb.WriteString("Include key people, organizations, and locations. Avoid filler words.\n")
	sb.WriteString("Output JSON only.\n\n")

	if summary != "" {
		sb.WriteString("Summary: ")
		sb.WriteString(truncateText(summary, llmQuerySummaryLimit))
		sb.WriteString("\n")
	}

	if text != "" {
		sb.WriteString("Text: ")
		sb.WriteString(truncateText(text, llmQueryTextLimit))
		sb.WriteString("\n")
	}

	if topic != "" {
		sb.WriteString("Topic: ")
		sb.WriteString(topic)
		sb.WriteString("\n")
	}

	if channel != "" {
		sb.WriteString("Channel: ")
		sb.WriteString(channel)
		sb.WriteString("\n")
	}

	linkHints := buildLinkHints(links, llmQueryLinksLimit)
	if linkHints != "" {
		sb.WriteString("Links: ")
		sb.WriteString(linkHints)
		sb.WriteString("\n")
	}

	sb.WriteString("\nJSON array:")

	return sb.String()
}

func buildLinkHints(links []domain.ResolvedLink, limit int) string {
	if len(links) == 0 || limit <= 0 {
		return ""
	}

	hints := make([]string, 0, limit)

	for _, link := range links {
		if len(hints) >= limit {
			break
		}

		label := strings.TrimSpace(link.Title)

		if label == "" {
			label = strings.TrimSpace(link.Domain)
		}

		if label == "" {
			continue
		}

		label = strings.ReplaceAll(label, "\n", " ")
		label = strings.ReplaceAll(label, "\r", " ")
		hints = append(hints, label)
	}

	return strings.Join(hints, "; ")
}

func parseLLMQueryOutput(output string) []string {
	output = strings.TrimSpace(output)
	if output == "" {
		return nil
	}

	if queries := tryParseJSONArray(output); queries != nil {
		return queries
	}

	return parseQueriesFromLines(output)
}

func tryParseJSONArray(output string) []string {
	var queries []string
	if err := json.Unmarshal([]byte(output), &queries); err == nil {
		return queries
	}

	start := strings.Index(output, "[")
	end := strings.LastIndex(output, "]")

	if start != -1 && end > start {
		var parsed []string

		if err := json.Unmarshal([]byte(output[start:end+1]), &parsed); err == nil {
			return parsed
		}
	}

	return nil
}

func parseQueriesFromLines(output string) []string {
	var queries []string

	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		line = cleanQueryLine(line)
		if line != "" {
			queries = append(queries, line)
		}
	}

	return queries
}

func cleanQueryLine(line string) string {
	line = strings.TrimPrefix(line, "-")
	line = strings.TrimPrefix(line, "•")
	line = strings.TrimSpace(line)

	if idx := strings.Index(line, ". "); idx > 0 && idx <= 3 {
		line = strings.TrimSpace(line[idx+1:])
	}

	return line
}

func buildLLMGeneratedQueries(raw []string, fallbackLang string) []GeneratedQuery {
	seen := make(map[string]bool)
	results := make([]GeneratedQuery, 0, maxQueries)

	for _, entry := range raw {
		if len(results) >= maxQueries {
			break
		}

		query := normalizeLLMQuery(entry)
		if query == "" {
			continue
		}

		lower := strings.ToLower(query)
		if seen[lower] {
			continue
		}

		seen[lower] = true

		lang := detectLanguage(query)
		if lang == langUnknown {
			lang = fallbackLang
		}

		results = append(results, GeneratedQuery{
			Query:    query,
			Strategy: "llm",
			Language: lang,
		})
	}

	return results
}

func normalizeLLMQuery(query string) string {
	query = strings.TrimSpace(query)
	query = strings.Trim(query, "\"'`")
	query = strings.ReplaceAll(query, "\n", " ")
	query = strings.ReplaceAll(query, "\r", " ")
	query = strings.Join(strings.Fields(query), " ")
	query = strings.Trim(query, ".,;:")

	return TruncateQuery(query)
}

func (w *Worker) filterLinksForQueries(item *db.EnrichmentQueueItem, links []domain.ResolvedLink) []domain.ResolvedLink {
	if len(links) == 0 {
		return links
	}

	msgLang := linkscore.DetectLanguage(item.Summary)
	if msgLang == "" {
		msgLang = linkscore.DetectLanguage(item.Text)
	}

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

// expandQueriesWithRouting translates queries based on the routing policy.
func (w *Worker) expandQueriesWithRouting(ctx context.Context, item *db.EnrichmentQueueItem, queries []GeneratedQuery) []GeneratedQuery {
	if !w.cfg.EnrichmentQueryTranslate || w.queryExpander == nil {
		return queries
	}

	targetLangs := w.getTargetLanguages(ctx, item)
	if len(targetLangs) == 0 {
		return queries
	}

	maxQueries := w.getMaxQueriesPerItem()

	return w.queryExpander.ExpandQueries(ctx, queries, targetLangs, maxQueries)
}

func (w *Worker) getMaxQueriesPerItem() int {
	if w.cfg.EnrichmentMaxQueriesPerItem > 0 {
		return w.cfg.EnrichmentMaxQueriesPerItem
	}

	return defaultMaxQueriesPerItem
}

// logGeneratedQueries logs each generated query with its details.
func (w *Worker) logGeneratedQueries(itemID string, queries []GeneratedQuery) {
	if len(queries) == 0 {
		w.logger.Debug().
			Str(logKeyItemID, itemID).
			Msg("no queries generated")

		return
	}

	for i, q := range queries {
		w.logger.Debug().
			Str(logKeyItemID, itemID).
			Int("index", i).
			Str(logKeyQuery, q.Query).
			Str(logKeyLanguage, q.Language).
			Str("strategy", q.Strategy).
			Msg("generated query")
	}

	w.logger.Info().
		Str(logKeyItemID, itemID).
		Int("query_count", len(queries)).
		Msg("generated search queries")
}

func (w *Worker) executeQueries(ctx context.Context, queries []GeneratedQuery, maxResults int) *searchState {
	state := &searchState{
		allResults: make([]SearchResult, 0),
		seenURLs:   make(map[string]bool),
	}

	var wg sync.WaitGroup

	// Limit concurrent query execution to avoid overwhelming search providers
	sem := make(chan struct{}, defaultMaxConcurrentQueries)

	for _, gq := range queries {
		if ctx.Err() != nil {
			break
		}

		if !w.acquireSemaphore(ctx, sem) {
			break
		}

		wg.Add(1)

		go func(q GeneratedQuery) {
			defer wg.Done()
			defer func() { <-sem }()
			defer w.recoverPanic("executeQuery")

			w.executeQuery(ctx, q, maxResults, state)
		}(gq)
	}

	wg.Wait()

	return state
}

func (w *Worker) executeQuery(ctx context.Context, gq GeneratedQuery, maxResults int, state *searchState) {
	start := time.Now()
	results, provider, err := w.registry.SearchWithFallback(ctx, gq.Query, gq.Language, maxResults)

	state.mu.Lock()
	state.lastProvider = provider
	state.mu.Unlock()

	observability.EnrichmentRequestDuration.WithLabelValues(string(provider)).Observe(time.Since(start).Seconds())

	if err != nil {
		observability.EnrichmentRequests.WithLabelValues("", statusError, gq.Language).Inc()

		state.mu.Lock()
		state.lastErr = err
		state.mu.Unlock()

		w.logger.Debug().
			Err(err).
			Str(logKeyQuery, gq.Query).
			Str(logKeyLanguage, gq.Language).
			Msg("query failed")

		return
	}

	observability.EnrichmentRequests.WithLabelValues(string(provider), statusSuccess, gq.Language).Inc()

	// Track search result counts
	resultCount := float64(len(results))
	observability.EnrichmentSearchResults.WithLabelValues(string(provider)).Observe(resultCount)
	observability.EnrichmentSearchResultsTotal.WithLabelValues(string(provider)).Add(resultCount)

	if len(results) == 0 {
		observability.EnrichmentSearchZeroResults.WithLabelValues(string(provider)).Inc()
	}

	// Track usage for budget controls
	w.trackUsage(ctx, provider)

	w.collectResults(results, gq.Language, state)
}

func (w *Worker) collectResults(results []SearchResult, language string, state *searchState) {
	state.mu.Lock()
	defer state.mu.Unlock()

	for _, result := range results {
		if state.seenURLs[result.URL] {
			continue
		}

		if !w.isDomainAllowed(result.Domain) {
			w.logger.Debug().Str("domain", result.Domain).Msg("domain filtered out")

			continue
		}

		// Filter navigation/index pages that won't have useful content
		if reason := w.urlFilter.IsNavigationURL(result.URL); reason != "" {
			w.logger.Debug().
				Str(logKeyURL, result.URL).
				Str(logKeyReason, reason).
				Msg("URL filtered as navigation page")

			continue
		}

		result.Language = language
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
	case ProviderSolr:
		registerSolr(cfg, registry)
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

func registerSolr(cfg *config.Config, registry *ProviderRegistry) {
	if cfg.SolrEnabled && cfg.SolrBaseURL != "" {
		solrProvider := NewSolrProvider(SolrConfig{
			Enabled:    true,
			BaseURL:    cfg.SolrBaseURL,
			Timeout:    cfg.SolrTimeout,
			MaxResults: cfg.SolrMaxResults,
		})
		registry.Register(solrProvider)
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
// Solr → GDELT → Event Registry → NewsAPI → SearxNG → OpenSearch
var defaultProviderOrder = []ProviderName{
	ProviderSolr,
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
		case ProviderSolr, ProviderGDELT, ProviderSearxNG, ProviderEventRegistry, ProviderNewsAPI, ProviderOpenSearch:
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

var (
	errNoEvidenceExtracted       = errors.New("no evidence extracted from search results")
	errTranslationLangMismatch   = errors.New("translation result language does not match target")
	errTranslationEmpty          = errors.New("translation result is empty")
	errTranslationSameAsOriginal = errors.New("translation result is same as original")
)

func (w *Worker) processSearchResults(ctx context.Context, item *db.EnrichmentQueueItem, results []SearchResult, provider ProviderName) error {
	params := w.buildResultProcessingParams(ctx, item, provider)
	scores, sourceCount := w.processResultsConcurrently(ctx, results, params)

	if sourceCount > 0 {
		w.updateItemScore(ctx, item.ItemID, scores, sourceCount)

		return nil
	}

	// No evidence was successfully extracted - this is retryable
	w.logger.Warn().
		Str(logKeyItemID, item.ItemID).
		Int("results_count", len(results)).
		Msg("no evidence extracted from any search result")

	return errNoEvidenceExtracted
}

type resultProcessingParams struct {
	cacheTTL     time.Duration
	maxEvidence  int
	minAgreement float32
	targetLangs  []string
	provider     ProviderName
	item         *db.EnrichmentQueueItem
}

func (w *Worker) buildResultProcessingParams(ctx context.Context, item *db.EnrichmentQueueItem, provider ProviderName) resultProcessingParams {
	maxEvidence := w.cfg.EnrichmentMaxEvidenceItem
	if maxEvidence <= 0 {
		maxEvidence = defaultMaxEvidencePerItem
	}

	return resultProcessingParams{
		cacheTTL:     w.getEvidenceCacheTTL(),
		maxEvidence:  maxEvidence,
		minAgreement: w.cfg.EnrichmentMinAgreement,
		targetLangs:  w.getTargetLanguages(ctx, item),
		provider:     provider,
		item:         item,
	}
}

func (w *Worker) processResultsConcurrently(ctx context.Context, results []SearchResult, params resultProcessingParams) ([]float32, int) {
	var (
		wg          sync.WaitGroup
		mu          sync.Mutex
		scores      []float32
		sourceCount int
	)

	sem := make(chan struct{}, defaultMaxConcurrentResults)

	for i, result := range results {
		if ctx.Err() != nil || i >= params.maxEvidence*resultProcessingMultiplier {
			break
		}

		if !w.acquireSemaphore(ctx, sem) {
			break
		}

		wg.Add(1)

		go func(res SearchResult) {
			defer wg.Done()
			defer func() { <-sem }()
			defer w.recoverPanic("processSingleResult")

			score, ok := w.processSingleResult(ctx, params.item, res, params.provider, params.cacheTTL, params.minAgreement, params.targetLangs)
			if !ok {
				return
			}

			mu.Lock()
			defer mu.Unlock()

			if sourceCount >= params.maxEvidence {
				return
			}

			scores = append(scores, score)
			sourceCount++

			observability.EnrichmentMatches.Inc()
			observability.EnrichmentCorroborationScore.Observe(float64(score))
		}(result)
	}

	wg.Wait()

	return scores, sourceCount
}

func (w *Worker) acquireSemaphore(ctx context.Context, sem chan struct{}) bool {
	select {
	case sem <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

// recoverPanic logs and recovers from panics in goroutines.
// Should be called via defer at the start of goroutines.
func (w *Worker) recoverPanic(operation string) {
	if r := recover(); r != nil {
		w.logger.Error().
			Interface("panic", r).
			Str("operation", operation).
			Msg("recovered from panic in worker goroutine")
	}
}

// createDBContext creates a context with independent timeout for database operations.
// This prevents DB operations from failing when the parent item context is near expiry.
// Uses context.AfterFunc to avoid goroutine leaks while still propagating cancellation.
func (w *Worker) createDBContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultDBTimeout)

	// Propagate parent cancellation using AfterFunc (no goroutine leak)
	stop := context.AfterFunc(parent, cancel)

	return ctx, func() {
		stop()
		cancel()
	}
}

func (w *Worker) updateItemScore(ctx context.Context, itemID string, scores []float32, sourceCount int) {
	avgScore := w.scorer.CalculateOverallScore(scores)
	tier := w.scorer.DetermineTier(sourceCount, avgScore)

	// Use independent DB context for score update
	dbCtx, dbCancel := w.createDBContext(ctx)
	defer dbCancel()

	if err := w.db.UpdateItemFactCheckScore(dbCtx, itemID, avgScore, tier, ""); err != nil {
		w.logger.Warn().Err(err).Msg("failed to update item fact check score")
	}
}

func (w *Worker) processSingleResult(
	ctx context.Context,
	item *db.EnrichmentQueueItem,
	result SearchResult,
	provider ProviderName,
	cacheTTL time.Duration,
	minAgreement float32,
	targetLangs []string,
) (float32, bool) {
	evidence, err := w.processEvidenceSource(ctx, result, provider, cacheTTL)
	if err != nil {
		w.logger.Warn().Err(err).Str(logKeyURL, result.URL).Msg("failed to process evidence source")

		return 0, false
	}

	if evidence.Source.ExtractionFailed {
		return 0, false
	}

	// Get summary for scoring - translate if language mismatch
	summaryForScoring := w.getSummaryForScoring(ctx, item.Summary, evidence)

	scoringResult := w.scorer.Score(summaryForScoring, evidence)
	claimLang := linkscore.DetectLanguage(scoringResult.BestClaim)

	if w.shouldSkipForLanguageMismatch(result, evidence, claimLang, targetLangs) {
		return 0, false
	}

	w.logScoringResult(item, result, evidence, scoringResult, minAgreement, claimLang)

	if scoringResult.AgreementScore < minAgreement {
		return 0, false
	}

	if err := w.saveItemEvidence(ctx, item.ItemID, evidence, scoringResult); err != nil {
		w.logger.Warn().Err(err).Msg("failed to save item evidence")

		return 0, false
	}

	return scoringResult.AgreementScore, true
}

func (w *Worker) shouldSkipForLanguageMismatch(result SearchResult, evidence *ExtractedEvidence, claimLang string, targetLangs []string) bool {
	sourceLang := resolveSourceLanguage(evidence, claimLang)

	if len(targetLangs) > 0 {
		return w.checkTargetLanguageMismatch(result, sourceLang, targetLangs)
	}

	return w.checkResultLanguageMismatch(result, sourceLang)
}

func resolveSourceLanguage(evidence *ExtractedEvidence, claimLang string) string {
	if evidence.Source.Language != "" {
		return evidence.Source.Language
	}

	if claimLang != "" {
		return claimLang
	}

	content := strings.TrimSpace(evidence.Source.Title + " " + evidence.Source.Description + " " + evidence.Source.Content)

	return linkscore.DetectLanguage(content)
}

func (w *Worker) checkTargetLanguageMismatch(result SearchResult, sourceLang string, targetLangs []string) bool {
	if sourceLang == "" {
		w.logger.Debug().
			Str(logKeyURL, result.URL).
			Str(logKeyTargetLang, strings.Join(targetLangs, ",")).
			Msg("skipping result due to unknown source language")

		return true
	}

	if matchesAnyLanguage(sourceLang, targetLangs) {
		return false
	}

	w.logger.Debug().
		Str(logKeyURL, result.URL).
		Str(logKeyTargetLang, strings.Join(targetLangs, ",")).
		Str(logKeySourceLang, sourceLang).
		Msg("skipping result due to language mismatch with routing policy")

	return true
}

func matchesAnyLanguage(sourceLang string, targetLangs []string) bool {
	for _, target := range targetLangs {
		if languageMatches(target, sourceLang) {
			return true
		}
	}

	return false
}

func (w *Worker) checkResultLanguageMismatch(result SearchResult, sourceLang string) bool {
	if result.Language == "" || result.Language == "auto" {
		return false
	}

	if sourceLang != "" && !languageMatches(result.Language, sourceLang) {
		w.logger.Debug().
			Str(logKeyURL, result.URL).
			Str(logKeyTargetLang, result.Language).
			Str(logKeySourceLang, sourceLang).
			Msg("skipping result due to language mismatch with target")

		return true
	}

	return false
}

// getSummaryForScoring returns the item summary translated to the evidence language if needed.
// This enables cross-language comparison by ensuring both texts are in the same language.
func (w *Worker) getSummaryForScoring(ctx context.Context, summary string, evidence *ExtractedEvidence) string {
	if w.translationClient == nil {
		return summary
	}

	// Detect evidence language from claims or source
	evidenceLang := w.detectEvidenceLanguage(evidence)
	if evidenceLang == "" {
		return summary
	}

	// Detect item language
	itemLang := w.queryGenerator.DetectLanguage(summary)
	if itemLang == "" || languageMatches(itemLang, evidenceLang) {
		return summary
	}

	// Translate summary to evidence language for comparison
	translated, err := w.translateSummaryForScoring(ctx, summary, evidenceLang)
	if err != nil {
		w.logger.Debug().Err(err).
			Str(logKeyItemLang, itemLang).
			Str(logKeyEvidenceLang, evidenceLang).
			Msg("failed to translate summary for scoring, using original")

		return summary
	}

	w.logger.Debug().
		Str(logKeyItemLang, itemLang).
		Str(logKeyEvidenceLang, evidenceLang).
		Msg("translated summary for cross-language scoring")

	return translated
}

// detectEvidenceLanguage determines the language of evidence claims.
func (w *Worker) detectEvidenceLanguage(evidence *ExtractedEvidence) string {
	// First try source language
	if evidence.Source.Language != "" {
		return evidence.Source.Language
	}

	// Try to detect from claims
	for _, claim := range evidence.Claims {
		if lang := linkscore.DetectLanguage(claim.Text); lang != "" {
			return lang
		}
	}

	// Fallback to content detection
	content := strings.TrimSpace(evidence.Source.Title + " " + evidence.Source.Description)
	if content != "" {
		return linkscore.DetectLanguage(content)
	}

	return ""
}

// translateSummaryForScoring translates the summary to target language with caching.
func (w *Worker) translateSummaryForScoring(ctx context.Context, summary, targetLang string) (string, error) {
	// Check cache first
	if cached, err := w.db.GetTranslation(ctx, summary, targetLang); err == nil && cached != "" {
		// Validate cached translation language
		if err := w.validateTranslation(cached, summary, targetLang); err != nil {
			w.logger.Warn().Err(err).
				Str(logKeyTargetLang, targetLang).
				Str(logKeyTranslatedLang, linkscore.DetectLanguage(cached)).
				Msg("cached translation invalid, will re-translate")
			// Continue to re-translate
		} else {
			return cached, nil
		}
	}

	// Translate
	translated, err := w.translationClient.Translate(ctx, summary, targetLang)
	if err != nil {
		return "", fmt.Errorf(fmtErrTranslateForScore, targetLang, err)
	}

	// Validate translation result
	if err := w.validateTranslation(translated, summary, targetLang); err != nil {
		translatedLang := linkscore.DetectLanguage(translated)
		w.logger.Warn().Err(err).
			Str(logKeyTargetLang, targetLang).
			Str(logKeyTranslatedLang, translatedLang).
			Int("translated_len", len(translated)).
			Str(logKeyTranslated, truncateLogClaim(translated)).
			Msg("translation validation failed")

		return "", fmt.Errorf(fmtErrLangMismatch, errTranslationLangMismatch, translatedLang, targetLang)
	}

	w.logger.Debug().
		Str(logKeyTargetLang, targetLang).
		Str(logKeyTranslated, truncateLogClaim(translated)).
		Msg("translation successful and validated")

	// Cache the valid translation
	if err := w.db.SaveTranslation(ctx, summary, targetLang, translated, defaultTranslationCacheTTL); err != nil {
		w.logger.Warn().Err(err).Msg("failed to cache summary translation")
	}

	return translated, nil
}

// validateTranslation checks that a translation result is valid and in the target language.
func (w *Worker) validateTranslation(translated, original, targetLang string) error {
	// Check for empty translation
	if strings.TrimSpace(translated) == "" {
		return errTranslationEmpty
	}

	// Check if translation is same as original (no translation happened)
	if strings.TrimSpace(translated) == strings.TrimSpace(original) {
		return errTranslationSameAsOriginal
	}

	// Detect language of translated text
	translatedLang := linkscore.DetectLanguage(translated)

	// If we can't detect the language, accept it (might be mixed or transliterated)
	if translatedLang == "" {
		return nil
	}

	// Check if translated language matches target
	if !languageMatches(translatedLang, targetLang) {
		return fmt.Errorf(fmtErrLangMismatch, errTranslationLangMismatch, translatedLang, targetLang)
	}

	return nil
}

func (w *Worker) logScoringResult(item *db.EnrichmentQueueItem, result SearchResult, evidence *ExtractedEvidence, scoringResult ScoringResult, minAgreement float32, claimLang string) {
	itemLang := w.queryGenerator.DetectLanguage(item.Summary)
	languageMismatch := itemLang != "" && claimLang != "" && itemLang != claimLang
	matchReason := w.matchDebugReason(evidence, scoringResult, minAgreement)

	w.logger.Info().
		Str(logKeyURL, result.URL).
		Float32("score", scoringResult.AgreementScore).
		Float32("min", minAgreement).
		Str(logKeyReason, matchReason).
		Int("matched_claims", len(scoringResult.MatchedClaims)).
		Float64("jaccard", scoringResult.BestJaccard).
		Float64("entity_overlap", scoringResult.BestEntityOverlap).
		Int("entity_matches", scoringResult.BestEntityMatches).
		Int("claims", len(evidence.Claims)).
		Int("item_tokens_count", len(scoringResult.ItemTokens)).
		Int("claim_tokens_count", len(scoringResult.BestClaimTokens)).
		Strs("item_tokens", scoringResult.ItemTokens).
		Strs("claim_tokens", scoringResult.BestClaimTokens).
		Int("content_len", len(evidence.Source.Content)).
		Int("description_len", len(evidence.Source.Description)).
		Int("title_len", len(evidence.Source.Title)).
		Bool("language_mismatch", languageMismatch).
		Str("item_lang", itemLang).
		Str(logKeySourceLang, evidence.Source.Language).
		Str("claim_lang", claimLang).
		Str("claim", truncateLogClaim(scoringResult.BestClaim)).
		Msg("processed evidence source matching")
}

func (w *Worker) matchDebugReason(evidence *ExtractedEvidence, scoring ScoringResult, minAgreement float32) string {
	if evidence == nil || evidence.Source == nil {
		return "no_evidence"
	}

	if evidence.Source.ExtractionFailed {
		return "extraction_failed"
	}

	if len(evidence.Claims) == 0 {
		return "no_claims_extracted"
	}

	if scoring.BestClaim == "" {
		return "best_claim_empty"
	}

	if scoring.AgreementScore == 0 {
		return "no_overlap"
	}

	if scoring.AgreementScore < minAgreement {
		return "below_min_agreement"
	}

	return "matched"
}

func (w *Worker) processEvidenceSource(ctx context.Context, result SearchResult, provider ProviderName, cacheTTL time.Duration) (*ExtractedEvidence, error) {
	urlHash := db.URLHash(result.URL)

	cached, err := w.db.GetEvidenceSource(ctx, urlHash)
	if err != nil {
		w.logger.Warn().Err(err).Msg("evidence source cache lookup failed")
	}

	if cached != nil && time.Now().Before(cached.ExpiresAt) {
		// Load claims from database for cached sources
		claims, err := w.loadClaimsFromDB(ctx, cached.ID)
		if err != nil {
			// Failed to load claims - fall through to re-extract to avoid returning empty claims
			w.logger.Warn().Err(err).Str("source_id", cached.ID).Msg("failed to load claims for cached source, falling back to re-extract")
		} else {
			observability.EnrichmentCacheHits.Inc()

			return &ExtractedEvidence{
				Source: cached,
				Claims: claims,
			}, nil
		}
	}

	observability.EnrichmentCacheMisses.Inc()

	evidence, err := w.extractor.Extract(ctx, result, provider, cacheTTL)
	if err != nil {
		return nil, err
	}

	// Use independent DB context to avoid timeout when item context is near expiry
	dbCtx, dbCancel := w.createDBContext(ctx)
	defer dbCancel()

	sourceID, err := w.db.SaveEvidenceSource(dbCtx, evidence.Source)
	if err != nil {
		return nil, fmt.Errorf("save evidence source: %w", err)
	}

	evidence.Source.ID = sourceID

	w.saveClaimsWithDedup(dbCtx, sourceID, evidence.Claims)

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

		// Skip claims without embeddings only when embedding client is configured
		// (meaning the embedding generation failed). When no client is configured,
		// we still save claims for basic functionality.
		if w.embeddingClient != nil && len(embedding) == 0 {
			w.logger.Debug().
				Str(logFieldClaimText, truncateText(claim.Text, maxLogClaimLen)).
				Msg("skipping claim without embedding")

			continue
		}

		// Check for similar existing claim (only if we have an embedding)
		if len(embedding) > 0 {
			existing, err := w.db.FindSimilarClaim(ctx, sourceID, embedding, similarity)
			if err != nil {
				w.logger.Warn().Err(err).Msg("failed to check for similar claim")
			} else if existing != nil {
				w.logger.Debug().
					Str("existing_id", existing.ID).
					Str(logFieldClaimText, truncateText(claim.Text, maxLogClaimLen)).
					Msg("skipping duplicate claim")

				continue
			}
		}

		dbClaim := &db.EvidenceClaim{
			EvidenceID:  sourceID,
			ClaimText:   claim.Text,
			EntitiesRaw: claim.EntitiesJSON(),
		}

		// Only set embedding if we have one
		if len(embedding) > 0 {
			dbClaim.Embedding = pgvector.NewVector(embedding)
		}

		if _, err := w.db.SaveEvidenceClaim(ctx, dbClaim); err != nil {
			w.logger.Warn().Err(err).Msg("failed to save evidence claim")
		}
	}
}

// loadClaimsFromDB loads claims from the database for a cached evidence source.
func (w *Worker) loadClaimsFromDB(ctx context.Context, sourceID string) ([]ExtractedClaim, error) {
	dbClaims, err := w.db.GetClaimsForSource(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("get claims for source: %w", err)
	}

	claims := make([]ExtractedClaim, 0, len(dbClaims))
	for _, dbClaim := range dbClaims {
		claims = append(claims, ExtractedClaim{
			Text:     dbClaim.ClaimText,
			Entities: ParseEntitiesFromJSON(dbClaim.EntitiesRaw),
		})
	}

	return claims, nil
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

	// Use independent DB context to avoid timeout when item context is near expiry
	dbCtx, dbCancel := w.createDBContext(ctx)
	defer dbCancel()

	if err := w.db.SaveItemEvidence(dbCtx, ie); err != nil {
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
	// Note: recoverStuckItems is now called separately every 5 minutes
	// for faster recovery of stuck items.
	w.cleanExpiredSources(ctx)
	w.cleanExcessEvidence(ctx)
	w.deduplicateClaims(ctx)
	w.cleanExpiredTranslations(ctx)
}

func (w *Worker) recoverStuckItems(ctx context.Context) {
	recovered, err := w.db.RecoverStuckEnrichmentItems(ctx, stuckProcessingThreshold)
	if err != nil {
		w.logger.Warn().Err(err).Msg("failed to recover stuck enrichment items")
	} else if recovered > 0 {
		w.logger.Warn().Int64("recovered", recovered).Msg("recovered stuck enrichment items")
	}
}

func (w *Worker) cleanExpiredSources(ctx context.Context) {
	deleted, err := w.db.DeleteExpiredEvidenceSources(ctx)
	if err != nil {
		w.logger.Warn().Err(err).Msg("failed to clean expired evidence sources")
	} else if deleted > 0 {
		w.logger.Info().Int64(logKeyDeleted, deleted).Msg("cleaned expired evidence sources")
	}
}

func (w *Worker) cleanExcessEvidence(ctx context.Context) {
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
}

func (w *Worker) deduplicateClaims(ctx context.Context) {
	deduped, err := w.db.DeduplicateEvidenceClaims(ctx)
	if err != nil {
		w.logger.Warn().Err(err).Msg("failed to deduplicate evidence claims")
	} else if deduped > 0 {
		w.logger.Info().Int64("deduped", deduped).Msg("deduplicated evidence claims")
	}
}

func (w *Worker) cleanExpiredTranslations(ctx context.Context) {
	deletedTranslations, err := w.db.CleanupExpiredTranslations(ctx)
	if err != nil {
		w.logger.Warn().Err(err).Msg("failed to clean expired translations")
	} else if deletedTranslations > 0 {
		w.logger.Info().Int64(logKeyDeleted, deletedTranslations).Msg("cleaned expired translations")
	}
}

func providerNamesToStrings(names []ProviderName) []string {
	strs := make([]string, len(names))
	for i, name := range names {
		strs[i] = string(name)
	}

	return strs
}

func truncateLogClaim(text string) string {
	if len(text) <= maxLogClaimLen {
		return text
	}

	return text[:maxLogClaimLen] + "..."
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

	newFilter := NewDomainFilter(allowDomains, denyDomains)

	w.filterMu.Lock()
	w.domainFilter = newFilter
	w.filterMu.Unlock()

	w.lastDomainReload = time.Now()
}

func (w *Worker) reloadLanguagePolicy(ctx context.Context) {
	policy := w.loadLanguagePolicy(ctx)
	newRouter := NewLanguageRouter(policy, w.db)

	w.filterMu.Lock()
	w.languageRouter = newRouter
	w.filterMu.Unlock()

	w.lastPolicyReload = time.Now()
}

// isDomainAllowed checks if a domain is allowed with thread-safe access.
func (w *Worker) isDomainAllowed(domain string) bool {
	w.filterMu.RLock()
	defer w.filterMu.RUnlock()

	return w.domainFilter.IsAllowed(domain)
}

// getTargetLanguages gets target languages with thread-safe access.
func (w *Worker) getTargetLanguages(ctx context.Context, item *db.EnrichmentQueueItem) []string {
	w.filterMu.RLock()
	defer w.filterMu.RUnlock()

	return w.languageRouter.GetTargetLanguages(ctx, item)
}

func (w *Worker) loadLanguagePolicy(_ context.Context) domain.LanguageRoutingPolicy {
	var policy domain.LanguageRoutingPolicy

	// 1. Load from environment variable if set.
	if strings.TrimSpace(w.cfg.EnrichmentLanguagePolicy) != "" {
		if err := json.Unmarshal([]byte(w.cfg.EnrichmentLanguagePolicy), &policy); err != nil {
			w.logger.Warn().Err(err).Msg("failed to parse ENRICHMENT_LANGUAGE_POLICY from env")

			policy.Default = []string{"en"}

			return policy
		}

		if isPolicyEmpty(policy) {
			policy.Default = []string{"en"}
		}

		return policy
	}

	// 2. If we still have no policy, use default routing (English)
	if isPolicyEmpty(policy) {
		policy.Default = []string{"en"}
	}

	return policy
}

func isPolicyEmpty(p domain.LanguageRoutingPolicy) bool {
	return len(p.Default) == 0 && len(p.Channel) == 0 && len(p.Context) == 0 && len(p.Topic) == 0
}

// loadDomainSetting loads a domain list from settings, falling back to config default.
func (w *Worker) loadDomainSetting(ctx context.Context, settingKey, configDefault string) string {
	var domains []string

	if err := w.db.GetSetting(ctx, settingKey, &domains); err == nil && len(domains) > 0 {
		return strings.Join(domains, ",")
	}

	return configDefault
}

// updateTelegramLanguage updates the Telegram document language in Solr.
// This populates language-specific dynamic fields for better search relevance.
func (w *Worker) updateTelegramLanguage(_ context.Context, item *db.EnrichmentQueueItem) {
	if w.solrClient == nil {
		return
	}

	// Only update if we have valid Telegram identifiers
	if item.TGPeerID == 0 || item.TGMessageID == 0 {
		return
	}

	lang := w.detectItemLanguage(item)
	if lang == "" {
		return // Can't update without language detection
	}

	docID := solr.TelegramDocID(item.TGPeerID, item.TGMessageID)
	fields := buildLanguageFields(lang, item.ChannelTitle, item.Text)

	//nolint:contextcheck // Fire-and-forget update uses background context
	w.fireAndForgetSolrUpdate(docID, fields, lang, item.TGPeerID, item.TGMessageID)
}

// detectItemLanguage detects language from item summary or text.
func (w *Worker) detectItemLanguage(item *db.EnrichmentQueueItem) string {
	lang := linkscore.DetectLanguage(item.Summary)
	if lang == "" {
		lang = linkscore.DetectLanguage(item.Text)
	}

	return lang
}

// buildLanguageFields builds the fields map for a language update.
func buildLanguageFields(lang, title, content string) map[string]interface{} {
	fields := map[string]interface{}{
		"language": lang,
	}

	// Populate language-specific dynamic fields for better search
	switch lang {
	case "en":
		fields["title_en"] = title
		fields["content_en"] = content
	case "ru":
		fields["title_ru"] = title
		fields["content_ru"] = content
	case "el":
		fields["title_el"] = title
		fields["content_el"] = content
	case "de":
		fields["title_de"] = title
		fields["content_de"] = content
	case "fr":
		fields["title_fr"] = title
		fields["content_fr"] = content
	}

	return fields
}

// fireAndForgetSolrUpdate performs an async Solr update.
func (w *Worker) fireAndForgetSolrUpdate(docID string, fields map[string]interface{}, lang string, peerID, messageID int64) {
	solrClient := w.solrClient
	logger := w.logger
	solrTimeout := w.cfg.SolrTimeout

	go func(docID string, fields map[string]interface{}, lang string) {
		updateCtx, cancel := context.WithTimeout(context.Background(), solrTimeout)
		defer cancel()

		if err := solrClient.AtomicUpdate(updateCtx, docID, fields); err != nil {
			// Don't log ErrNotFound - the document might not be indexed yet
			if !errors.Is(err, solr.ErrNotFound) {
				logger.Warn().Err(err).
					Int64("tg_peer_id", peerID).
					Int64("tg_message_id", messageID).
					Str("language", lang).
					Msg("failed to update Telegram document language in Solr")
			}
		}
	}(docID, fields, lang)
}
