// Package app provides the main application bootstrap and runtime orchestration.
//
// The App type wires together all dependencies and exposes methods to run
// different operational modes:
//
//   - Bot mode: Admin Telegram bot for operator commands
//   - Reader mode: MTProto client that ingests messages from tracked channels
//   - Worker mode: Processing pipeline for enrichment, dedup, and fact-checking
//   - Digest mode: Scheduled digest generation and posting
//   - HTTP mode: Standalone web server for research UI and expanded views
//
// Each mode can be run independently or combined based on deployment needs.
package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/bot"
	"github.com/lueurxax/telegram-digest-bot/internal/core/embeddings"
	"github.com/lueurxax/telegram-digest-bot/internal/core/links"
	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
	"github.com/lueurxax/telegram-digest-bot/internal/core/solr"
	"github.com/lueurxax/telegram-digest-bot/internal/expandedview"
	"github.com/lueurxax/telegram-digest-bot/internal/ingest/reader"
	"github.com/lueurxax/telegram-digest-bot/internal/output/digest"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/observability"
	"github.com/lueurxax/telegram-digest-bot/internal/process/enrichment"
	"github.com/lueurxax/telegram-digest-bot/internal/process/factcheck"
	"github.com/lueurxax/telegram-digest-bot/internal/process/linkseeder"
	"github.com/lueurxax/telegram-digest-bot/internal/process/pipeline"
	"github.com/lueurxax/telegram-digest-bot/internal/research"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const errBotInit = "bot initialization failed: %w"

const (
	discoveryMinSeenSettingKey       = "discovery_min_seen"
	discoveryMinEngagementSettingKey = "discovery_min_engagement"
	discoveryAllowSettingKey         = "discovery_description_allow"
	discoveryDenySettingKey          = "discovery_description_deny"
	discoveryMinSeenDefault          = 2
	discoveryMinEngagementDefault    = float32(50)
	msgFactCheckWorkerStopped        = "fact check worker stopped"
	msgEnrichmentWorkerStopped       = "enrichment worker stopped"
	llmAPIKeyMock                    = "mock"
	logFieldBaseURL                  = "base_url"
	logFieldItems                    = "items"
	researchRefreshInterval          = time.Hour
	researchRefreshLockID            = int64(94231)
	researchRefreshTimeout           = 15 * time.Minute
	researchClusterLookbackDays      = 14
	researchClusterItemLimit         = 2000
)

// App holds the application dependencies and provides methods to run different modes.
type App struct {
	cfg      *config.Config
	database *db.DB
	logger   *zerolog.Logger
}

type noopDigestPoster struct{}

func (noopDigestPoster) SendDigest(_ context.Context, _ int64, _ string, _ string) (int64, error) {
	return 0, nil
}

func (noopDigestPoster) SendDigestWithImage(_ context.Context, _ int64, _ string, _ string, _ []byte) (int64, error) {
	return 0, nil
}

func (noopDigestPoster) SendRichDigest(_ context.Context, _ int64, _ digest.RichDigestContent) (int64, error) {
	return 0, nil
}

func (noopDigestPoster) SendNotification(_ context.Context, _ string) error {
	return nil
}

// New creates a new App instance with the given dependencies.
func New(cfg *config.Config, database *db.DB, logger *zerolog.Logger) *App {
	return &App{
		cfg:      cfg,
		database: database,
		logger:   logger,
	}
}

