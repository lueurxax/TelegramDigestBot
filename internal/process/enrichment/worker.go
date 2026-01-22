package enrichment

import (
	"context"
	"encoding/json"
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
	defaultTranslationCacheTTL       = 24 * time.Hour
	defaultEnrichmentPollInterval    = 10 * time.Second
	defaultEnrichmentCleanupInterval = 6 * time.Hour
	defaultMaxResults                = 5
	defaultMaxQueriesPerItem         = 5
	defaultItemTimeout               = 60 * time.Second
	defaultMaxEvidencePerItem        = 5
	defaultDedupSimilarity           = 0.98
	maxLogClaimLen                   = 100
	budgetCheckInterval              = 5 * time.Minute
	domainFilterReloadInterval       = 5 * time.Minute
	llmQuerySummaryLimit             = 400
	llmQueryTextLimit                = 800
	llmQueryLinksLimit               = 3
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
	lastDomainReload  time.Time
	lastPolicyReload  time.Time
	logger            *zerolog.Logger
}

func NewWorker(cfg *config.Config, database Repository, embeddingClient EmbeddingClient, logger *zerolog.Logger) *Worker {
	registry := NewProviderRegistry(cfg.EnrichmentProviderCooldown)
	registry.SetGracePeriod(cfg.EnrichmentProviderGrace)
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
		languageRouter:  NewLanguageRouter(domain.LanguageRoutingPolicy{Default: []string{"en"}}, database),
		domainFilter:    NewDomainFilter(cfg.EnrichmentAllowlistDomains, cfg.EnrichmentDenylistDomains),
		logger:          logger,
	}
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

	targetLangs := w.languageRouter.GetTargetLanguages(ctx, item)
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

		if !w.domainFilter.IsAllowed(result.Domain) {
			w.logger.Debug().Str("domain", result.Domain).Msg("domain filtered out")

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
			Enabled:    true,
			BaseURL:    cfg.YaCyBaseURL,
			Timeout:    cfg.YaCyTimeout,
			Username:   cfg.YaCyUser,
			Password:   cfg.YaCyPassword,
			MaxResults: cfg.YaCyMaxResults,
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
	targetLangs := w.languageRouter.GetTargetLanguages(ctx, item)

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

			score, ok := w.processSingleResult(ctx, item, res, provider, cacheTTL, minAgreement, targetLangs)
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

	scoringResult := w.scorer.Score(item.Summary, evidence)
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

func (w *Worker) logScoringResult(item *db.EnrichmentQueueItem, result SearchResult, evidence *ExtractedEvidence, scoringResult ScoringResult, minAgreement float32, claimLang string) {
	itemLang := w.queryGenerator.DetectLanguage(item.Summary)
	languageMismatch := itemLang != "" && claimLang != "" && itemLang != claimLang
	matchReason := w.matchDebugReason(evidence, scoringResult, minAgreement)
	itemTokens := len(tokenize(item.Summary))
	claimTokens := len(tokenize(scoringResult.BestClaim))

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
		Int("item_tokens", itemTokens).
		Int("claim_tokens", claimTokens).
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

	// Clean expired translations
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

	w.domainFilter = NewDomainFilter(allowDomains, denyDomains)
	w.lastDomainReload = time.Now()
}

func (w *Worker) reloadLanguagePolicy(ctx context.Context) {
	policy := w.loadLanguagePolicy(ctx)
	w.languageRouter = NewLanguageRouter(policy, w.db)
	w.lastPolicyReload = time.Now()
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
