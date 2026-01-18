package app

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/bot"
	"github.com/lueurxax/telegram-digest-bot/internal/core/links"
	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
	"github.com/lueurxax/telegram-digest-bot/internal/ingest/reader"
	"github.com/lueurxax/telegram-digest-bot/internal/output/digest"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/observability"
	"github.com/lueurxax/telegram-digest-bot/internal/process/enrichment"
	"github.com/lueurxax/telegram-digest-bot/internal/process/factcheck"
	"github.com/lueurxax/telegram-digest-bot/internal/process/pipeline"
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
)

// App holds the application dependencies and provides methods to run different modes.
type App struct {
	cfg      *config.Config
	database *db.DB
	logger   *zerolog.Logger
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
	srv := observability.NewServer(a.database, a.cfg.HealthPort, a.logger)

	if err := srv.Start(ctx); err != nil {
		return fmt.Errorf("health server start: %w", err)
	}

	return nil
}

// RunBot runs the bot mode.
func (a *App) RunBot(ctx context.Context) error {
	a.logger.Info().Msg("Starting bot mode")

	llmClient := a.newLLMClient()

	// Create a digest scheduler for preview commands (nil poster since we only need BuildDigest)
	digestBuilder := digest.New(a.cfg, a.database, nil, llmClient, a.logger)

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

	llmClient := a.newLLMClient()
	resolver := a.newLinkResolver()

	p := pipeline.New(a.cfg, a.database, llmClient, resolver, a.logger)
	go a.runDiscoveryReconciliation(ctx)
	go a.runFactCheckWorker(ctx)
	go a.runEnrichmentWorker(ctx)

	if err := p.Run(ctx); err != nil {
		return fmt.Errorf("pipeline run: %w", err)
	}

	return nil
}

func (a *App) runFactCheckWorker(ctx context.Context) {
	worker := factcheck.NewWorker(a.cfg, a.database, a.logger)
	if err := worker.Run(ctx); err != nil {
		a.logger.Warn().Err(err).Msg("fact check worker stopped")
	}
}

func (a *App) runEnrichmentWorker(ctx context.Context) {
	llmClient := a.newLLMClient()

	worker := enrichment.NewWorker(a.cfg, a.database, llmClient, a.logger)

	// Wire optional LLM claim extraction
	if a.cfg.LLMAPIKey != "" && a.cfg.LLMAPIKey != "mock" {
		worker.EnableLLMExtraction(llmClient, a.cfg.LLMModel)
	}

	// Wire translation if configured
	if a.cfg.EnrichmentQueryTranslate {
		transModel := a.cfg.TranslationModel
		if transModel == "" {
			transModel = a.cfg.LLMModel // Fallback to main model
		}

		worker.SetTranslationClient(enrichment.NewTranslationAdapter(llmClient, transModel))
	}

	if err := worker.Run(ctx); err != nil {
		a.logger.Warn().Err(err).Msg("enrichment worker stopped")
	}
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
		a.logger.Warn().Err(err).Msg("failed to fetch discovery stats")

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
		a.logger.Warn().Err(err).Msg("failed to read discovery_min_seen")
	}

	if minSeen < 1 {
		minSeen = discoveryMinSeenDefault
	}

	minEngagement := discoveryMinEngagementDefault
	if err := a.database.GetSetting(ctx, discoveryMinEngagementSettingKey, &minEngagement); err != nil {
		a.logger.Warn().Err(err).Msg("failed to read discovery_min_engagement")
	}

	if minEngagement < 0 {
		minEngagement = discoveryMinEngagementDefault
	}

	return minSeen, minEngagement
}

func (a *App) getDiscoveryKeywordFilters(ctx context.Context) ([]string, []string) {
	var allow []string
	if err := a.database.GetSetting(ctx, discoveryAllowSettingKey, &allow); err != nil {
		a.logger.Warn().Err(err).Msg("failed to read discovery_description_allow")
	}

	var deny []string
	if err := a.database.GetSetting(ctx, discoveryDenySettingKey, &deny); err != nil {
		a.logger.Warn().Err(err).Msg("failed to read discovery_description_deny")
	}

	return db.NormalizeDiscoveryKeywords(allow), db.NormalizeDiscoveryKeywords(deny)
}

func (a *App) getDiscoveryKeywordFilterStats(ctx context.Context, minSeen int, minEngagement float32, allow, deny []string) (int, int, int) {
	candidates, err := a.database.GetPendingDiscoveriesForFiltering(ctx, minSeen, minEngagement)
	if err != nil {
		a.logger.Warn().Err(err).Msg("failed to fetch discovery keyword candidates")

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

	llmClient := a.newLLMClient()

	// Create bot as DigestPoster only (nil digestBuilder since bot won't process commands)
	b, err := bot.New(a.cfg, a.database, nil, llmClient, a.logger)
	if err != nil {
		return fmt.Errorf(errBotInit, err)
	}

	s := digest.New(a.cfg, a.database, b, llmClient, a.logger)

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

// newLLMClient creates a new LLM client.
func (a *App) newLLMClient() llm.Client {
	return llm.New(a.cfg, a.database, a.logger)
}

// newLinkResolver creates a new link resolver.
func (a *App) newLinkResolver() *links.Resolver {
	return links.New(a.cfg, a.database, db.NewChannelRepoAdapter(a.database), nil, a.logger)
}