// StartHealthServer starts the health check and metrics server.
func (a *App) StartHealthServer(ctx context.Context) error {
	var (
		expandedHandler http.Handler
		researchHandler http.Handler
	)

	if a.cfg.ExpandedViewSigningSecret != "" && a.cfg.ExpandedViewBaseURL != "" {
		tokenService := expandedview.NewTokenService(
			a.cfg.ExpandedViewSigningSecret,
			a.cfg.ExpandedViewTTLHours,
		)

		handler, err := expandedview.NewHandler(a.cfg, tokenService, a.database, a.logger)
		if err != nil {
			return fmt.Errorf("expanded view handler init: %w", err)
		}

		expandedHandler = handler

		a.logger.Info().Str(logFieldBaseURL, a.cfg.ExpandedViewBaseURL).Msg("Expanded view handler enabled")

		authService := research.NewAuthTokenService(a.cfg.ExpandedViewSigningSecret, research.DefaultLoginTokenTTL)

		researchHandler, err = research.NewHandler(a.cfg, a.database, authService, a.logger, a.rebuildResearch)
		if err != nil {
			return fmt.Errorf("research handler init: %w", err)
		}

		a.logger.Info().Str(logFieldBaseURL, a.cfg.ExpandedViewBaseURL).Msg("Research handler enabled")
	}

	srv := observability.NewServerWithHandlers(a.database, a.cfg.HealthPort, expandedHandler, researchHandler, a.logger)

	if err := srv.Start(ctx); err != nil {
		return fmt.Errorf("health server start: %w", err)
	}

	return nil
}

// RunHTTP runs the HTTP-only mode for serving the research UI and expanded view.
// This mode is designed for zero-downtime deployments with RollingUpdate strategy.
func (a *App) RunHTTP(ctx context.Context) error {
	a.logger.Info().Msg("Starting HTTP-only mode")

	return a.StartHealthServer(ctx)
}

// RunBot runs the bot mode.
func (a *App) RunBot(ctx context.Context) error {
	a.logger.Info().Msg("Starting bot mode")

	llmClient := a.newLLMClient(ctx)

	// Create a digest scheduler for preview commands (nil poster since we only need BuildDigest)
	digestBuilder := digest.New(a.cfg, a.database, nil, llmClient, a.logger)

	// Set up expand link generator for preview commands
	if a.cfg.ExpandedViewSigningSecret != "" && a.cfg.ExpandedViewBaseURL != "" {
		tokenService := expandedview.NewTokenService(
			a.cfg.ExpandedViewSigningSecret,
			a.cfg.ExpandedViewTTLHours,
		)
		digestBuilder.SetExpandLinkGenerator(tokenService)
	}

	//nolint:contextcheck // Budget alert callback fires async with no request context
	b, err := bot.New(a.cfg, a.database, digestBuilder, llmClient, a.logger)
	if err != nil {
		return fmt.Errorf(errBotInit, err)
	}

	if err := b.Run(ctx); err != nil {
		return fmt.Errorf("bot run: %w", err)
	}

	return nil
}

// RunReader runs the reader mode.
func (a *App) RunReader(ctx context.Context) error {
	a.logger.Info().Msg("Starting reader mode")

	channelRepo := db.NewChannelRepoAdapter(a.database)
	r := reader.New(a.cfg, a.database, a.database, channelRepo, a.logger)

	if err := r.Run(ctx); err != nil {
		return fmt.Errorf("reader run: %w", err)
	}

	return nil
}

// RunWorker runs the worker mode.
func (a *App) RunWorker(ctx context.Context) error {
	a.logger.Info().Msg("Starting worker mode")

	llmClient := a.newLLMClient(ctx)
	embeddingClient := a.newEmbeddingClient(ctx)
	resolver := a.newLinkResolver()
	seeder := a.newLinkSeeder()

	p := pipeline.New(a.cfg, a.database, llmClient, embeddingClient, resolver, seeder, a.logger)
	go a.runDiscoveryReconciliation(ctx)
	go a.runFactCheckWorker(ctx)
	go a.runEnrichmentWorker(ctx, embeddingClient)
	go a.runResearchRefresh(ctx)

	if err := p.Run(ctx); err != nil {
		return fmt.Errorf("pipeline run: %w", err)
	}

	return nil
}

func (a *App) runFactCheckWorker(ctx context.Context) {
	worker := factcheck.NewWorker(a.cfg, a.database, a.logger)
	if err := worker.Run(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			a.logger.Info().Msg(msgFactCheckWorkerStopped)
			return
		}

		a.logger.Warn().Err(err).Msg(msgFactCheckWorkerStopped)
	}
}

func (a *App) runEnrichmentWorker(ctx context.Context, embeddingClient embeddings.Client) {
	llmClient := a.newLLMClient(ctx)
	worker := enrichment.NewWorker(a.cfg, a.database, embeddingClient, a.logger)

	a.configureEnrichmentWorker(worker, llmClient)

	if err := worker.Run(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			a.logger.Info().Msg(msgEnrichmentWorkerStopped)

			return
		}

		a.logger.Warn().Err(err).Msg(msgEnrichmentWorkerStopped)
	}
}

func (a *App) runResearchRefresh(ctx context.Context) {
	// Research refresh runs unconditionally to keep analytics tables populated.
	// These tables (claims, cluster_first_appearance, etc.) are used by the
	// research UI and provide useful analytics even without expanded view feature.
	a.refreshResearchOnce(ctx)

	ticker := time.NewTicker(researchRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.refreshResearchOnce(ctx)
		}
	}
}

func (a *App) refreshResearchOnce(ctx context.Context) {
	refreshCtx, cancel := context.WithTimeout(ctx, researchRefreshTimeout)
	defer cancel()

	acquired, err := a.database.TryAcquireAdvisoryLock(refreshCtx, researchRefreshLockID)
	if err != nil {
		a.logger.Warn().Err(err).Msg("research refresh lock failed")
		return
	}

	if !acquired {
		return
	}

	defer func() {
		if err := a.database.ReleaseAdvisoryLock(refreshCtx, researchRefreshLockID); err != nil {
			a.logger.Warn().Err(err).Msg("release research refresh lock failed")
		}
	}()

	a.runResearchAnalytics(refreshCtx)
	a.runResearchMaintenance(refreshCtx)
}

func (a *App) runResearchAnalytics(ctx context.Context) {
	// Log pipeline health
	readyCount, err := a.database.CountReadyItems(ctx)
	if err != nil {
		a.logger.Debug().Err(err).Msg("failed to count ready items")
	}

	backlog, err := a.database.GetBacklogCount(ctx)
	if err != nil {
		a.logger.Debug().Err(err).Msg("failed to get backlog count")
	}

	a.logger.Info().Int("ready_items", readyCount).Int("backlog", backlog).Msg("Starting research refresh")

	a.runResearchClustering(ctx)

	if err := a.database.RefreshResearchMaterializedViews(ctx); err != nil {
		a.logger.Warn().Err(err).Msg("research refresh failed")
		// Continue to heuristic claims population even if views fail
	}

	// Populate heuristic claims for items without evidence
	a.populateHeuristicClaims(ctx)
}

func (a *App) rebuildResearch(ctx context.Context) error {
	a.runResearchClustering(ctx)

	if err := a.database.RefreshResearchMaterializedViews(ctx); err != nil {
		return fmt.Errorf("refresh research views: %w", err)
	}

	a.populateHeuristicClaims(ctx)

	return nil
}

func (a *App) runResearchClustering(ctx context.Context) {
	end := time.Now().UTC()
	start := end.Add(-time.Duration(researchClusterLookbackDays) * 24 * time.Hour)

	items, err := a.database.GetReadyItemsForResearch(ctx, start, end, researchClusterItemLimit)
	if err != nil {
		a.logger.Warn().Err(err).Msg("research clustering: failed to load items")
		return
	}

	if len(items) < 2 {
		a.logger.Debug().Int(logFieldItems, len(items)).Msg("research clustering skipped: not enough items")
		return
	}

	clusterer := digest.New(a.cfg, a.database, noopDigestPoster{}, nil, a.logger)
	if err := clusterer.ClusterItemsForResearch(ctx, items, start, end, a.logger); err != nil {
		a.logger.Warn().Err(err).Msg("research clustering failed")
		return
	}

	a.logger.Info().
		Int("items", len(items)).
		Time("start", start).
		Time("end", end).
		Msg("research clustering completed")
}

func (a *App) runResearchMaintenance(ctx context.Context) {
	if err := a.database.DeleteExpiredResearchSessions(ctx); err != nil {
		a.logger.Warn().Err(err).Msg("cleanup research sessions failed")
	}

	retentionCounts, err := a.database.CleanupResearchRetention(ctx)
	if err != nil {
		a.logger.Warn().Err(err).Msg("cleanup research retention failed")
		return
	}

	if retentionCounts.ItemsDeleted > 0 || retentionCounts.EvidenceDeleted > 0 || retentionCounts.TranslationsDeleted > 0 {
		a.logger.Info().
			Int64("items_deleted", retentionCounts.ItemsDeleted).
			Int64("evidence_deleted", retentionCounts.EvidenceDeleted).
			Int64("translations_deleted", retentionCounts.TranslationsDeleted).
			Msg("research retention cleanup")
	}
}

func (a *App) populateHeuristicClaims(ctx context.Context) {
	populator := research.NewHeuristicClaimPopulator(a.database, a.logger)

	// Enable semantic deduplication if embedding provider is configured
	if a.hasConfiguredEmbeddingProvider() {
		embeddingClient := a.newEmbeddingClient(ctx)
		populator.SetEmbeddingClient(embeddingClient)
	}

	inserted, err := populator.PopulateHeuristicClaims(ctx)
	if err != nil {
		a.logger.Warn().Err(err).Msg("heuristic claims population failed")
		return
	}

	if inserted > 0 {
		a.logger.Info().Int64("claims_inserted", inserted).Msg("heuristic claims populated")
	}
}

func (a *App) configureEnrichmentWorker(worker *enrichment.Worker, llmClient llm.Client) {
	if a.hasConfiguredLLMProvider() {
		worker.EnableLLMExtraction(llmClient, a.cfg.LLMModel)

		// Always enable LLM query generation when provider is available
		queryModel := a.cfg.EnrichmentQueryLLMModel
		if queryModel == "" {
			queryModel = a.cfg.LLMModel
		}

		worker.EnableLLMQueryGeneration(llmClient, queryModel)
	}

	if a.cfg.EnrichmentQueryTranslate {
		transModel := a.cfg.TranslationModel
		if transModel == "" {
			transModel = a.cfg.LLMModel
		}

		worker.SetTranslationClient(enrichment.NewTranslationAdapter(llmClient, transModel))
	}
}

func (a *App) hasConfiguredLLMProvider() bool {
	if a.cfg.LLMAPIKey != "" && a.cfg.LLMAPIKey != llmAPIKeyMock {
		return true
	}

	if a.cfg.AnthropicAPIKey != "" {
		return true
	}

	if a.cfg.GoogleAPIKey != "" || a.cfg.GoogleAPIKeyPaid != "" {
		return true
	}

	if a.cfg.OpenRouterAPIKey != "" {
		return true
	}

	return false
}

func (a *App) hasConfiguredEmbeddingProvider() bool {
	// OpenAI embedding (uses LLMAPIKey)
	if a.cfg.LLMAPIKey != "" && a.cfg.LLMAPIKey != llmAPIKeyMock {
		return true
	}

	// Cohere embedding
	if a.cfg.CohereAPIKey != "" {
		return true
	}

	// Google embedding
	if a.cfg.GoogleAPIKey != "" {
		return true
	}

	return false
}

func (a *App) runDiscoveryReconciliation(ctx context.Context) {
	const (
		reconcileInterval = 6 * time.Hour
		cleanupBatchSize  = 100
		cleanupBatchLimit = 100
		systemAdminUserID = int64(0)
	)

	a.runDiscoveryCleanupOnce(ctx, cleanupBatchSize, cleanupBatchLimit, systemAdminUserID)

	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.runDiscoveryCleanupOnce(ctx, cleanupBatchSize, cleanupBatchLimit, systemAdminUserID)
		}
	}
}

func (a *App) runDiscoveryCleanupOnce(ctx context.Context, batchSize int, maxBatches int, adminID int64) {
	updatedTotal := 0

	for i := 0; i < maxBatches; i++ {
		if ctx.Err() != nil {
			return
		}

		updated, err := a.database.CleanupDiscoveriesBatch(ctx, batchSize, adminID)
		if err != nil {
			a.logger.Warn().Err(err).Msg("discovery cleanup batch failed")
			continue
		}

		if updated == 0 {
			break
		}

		updatedTotal += updated
	}

	if updatedTotal > 0 {
		a.logger.Info().Int("updated", updatedTotal).Msg("discovery cleanup complete")
	}

	a.updateDiscoveryMetrics(ctx)
}

func (a *App) updateDiscoveryMetrics(ctx context.Context) {
	stats, err := a.database.GetDiscoveryStats(ctx)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			a.logger.Warn().Err(err).Msg("failed to fetch discovery stats")
		}

		return
	}

	minSeen, minEngagement := a.getDiscoveryThresholds(ctx)

	allow, deny := a.getDiscoveryKeywordFilters(ctx)
	candidateCount, allowMiss, denyHit := a.getDiscoveryKeywordFilterStats(ctx, minSeen, minEngagement, allow, deny)

	pending := stats.PendingCount

	actionable := int64(candidateCount - allowMiss - denyHit)
	if actionable < 0 {
		actionable = 0
	}

	observability.DiscoveryPending.Set(float64(pending))
	observability.DiscoveryActionable.Set(float64(actionable))

	approvalRate := 0.0

	denom := stats.AddedCount + stats.RejectedCount
	if denom > 0 {
		approvalRate = float64(stats.AddedCount) / float64(denom)
	}

	observability.DiscoveryApprovalRate.Set(approvalRate)
}

func (a *App) getDiscoveryThresholds(ctx context.Context) (int, float32) {
	minSeen := discoveryMinSeenDefault
	if err := a.database.GetSetting(ctx, discoveryMinSeenSettingKey, &minSeen); err != nil {
		if !errors.Is(err, context.Canceled) {
			a.logger.Warn().Err(err).Msg("failed to read discovery_min_seen")
		}
	}

	if minSeen < 1 {
		minSeen = discoveryMinSeenDefault
	}

	minEngagement := discoveryMinEngagementDefault
	if err := a.database.GetSetting(ctx, discoveryMinEngagementSettingKey, &minEngagement); err != nil {
		if !errors.Is(err, context.Canceled) {
			a.logger.Warn().Err(err).Msg("failed to read discovery_min_engagement")
		}
	}

	if minEngagement < 0 {
		minEngagement = discoveryMinEngagementDefault
	}

	return minSeen, minEngagement
}

func (a *App) getDiscoveryKeywordFilters(ctx context.Context) ([]string, []string) {
	var allow []string
	if err := a.database.GetSetting(ctx, discoveryAllowSettingKey, &allow); err != nil {
		if !errors.Is(err, context.Canceled) {
			a.logger.Warn().Err(err).Msg("failed to read discovery_description_allow")
		}
	}

	var deny []string
	if err := a.database.GetSetting(ctx, discoveryDenySettingKey, &deny); err != nil {
		if !errors.Is(err, context.Canceled) {
			a.logger.Warn().Err(err).Msg("failed to read discovery_description_deny")
		}
	}

	return db.NormalizeDiscoveryKeywords(allow), db.NormalizeDiscoveryKeywords(deny)
}

func (a *App) getDiscoveryKeywordFilterStats(ctx context.Context, minSeen int, minEngagement float32, allow, deny []string) (int, int, int) {
	candidates, err := a.database.GetPendingDiscoveriesForFiltering(ctx, minSeen, minEngagement)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			a.logger.Warn().Err(err).Msg("failed to fetch discovery keyword candidates")
		}

		return 0, 0, 0
	}

	if len(allow) == 0 && len(deny) == 0 {
		return len(candidates), 0, 0
	}

	_, allowMiss, denyHit := db.FilterDiscoveriesByKeywords(candidates, allow, deny)

	return len(candidates), allowMiss, denyHit
}

// RunDigest runs the digest mode.
func (a *App) RunDigest(ctx context.Context, once bool) error {
	a.logger.Info().Bool("once", once).Msg("Starting digest mode")

	llmClient := a.newLLMClient(ctx)

	// Create bot as DigestPoster only (nil digestBuilder since bot won't process commands)
	//nolint:contextcheck // Budget alert callback fires async with no request context
	b, err := bot.New(a.cfg, a.database, nil, llmClient, a.logger)
	if err != nil {
		return fmt.Errorf(errBotInit, err)
	}

	s := digest.New(a.cfg, a.database, b, llmClient, a.logger)

	// Set up expand link generator if signing secret and base URL are configured
	if a.cfg.ExpandedViewSigningSecret != "" && a.cfg.ExpandedViewBaseURL != "" {
		tokenService := expandedview.NewTokenService(
			a.cfg.ExpandedViewSigningSecret,
			a.cfg.ExpandedViewTTLHours,
		)
		s.SetExpandLinkGenerator(tokenService)
		a.logger.Info().Str(logFieldBaseURL, a.cfg.ExpandedViewBaseURL).Msg("Expanded view links enabled in digest")
	}

	if once {
		if err := s.RunOnce(ctx); err != nil {
			return fmt.Errorf("digest run once: %w", err)
		}

		return nil
	}

	if err := s.Run(ctx); err != nil {
		return fmt.Errorf("digest run: %w", err)
	}

	return nil
}

// newLLMClient creates a new LLM client with multi-provider fallback.
func (a *App) newLLMClient(ctx context.Context) llm.Client {
	return llm.New(ctx, a.cfg, a.database, a.database, a.logger)
}

// newEmbeddingClient creates a new embedding client with multi-provider support.
func (a *App) newEmbeddingClient(ctx context.Context) embeddings.Client {
	logger := a.logger.With().Str("component", "embeddings").Logger()

	return embeddings.NewClient(ctx, embeddings.Config{
		OpenAIAPIKey:     a.cfg.LLMAPIKey,
		OpenAIModel:      a.cfg.OpenAIEmbeddingModel,
		OpenAIDimensions: a.cfg.OpenAIEmbeddingDimensions,
		OpenAIRateLimit:  a.cfg.RateLimitRPS,
		CohereAPIKey:     a.cfg.CohereAPIKey,
		CohereModel:      a.cfg.CohereEmbeddingModel,
		CohereRateLimit:  1,
		GoogleAPIKey:     a.cfg.GoogleAPIKey,
		GoogleRateLimit:  a.cfg.RateLimitRPS,
		ProviderOrder:    a.cfg.EmbeddingProviderOrder,
		CircuitBreakerConfig: embeddings.CircuitBreakerConfig{
			Threshold:  a.cfg.EmbeddingCircuitThreshold,
			ResetAfter: a.cfg.EmbeddingCircuitTimeout,
		},
		TargetDimensions: a.cfg.OpenAIEmbeddingDimensions,
	}, &logger)
}

// newLinkResolver creates a new link resolver.
func (a *App) newLinkResolver() *links.Resolver {
	return links.New(a.cfg, a.database, db.NewChannelRepoAdapter(a.database), nil, a.logger)
}

// newLinkSeeder creates a new link seeder for seeding external links into crawler queue.
func (a *App) newLinkSeeder() pipeline.LinkSeeder {
	if a.cfg.SolrBaseURL == "" {
		return nil
	}

	solrClient := solr.New(solr.Config{
		BaseURL:    a.cfg.SolrBaseURL,
		Timeout:    a.cfg.SolrTimeout,
		MaxResults: a.cfg.SolrMaxResults,
	})

	seeder := linkseeder.NewFromConfig(a.cfg, solrClient, a.logger)

	return &linkSeederAdapter{seeder: seeder}
}

// linkSeederAdapter adapts *linkseeder.Seeder to pipeline.LinkSeeder interface.
type linkSeederAdapter struct {
	seeder *linkseeder.Seeder
}

// SeedLinks implements pipeline.LinkSeeder.
func (a *linkSeederAdapter) SeedLinks(ctx context.Context, input pipeline.LinkSeedInput) pipeline.LinkSeedResult {
	result := a.seeder.SeedLinks(ctx, linkseeder.SeedInput{
		PeerID:    input.PeerID,
		MessageID: input.MessageID,
		Channel:   input.Channel,
		URLs:      input.URLs,
	})

	return pipeline.LinkSeedResult{
		Extracted: result.Extracted,
		Enqueued:  result.Enqueued,
		Skipped:   result.Skipped,
		Errors:    result.Errors,
	}
}
