package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/core/embeddings"
	linkscore "github.com/lueurxax/telegram-digest-bot/internal/core/links"
	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/observability"
	"github.com/lueurxax/telegram-digest-bot/internal/process/dedup"
	"github.com/lueurxax/telegram-digest-bot/internal/process/factcheck"
	"github.com/lueurxax/telegram-digest-bot/internal/process/filters"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

type Repository interface {
	GetSetting(ctx context.Context, key string, target interface{}) error
	GetUnprocessedMessages(ctx context.Context, limit int) ([]db.RawMessage, error)
	GetBacklogCount(ctx context.Context) (int, error)
	GetActiveFilters(ctx context.Context) ([]db.Filter, error)
	MarkAsProcessed(ctx context.Context, id string) error
	ReleaseClaimedMessage(ctx context.Context, id string) error
	RecoverStuckPipelineMessages(ctx context.Context, stuckThreshold time.Duration) (int64, error)
	GetRecentMessagesForChannel(ctx context.Context, channelID string, before time.Time, limit int) ([]string, error)
	GetChannelStats(ctx context.Context) (map[string]db.ChannelStats, error)
	SaveItem(ctx context.Context, item *db.Item) error
	SaveItemError(ctx context.Context, rawMsgID string, errJSON []byte) error
	SaveRelevanceGateLog(ctx context.Context, rawMsgID string, decision string, confidence *float32, reason, model, gateVersion string) error
	SaveRawMessageDropLog(ctx context.Context, rawMsgID, reason, detail string) error
	SaveEmbedding(ctx context.Context, itemID string, embedding []float32) error
	EnqueueFactCheck(ctx context.Context, itemID, claim, normalizedClaim string) error
	CountPendingFactChecks(ctx context.Context) (int, error)
	EnqueueEnrichment(ctx context.Context, itemID, summary string) error
	CountPendingEnrichments(ctx context.Context) (int, error)
	CheckStrictDuplicate(ctx context.Context, hash string, id string) (bool, error)
	FindSimilarItem(ctx context.Context, embedding []float32, threshold float32, minCreatedAt time.Time) (string, error)
	FindSimilarItemForChannel(ctx context.Context, embedding []float32, channelID string, threshold float32, minCreatedAt time.Time) (string, error)
	FindSimilarIrrelevantItem(ctx context.Context, embedding []float32, since time.Time) (*db.SimilarIrrelevantItem, error)
	GetWeightedChannelRatingSummary(ctx context.Context, since time.Time, halfLifeDays float64) ([]db.WeightedRatingSummary, error)
	GetSummaryCache(ctx context.Context, canonicalHash, digestLanguage string) (*db.SummaryCacheEntry, error)
	UpsertSummaryCache(ctx context.Context, entry *db.SummaryCacheEntry) error
	GetItemByCanonicalURL(ctx context.Context, canonicalURL, excludeRawMsgID string) (*db.CanonicalItem, error)
	LinkMessageToLink(ctx context.Context, rawMsgID, linkCacheID string, position int) error
	// Bullet extraction
	InsertBullet(ctx context.Context, bullet *db.Bullet) error
	UpdateBulletEmbedding(ctx context.Context, bulletID string, embedding []float32) error
	UpdateBulletStatus(ctx context.Context, bulletID, status string) error
	GetPendingBulletsForDedup(ctx context.Context, lookbackHours int) ([]db.PendingBulletForDedup, error)
	MarkDuplicateBullets(ctx context.Context, bulletIDs []string) error
	MarkBulletAsDuplicateOf(ctx context.Context, bulletID, canonicalID string) error
	MarkBulletAsCanonical(ctx context.Context, bulletID string) error
}

// Compile-time assertion that *db.DB implements Repository.
var _ Repository = (*db.DB)(nil)

type LinkResolver interface {
	ResolveLinks(ctx context.Context, text string, maxLinks int, webTTL, tgTTL time.Duration) ([]domain.ResolvedLink, error)
}

// LinkSeeder seeds external links from messages into the crawler queue.
// Implemented by *linkseeder.Seeder.
type LinkSeeder interface {
	SeedLinks(ctx context.Context, input LinkSeedInput) LinkSeedResult
}

// LinkSeedInput contains information for link seeding.
// This type is compatible with linkseeder.SeedInput.
type LinkSeedInput struct {
	PeerID    int64
	MessageID int64
	Channel   string
	URLs      []string
}

// LinkSeedResult contains the results of a seeding operation.
// This type is compatible with linkseeder.SeedResult.
type LinkSeedResult struct {
	Extracted int
	Enqueued  int
	Skipped   map[string]int
	Errors    int
}

type Pipeline struct {
	cfg             *config.Config
	database        Repository
	llmClient       llm.Client
	embeddingClient embeddings.Client
	linkResolver    LinkResolver
	linkSeeder      LinkSeeder
	logger          *zerolog.Logger
}

type pipelineSettings struct {
	batchSize                  int
	filterList                 []db.Filter
	adsFilterEnabled           bool
	minLengthDefault           int
	minLengthByLang            map[string]int
	summaryStripPhrasesByLang  map[string][]string
	summaryStripPhrasesDefault []string
	domainAllowlist            map[string]struct{}
	domainDenylist             map[string]struct{}
	adsKeywords                []string
	skipForwards               bool
	filtersMode                string
	dedupMode                  string
	dedupWindow                time.Duration
	dedupSameChannelWindow     time.Duration
	topicsEnabled              bool
	relevanceThreshold         float32
	digestLanguage             string
	visionRoutingEnabled       bool
	tieredImportanceEnabled    bool
	digestTone                 string
	normalizeScores            bool
	relevanceGateEnabled       bool
	relevanceGateMode          string
	relevanceGateModel         string
	channelStats               map[string]db.ChannelStats
	linkEnrichmentEnabled      bool
	maxLinks                   int
	linkCacheTTL               time.Duration
	tgLinkCacheTTL             time.Duration
	linkEnrichmentScope        string
	linkMinWords               int
	linkSnippetMaxChars        int
	linkEmbeddingMaxMsgLen     int
	linkPrimaryMinWords        int
	linkPrimaryShortMsgChars   int
	linkPrimaryMaxLinks        int
	linkPrimaryAllowlist       map[string]struct{}
	linkPrimaryDonationDeny    map[string]struct{}
	linkPrimaryCTATerms        []string
	linkCanonicalAllowlist     map[string]struct{}
	linkCanonicalTrusted       map[string]struct{}
	linkCanonicalDenylist      map[string]struct{}
	summaryCachePromptVersion  string
	bulletModeEnabled          bool
	bulletMinImportance        float32
}

const (
	dropReasonDuplicateBatch      = "duplicate_batch"
	dropReasonForwarded           = "forwarded"
	dropReasonRelevanceGate       = "relevance_gate"
	dropReasonDedupSemanticBatch  = "dedup_semantic_batch"
	dropReasonDedupSemanticSame   = "dedup_semantic_same_channel"
	dropReasonDedupSemanticGlobal = "dedup_semantic_global"
	dropReasonDedupStrictGlobal   = "dedup_strict_global"
	bulletLLMBatchSizeLimit       = 5

	defaultPromptVersion   = "v1"
	summaryCacheMaxAgeDays = 30
	hoursPerDay            = 24
)

func New(cfg *config.Config, database Repository, llmClient llm.Client, embeddingClient embeddings.Client, linkResolver LinkResolver, linkSeeder LinkSeeder, logger *zerolog.Logger) *Pipeline {
	return &Pipeline{
		cfg:             cfg,
		database:        database,
		llmClient:       llmClient,
		embeddingClient: embeddingClient,
		linkResolver:    linkResolver,
		linkSeeder:      linkSeeder,
		logger:          logger,
	}
}

func (p *Pipeline) Run(ctx context.Context) error {
	pollInterval, err := time.ParseDuration(p.cfg.WorkerPollInterval)
	if err != nil {
		p.logger.Error().Err(err).Str("interval", p.cfg.WorkerPollInterval).Msg("invalid worker poll interval, using 10s")

		pollInterval = DefaultPollInterval
	}

	// Track last recovery time
	lastRecovery := time.Now()
	lastBulletDedup := time.Now()
	bulletDedupInterval := time.Duration(p.cfg.BulletDedupIntervalMins) * time.Minute

	for {
		// Periodically recover stuck messages
		if time.Since(lastRecovery) >= RecoveryInterval {
			p.recoverStuckMessages(ctx)

			lastRecovery = time.Now()
		}

		// Periodically deduplicate pending bullets
		if time.Since(lastBulletDedup) >= bulletDedupInterval {
			p.runBulletDeduplication(ctx)

			lastBulletDedup = time.Now()
		}

		correlationID := uuid.New().String()
		p.logger.Info().Str(LogFieldCorrelationID, correlationID).Msg("Starting pipeline batch")

		if err := p.processNextBatch(ctx, correlationID); err != nil {
			p.logger.Error().Err(err).Str(LogFieldCorrelationID, correlationID).Msg("failed to process batch")
		}

		select {
		case <-ctx.Done():
			return ctx.Err() //nolint:wrapcheck
		case <-time.After(pollInterval):
		}
	}
}

// recoverStuckMessages recovers messages that were claimed but never processed.
// This handles cases where a worker crashed or timed out after claiming messages.
func (p *Pipeline) recoverStuckMessages(ctx context.Context) {
	recovered, err := p.database.RecoverStuckPipelineMessages(ctx, StuckMessageThreshold)
	if err != nil {
		p.logger.Error().Err(err).Msg("failed to recover stuck pipeline messages")
		return
	}

	if recovered > 0 {
		p.logger.Info().Int64("recovered", recovered).Msg("recovered stuck pipeline messages")
	}
}

// runBulletDeduplication processes pending bullets and marks duplicates.
func (p *Pipeline) runBulletDeduplication(ctx context.Context) {
	logger := p.logger.With().Str(LogFieldTask, "bullet_dedup").Logger()

	if err := p.DeduplicatePendingBullets(ctx, logger); err != nil {
		logger.Error().Err(err).Msg("bullet deduplication failed")
	}
}

func (p *Pipeline) processNextBatch(ctx context.Context, correlationID string) error {
	logger := p.logger.With().Str(LogFieldCorrelationID, correlationID).Logger()

	s, err := p.loadPipelineSettings(ctx, logger)
	if err != nil {
		return err
	}

	messages, err := p.database.GetUnprocessedMessages(ctx, s.batchSize)
	if err != nil {
		return fmt.Errorf("get unprocessed messages: %w", err)
	}

	if len(messages) == 0 {
		return nil
	}

	batchStart := time.Now()

	defer func() {
		observability.PipelineBatchDurationSeconds.Observe(time.Since(batchStart).Seconds())
	}()

	p.recordMessageAgeMetrics(messages)

	// Log backlog
	backlog, err := p.database.GetBacklogCount(ctx)
	if err == nil {
		logger.Info().Int("backlog", backlog).Msg("Pipeline backlog")
		observability.PipelineBacklog.Set(float64(backlog))
	}

	candidates, embeddings, err := p.prepareCandidates(ctx, logger, messages, s)
	if err != nil {
		return err
	}

	if len(candidates) == 0 {
		return nil
	}

	results, err := p.runLLMProcessing(ctx, logger, candidates, s)
	if err != nil {
		return err
	}

	return p.storeResults(ctx, logger, candidates, results, embeddings, s)
}

// recordMessageAgeMetrics records metrics for message age and backlog.
func (p *Pipeline) recordMessageAgeMetrics(messages []db.RawMessage) {
	now := time.Now()

	oldestAge := now.Sub(messages[0].TGDate).Seconds()
	if oldestAge < 0 {
		oldestAge = 0
	}

	observability.PipelineBacklogOldestAgeSeconds.Set(oldestAge)

	for _, message := range messages {
		age := now.Sub(message.TGDate).Seconds()
		if age < 0 {
			age = 0
		}

		observability.PipelineMessageAgeSeconds.Observe(age)

		kind := "native"

		if message.IsForward {
			kind = "forwarded"
		}

		observability.PipelineMessageAgeSecondsByKind.WithLabelValues(kind).Observe(age)
	}
}

var (
	nameRegex   = regexp.MustCompile(`[A-Z][a-z]+`)
	numberRegex = regexp.MustCompile(`\d+`)
	dateRegex   = regexp.MustCompile(`(?i)(january|february|march|april|may|june|july|august|september|october|november|december|monday|tuesday|wednesday|thursday|friday|saturday|sunday|today|yesterday|tomorrow)`)
)

func (p *Pipeline) hasUniqueInfo(summary string) bool {
	// Strip HTML tags for cleaner matching
	clean := summary
	if strings.Contains(summary, "<") {
		clean = regexp.MustCompile(`<[^>]*>`).ReplaceAllString(summary, "")
	}

	return nameRegex.MatchString(clean) || numberRegex.MatchString(clean) || dateRegex.MatchString(clean)
}

func (p *Pipeline) augmentTextWithLinks(c *llm.MessageInput, s *pipelineSettings, scope string) string {
	if !strings.Contains(s.linkEnrichmentScope, scope) || len(c.ResolvedLinks) == 0 {
		return c.Text
	}

	// Heuristics from proposal
	if scope == domain.ScopeTopic {
		// Topic detection uses link snippets only if message is short or lacks entities.
		// For now we check length < 120.
		if len(c.Text) >= domain.ShortMessageThreshold {
			return c.Text
		}
	}

	var sb strings.Builder
	sb.WriteString(c.Text)
	sb.WriteString("\n\nReferenced Content:\n")

	for _, link := range c.ResolvedLinks {
		// Guardrails: LINK_MIN_WORDS and denylist (denylist handled by resolver)
		wordCount := len(strings.Fields(link.Content))
		if wordCount < s.linkMinWords {
			continue
		}

		snippet := link.Content
		if len(snippet) > s.linkSnippetMaxChars {
			snippet = snippet[:s.linkSnippetMaxChars]
		}

		sb.WriteString(fmt.Sprintf("- %s: %s\n", link.Title, snippet))
	}

	return sb.String()
}

func (p *Pipeline) loadPipelineSettings(ctx context.Context, logger zerolog.Logger) (*pipelineSettings, error) {
	s := &pipelineSettings{
		batchSize:                 p.cfg.WorkerBatchSize,
		relevanceThreshold:        p.cfg.RelevanceThreshold,
		relevanceGateEnabled:      p.cfg.RelevanceGateEnabled,
		relevanceGateMode:         p.cfg.RelevanceGateMode,
		relevanceGateModel:        p.cfg.RelevanceGateModel,
		linkEnrichmentEnabled:     true,
		maxLinks:                  p.cfg.MaxLinksPerMessage,
		linkCacheTTL:              p.cfg.LinkCacheTTL,
		tgLinkCacheTTL:            p.cfg.TelegramLinkCacheTTL,
		linkEnrichmentScope:       p.cfg.LinkEnrichmentScope,
		linkMinWords:              p.cfg.LinkMinWords,
		linkSnippetMaxChars:       p.cfg.LinkSnippetMaxChars,
		linkEmbeddingMaxMsgLen:    p.cfg.LinkEmbeddingMaxMsgLen,
		linkPrimaryMinWords:       p.cfg.LinkPrimaryMinWords,
		linkPrimaryShortMsgChars:  p.cfg.LinkPrimaryShortMsg,
		linkPrimaryMaxLinks:       p.cfg.LinkPrimaryMaxLinks,
		linkPrimaryAllowlist:      parseDomainList(p.cfg.LinkPrimaryAllowlist),
		linkPrimaryDonationDeny:   parseDomainList(p.cfg.LinkPrimaryDonationDL),
		linkPrimaryCTATerms:       parseCSVList(p.cfg.LinkPrimaryCTATerms),
		linkCanonicalAllowlist:    parseDomainList(p.cfg.LinkCanonicalAllowlist),
		linkCanonicalTrusted:      parseDomainList(p.cfg.LinkCanonicalTrusted),
		linkCanonicalDenylist:     parseDomainList(p.cfg.LinkCanonicalDenylist),
		summaryCachePromptVersion: defaultPromptVersion,
		bulletModeEnabled:         true,
		bulletMinImportance:       p.cfg.BulletMinImportance,
		filtersMode:               FilterModeMixed,
		dedupMode:                 DedupModeSemantic,
		dedupWindow:               time.Duration(p.cfg.ClusterTimeWindowHours) * time.Hour,
		dedupSameChannelWindow:    time.Duration(p.cfg.DedupSameChannelWindowHours) * time.Hour,
		topicsEnabled:             true,
		minLengthDefault:          DefaultMinLength,
		minLengthByLang: map[string]int{
			"ru": p.cfg.FilterMinLengthRu,
			"uk": p.cfg.FilterMinLengthUk,
			"en": p.cfg.FilterMinLengthEn,
		},
		summaryStripPhrasesDefault: parseSummaryStripPhrases(p.cfg.SummaryStripPhrases),
		summaryStripPhrasesByLang: map[string][]string{
			"ru": parseSummaryStripPhrases(p.cfg.SummaryStripPhrasesRu),
			"uk": parseSummaryStripPhrases(p.cfg.SummaryStripPhrasesUk),
			"en": parseSummaryStripPhrases(p.cfg.SummaryStripPhrasesEn),
		},
		domainAllowlist: parseDomainList(p.cfg.DomainAllowlist),
		domainDenylist:  parseDomainList(p.cfg.DomainDenylist),
	}

	p.getSetting(ctx, "worker_batch_size", &s.batchSize, logger)
	p.loadFilterSettings(ctx, s, logger)
	p.loadCoreSettings(ctx, s, logger)
	p.loadLinkSettings(ctx, s, logger)

	s.normalizeMinLengthSettings()
	s.normalizeSummarySettings()
	s.normalizeDedupWindows()
	s.normalizeLinkSettings()

	if s.bulletModeEnabled && s.batchSize > bulletLLMBatchSizeLimit {
		s.batchSize = bulletLLMBatchSizeLimit
	}

	return s, nil
}

func (p *Pipeline) getSetting(ctx context.Context, key string, target interface{}, logger zerolog.Logger) {
	if err := p.database.GetSetting(ctx, key, target); err != nil {
		logger.Debug().Err(err).Str("key", key).Msg("could not get setting from DB")
	}
}

func (p *Pipeline) getDurationSetting(ctx context.Context, key string, defaultVal time.Duration, logger zerolog.Logger) time.Duration {
	durationStr := defaultVal.String()
	p.getSetting(ctx, key, &durationStr, logger)

	if parsed, err := time.ParseDuration(durationStr); err == nil {
		return parsed
	}

	return defaultVal
}

func (p *Pipeline) loadFilterSettings(ctx context.Context, s *pipelineSettings, logger zerolog.Logger) {
	filterList, err := p.database.GetActiveFilters(ctx)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to get active filters")
	}

	s.filterList = filterList

	p.getSetting(ctx, "filters_ads", &s.adsFilterEnabled, logger)
	p.getSetting(ctx, "filters_min_length", &s.minLengthDefault, logger)
	p.getSetting(ctx, "filters_ads_keywords", &s.adsKeywords, logger)
	p.getSetting(ctx, "filters_skip_forwards", &s.skipForwards, logger)
	p.getSetting(ctx, "filters_mode", &s.filtersMode, logger)
	p.getSetting(ctx, "dedup_mode", &s.dedupMode, logger)
}

func (p *Pipeline) loadCoreSettings(ctx context.Context, s *pipelineSettings, logger zerolog.Logger) {
	p.getSetting(ctx, "topics_enabled", &s.topicsEnabled, logger)
	p.getSetting(ctx, "relevance_threshold", &s.relevanceThreshold, logger)
	p.getSetting(ctx, "digest_language", &s.digestLanguage, logger)
	p.getSetting(ctx, "vision_routing_enabled", &s.visionRoutingEnabled, logger)
	p.getSetting(ctx, "tiered_importance_enabled", &s.tieredImportanceEnabled, logger)
	p.getSetting(ctx, "digest_tone", &s.digestTone, logger)
	p.getSetting(ctx, "normalize_scores", &s.normalizeScores, logger)
	p.getSetting(ctx, "relevance_gate_enabled", &s.relevanceGateEnabled, logger)
	p.getSetting(ctx, "relevance_gate_mode", &s.relevanceGateMode, logger)
	p.getSetting(ctx, "relevance_gate_model", &s.relevanceGateModel, logger)
	p.getSetting(ctx, "bullet_mode_enabled", &s.bulletModeEnabled, logger)
	p.getSetting(ctx, "bullet_min_importance", &s.bulletMinImportance, logger)

	if s.normalizeScores {
		var err error

		s.channelStats, err = p.database.GetChannelStats(ctx)
		if err != nil {
			logger.Warn().Err(err).Msg("failed to fetch channel stats for normalization")

			s.normalizeScores = false
		}
	}
}

func (p *Pipeline) loadLinkSettings(ctx context.Context, s *pipelineSettings, logger zerolog.Logger) {
	p.getSetting(ctx, "link_enrichment_enabled", &s.linkEnrichmentEnabled, logger)
	p.getSetting(ctx, "link_enrichment_scope", &s.linkEnrichmentScope, logger)
	p.getSetting(ctx, "link_min_words", &s.linkMinWords, logger)
	p.getSetting(ctx, "link_snippet_max_chars", &s.linkSnippetMaxChars, logger)
	p.getSetting(ctx, "link_embedding_max_msg_len", &s.linkEmbeddingMaxMsgLen, logger)
	p.getSetting(ctx, "max_links_per_message", &s.maxLinks, logger)
	s.linkCacheTTL = p.getDurationSetting(ctx, "link_cache_ttl", p.cfg.LinkCacheTTL, logger)
	s.tgLinkCacheTTL = p.getDurationSetting(ctx, "tg_link_cache_ttl", p.cfg.TelegramLinkCacheTTL, logger)
}

func (p *Pipeline) markProcessed(ctx context.Context, logger zerolog.Logger, msgID string) {
	if err := p.database.MarkAsProcessed(ctx, msgID); err != nil {
		logger.Error().Str(LogFieldMsgID, msgID).Err(err).Msg(LogMsgFailedToMarkProcessed)
	}
}

// releaseClaimedMessage releases a claimed message so it can be retried.
// Used when processing fails due to transient errors like LLM timeouts.
func (p *Pipeline) releaseClaimedMessage(ctx context.Context, logger zerolog.Logger, msgID string) {
	if err := p.database.ReleaseClaimedMessage(ctx, msgID); err != nil {
		logger.Error().Str(LogFieldMsgID, msgID).Err(err).Msg("failed to release claimed message")
	}
}

func (p *Pipeline) saveItemError(ctx context.Context, logger zerolog.Logger, msgID string, errMsg string) {
	errJSON, _ := json.Marshal(map[string]string{"error": errMsg})

	if err := p.database.SaveItemError(ctx, msgID, errJSON); err != nil {
		logger.Warn().Str(LogFieldMsgID, msgID).Err(err).Msg("failed to save item error")
	}
}

func (p *Pipeline) recordDrop(ctx context.Context, logger zerolog.Logger, msgID, reason, detail string) {
	if reason == "" {
		return
	}

	if err := p.database.SaveRawMessageDropLog(ctx, msgID, reason, detail); err != nil {
		logger.Warn().Str(LogFieldMsgID, msgID).Err(err).Msg("failed to save drop log")
	}

	observability.DropsTotal.WithLabelValues(reason).Inc()
}

func (p *Pipeline) recordRelevanceGateDecision(ctx context.Context, logger zerolog.Logger, msgID string, decision gateDecision) {
	confidence := decision.confidence
	model := decision.model
	version := decision.version

	if model == "" {
		model = gateModelHeuristic
	}

	if version == "" {
		version = gateVersionHeuristic
	}

	if err := p.database.SaveRelevanceGateLog(ctx, msgID, decision.decision, &confidence, decision.reason, model, version); err != nil {
		logger.Warn().Str(LogFieldMsgID, msgID).Err(err).Msg("failed to save relevance gate log")
	}
}

func (p *Pipeline) prepareCandidates(ctx context.Context, logger zerolog.Logger, messages []db.RawMessage, s *pipelineSettings) ([]llm.MessageInput, map[string][]float32, error) {
	f := filters.New(s.filterList, s.adsFilterEnabled, s.minLengthDefault, s.adsKeywords, s.filtersMode)

	var deduplicator dedup.Deduplicator
	if s.dedupMode == DedupModeSemantic {
		deduplicator = dedup.NewSemantic(p.database, p.cfg.ClusterSimilarityThreshold, s.dedupWindow)
	} else {
		deduplicator = dedup.NewStrict(p.database)
	}

	var candidates []llm.MessageInput

	embeddings := make(map[string][]float32)
	seenHashes := make(map[string]string) // hash -> msg_id

	for _, m := range messages {
		if p.skipMessageBasic(ctx, logger, &m, s, seenHashes, f) {
			continue
		}

		// 1. Fetch links and channel context early
		candidate := p.enrichMessage(ctx, logger, m, s)

		// 2. Advanced filters (Relevance Gate) using augmented text
		if p.skipMessageAdvanced(ctx, logger, &candidate, s) {
			continue
		}

		// 3. Deduplication
		_, skip := p.handleDeduplication(ctx, logger, &candidate, s, candidates, embeddings, deduplicator)
		if skip {
			continue
		}

		candidates = append(candidates, candidate)
		seenHashes[m.CanonicalHash] = m.ID
	}

	return candidates, embeddings, nil
}

func (p *Pipeline) skipMessageBasic(ctx context.Context, logger zerolog.Logger, m *db.RawMessage, s *pipelineSettings, seenHashes map[string]string, f *filters.Filterer) bool {
	if p.skipBatchDuplicate(ctx, logger, m, seenHashes) {
		return true
	}

	previewText := previewTextFromMessage(m)
	filterText, stripped := p.prepareFilterText(m.Text, previewText)

	if p.skipEmptyContent(ctx, logger, m, s, filterText, previewText, stripped) {
		return true
	}

	if p.skipByContentFilters(ctx, logger, m, filterText, previewText) {
		return true
	}

	m.Text = filterText

	return p.skipByLengthAndFilterer(ctx, logger, m, s, filterText, previewText, f)
}

func (p *Pipeline) skipBatchDuplicate(ctx context.Context, logger zerolog.Logger, m *db.RawMessage, seenHashes map[string]string) bool {
	dupID, seen := seenHashes[m.CanonicalHash]
	if !seen {
		return false
	}

	logger.Info().Str(LogFieldMsgID, m.ID).Str(LogFieldDuplicateID, dupID).Msg("skipping strict duplicate in batch")
	p.recordDrop(ctx, logger, m.ID, dropReasonDuplicateBatch, dupID)
	p.markProcessed(ctx, logger, m.ID)

	return true
}

func (p *Pipeline) prepareFilterText(text, previewText string) (string, bool) {
	filterText := combinePreviewText(text, previewText)
	filterText, stripped := filters.StripFooterBoilerplate(filterText)

	if previewText != "" && len(filterText) < domain.ShortMessageThreshold && !strings.Contains(filterText, previewText) {
		filterText = strings.TrimSpace(filterText + "\n\n" + previewText)
	}

	return filterText, stripped
}

func (p *Pipeline) skipEmptyContent(ctx context.Context, logger zerolog.Logger, m *db.RawMessage, s *pipelineSettings, filterText, _ string, stripped bool) bool {
	if stripped && strings.TrimSpace(filterText) == "" {
		p.recordDrop(ctx, logger, m.ID, filters.ReasonBoilerplate, "footer_only")
		p.markProcessed(ctx, logger, m.ID)

		return true
	}

	if m.IsForward && strings.TrimSpace(filterText) == "" {
		p.recordDrop(ctx, logger, m.ID, filters.ReasonForwardShell, "")
		p.markProcessed(ctx, logger, m.ID)

		return true
	}

	if s.skipForwards && m.IsForward {
		logger.Info().Str(LogFieldMsgID, m.ID).Msg("skipping forwarded message")
		p.recordDrop(ctx, logger, m.ID, dropReasonForwarded, "")
		p.markProcessed(ctx, logger, m.ID)

		return true
	}

	return false
}

func (p *Pipeline) skipByContentFilters(ctx context.Context, logger zerolog.Logger, m *db.RawMessage, filterText, previewText string) bool {
	if filters.IsEmojiOnly(filterText) && !hasLinkOrPreview(m, previewText) {
		p.recordDrop(ctx, logger, m.ID, filters.ReasonEmojiOnly, "")
		p.markProcessed(ctx, logger, m.ID)

		return true
	}

	if filters.IsBoilerplateOnly(filterText) {
		p.recordDrop(ctx, logger, m.ID, filters.ReasonBoilerplate, "cta_only")
		p.markProcessed(ctx, logger, m.ID)

		return true
	}

	return false
}

func (p *Pipeline) skipByLengthAndFilterer(ctx context.Context, logger zerolog.Logger, m *db.RawMessage, s *pipelineSettings, filterText, previewText string, f *filters.Filterer) bool {
	lang := detectLanguageForFilter(filterText, previewText)
	minLength := s.minLengthForLanguage(lang)
	hasLinks := hasLinkOrPreview(m, previewText)

	if len(filterText) < minLength && !hasLinks {
		p.recordDrop(ctx, logger, m.ID, filters.ReasonMinLength, "")
		p.markProcessed(ctx, logger, m.ID)

		return true
	}

	effectiveMinLength := minLength
	if len(filterText) < minLength && hasLinks {
		effectiveMinLength = 0
	}

	if filtered, reason := f.FilterReasonWithMinLength(m.Text, effectiveMinLength); filtered {
		p.recordDrop(ctx, logger, m.ID, reason, "")
		p.markProcessed(ctx, logger, m.ID)

		return true
	}

	return false
}

func (p *Pipeline) skipMessageAdvanced(ctx context.Context, logger zerolog.Logger, c *llm.MessageInput, s *pipelineSettings) bool {
	if s.relevanceGateEnabled {
		text := p.augmentTextWithLinks(c, s, domain.ScopeRelevance)
		decision := p.evaluateRelevanceGate(ctx, logger, text, s)
		p.recordRelevanceGateDecision(ctx, logger, c.ID, decision)

		if decision.decision == DecisionIrrelevant {
			logger.Info().Str(LogFieldMsgID, c.ID).Str("reason", decision.reason).Msg("skipping message by relevance gate")
			p.recordDrop(ctx, logger, c.ID, dropReasonRelevanceGate, decision.reason)
			p.markProcessed(ctx, logger, c.ID)

			return true
		}
	}

	return false
}

func (p *Pipeline) handleDeduplication(ctx context.Context, logger zerolog.Logger, c *llm.MessageInput, s *pipelineSettings, candidates []llm.MessageInput, embeddings map[string][]float32, deduplicator dedup.Deduplicator) ([]float32, bool) {
	emb, skip := p.generateEmbeddingIfNeeded(ctx, logger, c, s, embeddings)
	if skip {
		return nil, true
	}

	if p.checkBatchDuplicate(ctx, logger, &c.RawMessage, s, candidates, embeddings, emb) {
		return nil, true
	}

	if p.checkSameChannelDuplicate(ctx, logger, &c.RawMessage, s, emb) {
		return nil, true
	}

	if p.checkGlobalDuplicate(ctx, logger, &c.RawMessage, s, emb, deduplicator) {
		return nil, true
	}

	return emb, false
}

func (p *Pipeline) generateEmbeddingIfNeeded(ctx context.Context, logger zerolog.Logger, c *llm.MessageInput, s *pipelineSettings, embeddings map[string][]float32) ([]float32, bool) {
	if s.dedupMode != DedupModeSemantic && !s.topicsEnabled {
		return nil, false
	}

	text := c.Text
	if strings.Contains(s.linkEnrichmentScope, domain.ScopeDedup) && len(c.ResolvedLinks) > 0 {
		if len(c.Text) < s.linkEmbeddingMaxMsgLen {
			// Message is short: use message + link snippet
			text = p.augmentTextWithLinks(c, s, domain.ScopeDedup)
		} else {
			// Message is long: use title/domain but prioritize message
			var sb strings.Builder
			sb.WriteString(c.Text)
			sb.WriteString("\nRef: ")

			for _, link := range c.ResolvedLinks {
				sb.WriteString(fmt.Sprintf("%s (%s) ", link.Title, link.Domain))
			}

			text = sb.String()
		}
	}

	emb, err := p.embeddingClient.GetEmbedding(ctx, text)
	if err != nil {
		logger.Error().Str(LogFieldMsgID, c.ID).Err(err).Msg("failed to get embedding")

		return nil, true
	}

	embeddings[c.ID] = emb

	return emb, false
}

func (p *Pipeline) checkBatchDuplicate(ctx context.Context, logger zerolog.Logger, m *db.RawMessage, s *pipelineSettings, candidates []llm.MessageInput, embeddings map[string][]float32, emb []float32) bool {
	if s.dedupMode != DedupModeSemantic {
		return false
	}

	for _, cand := range candidates {
		if dedup.CosineSimilarity(embeddings[cand.ID], emb) > p.cfg.ClusterSimilarityThreshold {
			logger.Info().Str(LogFieldMsgID, m.ID).Str(LogFieldDuplicateID, cand.ID).Msg("skipping semantic duplicate in batch")
			p.recordDrop(ctx, logger, m.ID, dropReasonDedupSemanticBatch, cand.ID)
			p.markProcessed(ctx, logger, m.ID)

			return true
		}
	}

	return false
}

func (p *Pipeline) checkSameChannelDuplicate(ctx context.Context, logger zerolog.Logger, m *db.RawMessage, s *pipelineSettings, emb []float32) bool {
	if s.dedupMode != DedupModeSemantic || s.dedupSameChannelWindow <= 0 || len(emb) == 0 {
		return false
	}

	if m.ChannelID == "" {
		return false
	}

	minCreatedAt := time.Now().Add(-s.dedupSameChannelWindow)
	if !m.TGDate.IsZero() {
		minCreatedAt = m.TGDate.Add(-s.dedupSameChannelWindow)
	}

	threshold := p.cfg.ClusterSimilarityThreshold
	if threshold < 0.85 {
		threshold = 0.85
	}

	dupID, err := p.database.FindSimilarItemForChannel(ctx, emb, m.ChannelID, threshold, minCreatedAt)
	if err != nil {
		logger.Error().Str(LogFieldMsgID, m.ID).Err(err).Msg("failed to check same-channel duplicates")
		return false
	}

	if dupID == "" {
		return false
	}

	logger.Info().Str(LogFieldMsgID, m.ID).Str(LogFieldDuplicateID, dupID).Msg("skipping same-channel near-duplicate")
	p.recordDrop(ctx, logger, m.ID, dropReasonDedupSemanticSame, dupID)
	p.markProcessed(ctx, logger, m.ID)

	return true
}

func (p *Pipeline) checkGlobalDuplicate(ctx context.Context, logger zerolog.Logger, m *db.RawMessage, s *pipelineSettings, emb []float32, deduplicator dedup.Deduplicator) bool {
	isDup, dupID, dErr := deduplicator.IsDuplicate(ctx, *m, emb)
	if dErr != nil {
		logger.Error().Str(LogFieldMsgID, m.ID).Err(dErr).Msg("failed to check for duplicates")

		return false
	}

	if !isDup {
		return false
	}

	logger.Info().Str(LogFieldMsgID, m.ID).Str(LogFieldDuplicateID, dupID).Msg("skipping duplicate message")

	reason := dropReasonDedupStrictGlobal
	if s.dedupMode == DedupModeSemantic {
		reason = dropReasonDedupSemanticGlobal
	}

	p.recordDrop(ctx, logger, m.ID, reason, dupID)
	p.markProcessed(ctx, logger, m.ID)

	return true
}

func (p *Pipeline) enrichMessage(ctx context.Context, logger zerolog.Logger, m db.RawMessage, s *pipelineSettings) llm.MessageInput {
	channelCtx, cErr := p.database.GetRecentMessagesForChannel(ctx, m.ChannelID, m.TGDate, DefaultChannelContextLimit)
	if cErr != nil {
		logger.Warn().Err(cErr).Str(LogFieldMsgID, m.ID).Msg("failed to fetch channel context")
	}

	resolvedLinks, eErr := p.enrichWithLinks(ctx, &m, s.linkEnrichmentEnabled, s.maxLinks, s.linkCacheTTL, s.tgLinkCacheTTL)
	if eErr != nil {
		logger.Warn().Err(eErr).Str(LogFieldMsgID, m.ID).Msg("link enrichment failed")
	}

	// Seed external links into crawler queue (non-blocking, opportunistic)
	p.seedLinksForCrawler(ctx, logger, m)

	return llm.MessageInput{
		RawMessage:    m,
		Context:       channelCtx,
		ResolvedLinks: resolvedLinks,
	}
}

func (p *Pipeline) runLLMProcessing(ctx context.Context, logger zerolog.Logger, candidates []llm.MessageInput, s *pipelineSettings) ([]llm.BatchResult, error) {
	start := time.Now()

	results := make([]llm.BatchResult, len(candidates))
	cached := make([]bool, len(candidates))
	digestLang := normalizeLanguage(s.digestLanguage)

	p.loadCachedSummaries(ctx, logger, candidates, results, cached, digestLang, s.summaryCachePromptVersion)

	// Group indices by model for Vision Routing
	modelGroups := p.groupIndicesByModel(candidates, cached)

	for model, indices := range modelGroups {
		if err := p.processModelBatch(ctx, logger, candidates, results, model, indices, s); err != nil {
			return nil, err
		}
	}

	// 2.1 Tiered Importance Analysis
	p.performTieredImportanceAnalysis(ctx, logger, candidates, results, cached, s)

	logger.Info().Int(LogFieldCount, len(candidates)).Dur("duration", time.Since(start)).Msg("LLM processing finished")

	return results, nil
}

func (p *Pipeline) loadCachedSummaries(ctx context.Context, logger zerolog.Logger, candidates []llm.MessageInput, results []llm.BatchResult, cached []bool, digestLang, promptVersion string) {
	for i, candidate := range candidates {
		entry, ok := p.lookupSummaryCache(ctx, logger, candidate, digestLang, promptVersion)
		if !ok {
			continue
		}

		results[i] = llm.BatchResult{
			Index:           i,
			RelevanceScore:  entry.RelevanceScore,
			ImportanceScore: entry.ImportanceScore,
			Topic:           entry.Topic,
			Summary:         entry.Summary,
			Language:        entry.Language,
		}
		cached[i] = true
	}
}

func (p *Pipeline) lookupSummaryCache(ctx context.Context, logger zerolog.Logger, candidate llm.MessageInput, digestLang, promptVersion string) (*db.SummaryCacheEntry, bool) {
	cacheKey := summaryCacheKey(candidate, promptVersion)
	if cacheKey == "" {
		return nil, false
	}

	entry, err := p.database.GetSummaryCache(ctx, cacheKey, digestLang)
	if errors.Is(err, db.ErrSummaryCacheNotFound) && previewTextForCache(candidate) == "" {
		entry, err = p.database.GetSummaryCache(ctx, candidate.CanonicalHash, digestLang)
	}

	if err != nil {
		if !errors.Is(err, db.ErrSummaryCacheNotFound) {
			logger.Warn().Err(err).Str(LogFieldMsgID, candidate.ID).Msg("failed to load summary cache")
		}

		return nil, false
	}

	if strings.TrimSpace(entry.Summary) == "" {
		return nil, false
	}

	if summaryCacheMaxAgeDays > 0 {
		maxAge := time.Duration(summaryCacheMaxAgeDays) * hoursPerDay * time.Hour
		if time.Since(entry.UpdatedAt) > maxAge {
			return nil, false
		}
	}

	return entry, true
}

func (p *Pipeline) groupIndicesByModel(candidates []llm.MessageInput, cached []bool) map[string][]int {
	modelGroups := make(map[string][]int)

	// Use empty string to let the LLM registry handle task-specific model selection
	// via LLM_SUMMARIZE_MODEL env var or default task config
	for i := range candidates {
		if len(cached) > i && cached[i] {
			continue
		}

		modelGroups[""] = append(modelGroups[""], i)
	}

	return modelGroups
}

func (p *Pipeline) processModelBatch(ctx context.Context, logger zerolog.Logger, candidates []llm.MessageInput, results []llm.BatchResult, model string, indices []int, s *pipelineSettings) error {
	groupCandidates := make([]llm.MessageInput, len(indices))
	for j, idx := range indices {
		candidate := candidates[idx]
		candidate.Text = p.augmentTextForLLM(candidate, s)
		groupCandidates[j] = candidate
	}

	// Use timeout context for LLM call to prevent indefinite hangs
	llmCtx, cancel := context.WithTimeout(ctx, LLMBatchTimeout)
	defer cancel()

	llmStart := time.Now()

	groupResults, err := p.llmClient.ProcessBatch(llmCtx, groupCandidates, s.digestLanguage, model, s.digestTone)
	if err != nil {
		logger.Error().Err(err).Str(LogFieldModel, model).Msg("LLM batch processing failed")
		observability.PipelineProcessed.WithLabelValues(StatusError).Add(float64(len(indices)))

		// Release claimed messages so they can be retried by another worker.
		// This handles transient errors like LLM timeouts.
		for _, idx := range indices {
			p.releaseClaimedMessage(ctx, logger, candidates[idx].ID)
		}

		return fmt.Errorf("LLM batch processing: %w", err)
	}

	observability.LLMRequestDuration.WithLabelValues(model).Observe(time.Since(llmStart).Seconds())

	if len(groupResults) != len(indices) {
		logger.Warn().Int("expected", len(indices)).Int("actual", len(groupResults)).Str(LogFieldModel, model).Msg("LLM batch size mismatch, results might be misaligned")
	}

	for j, idx := range indices {
		if j < len(groupResults) {
			results[idx] = groupResults[j]
		}
	}

	return nil
}

func (p *Pipeline) augmentTextForLLM(c llm.MessageInput, _ *pipelineSettings) string {
	return c.Text
}

func (p *Pipeline) performTieredImportanceAnalysis(ctx context.Context, logger zerolog.Logger, candidates []llm.MessageInput, results []llm.BatchResult, cached []bool, s *pipelineSettings) {
	if !s.tieredImportanceEnabled {
		return
	}

	tieredIndices, tieredCandidates := selectTieredCandidates(candidates, results, cached)
	if len(tieredCandidates) == 0 {
		return
	}

	logger.Info().Int(LogFieldCount, len(tieredCandidates)).Msg("Performing tiered importance analysis")

	llmStart := time.Now()

	// Pass empty model to let the LLM registry handle task-specific model selection
	tieredResults, err := p.llmClient.ProcessBatch(ctx, tieredCandidates, s.digestLanguage, "", s.digestTone)
	if err != nil {
		logger.Warn().Err(err).Msg("Tiered importance analysis failed, keeping original results")

		return
	}

	if len(tieredResults) != len(tieredCandidates) {
		return
	}

	observability.LLMRequestDuration.WithLabelValues("summarize").Observe(time.Since(llmStart).Seconds())
	applyTieredResults(results, tieredResults, tieredIndices)
}

func selectTieredCandidates(candidates []llm.MessageInput, results []llm.BatchResult, cached []bool) ([]int, []llm.MessageInput) {
	var tieredIndices []int

	var tieredCandidates []llm.MessageInput

	for i, res := range results {
		if len(cached) > i && cached[i] {
			continue
		}

		if res.ImportanceScore > TieredImportanceThreshold {
			tieredIndices = append(tieredIndices, i)
			tieredCandidates = append(tieredCandidates, candidates[i])
		}
	}

	return tieredIndices, tieredCandidates
}

func applyTieredResults(results []llm.BatchResult, tieredResults []llm.BatchResult, tieredIndices []int) {
	for j, idx := range tieredIndices {
		results[idx] = tieredResults[j]
	}
}

func (p *Pipeline) storeResults(ctx context.Context, logger zerolog.Logger, candidates []llm.MessageInput, results []llm.BatchResult, embeddings map[string][]float32, s *pipelineSettings) error {
	p.normalizeResults(candidates, results, s)

	channelBiases := p.loadChannelBias(ctx, logger)

	readyCount := 0
	rejectedCount := 0

	for i, res := range results {
		lang, langSource := resolveItemLanguage(candidates[i], res)
		res.Language = lang
		stripPhrases := s.summaryStripPhrasesFor(lang)

		res.Summary = postProcessSummary(res.Summary, stripPhrases)
		p.applyCanonicalSummary(ctx, logger, candidates[i], &res, stripPhrases, s)
		res.Summary = p.tryFallbackSummary(res.Summary, candidates[i].Text, stripPhrases)

		if res.Summary == "" {
			p.handleEmptySummary(ctx, logger, candidates[i].ID, i)

			continue
		}

		extractedBullets, bulletSummary := p.processBullets(ctx, logger, candidates[i], &res, s)
		bias := p.applyChannelBias(candidates[i], &res, channelBiases)
		forceReject, _ := p.applyIrrelevantSuppression(ctx, logger, candidates[i].ID, embeddings[candidates[i].ID], &res)

		item := p.createItem(logger, candidates[i], res, bias, s)
		item.Language = lang
		item.LanguageSource = langSource
		p.applyBulletMetadata(item, extractedBullets, bulletSummary)

		if forceReject {
			item.Status = StatusRejected
		}

		if item.Status == StatusReady {
			p.finalizeReadyItem(ctx, logger, candidates[i].ID, item, s)
		}

		ready, rejected := p.storeAndCount(ctx, logger, candidates[i], item, embeddings, extractedBullets, s)
		readyCount += ready
		rejectedCount += rejected
	}

	logger.Info().Int("ready", readyCount).Int("rejected", rejectedCount).Msg("Batch results stored")

	return nil
}

func (p *Pipeline) tryFallbackSummary(summary, text string, stripPhrases []string) string {
	if summary != "" && !isWeakSummary(summary) {
		return summary
	}

	lead := selectLeadSentence(text)
	if lead != "" && !isMostlySymbols(lead) {
		return postProcessSummary(lead, stripPhrases)
	}

	return summary
}

func (p *Pipeline) applyCanonicalSummary(ctx context.Context, logger zerolog.Logger, candidate llm.MessageInput, res *llm.BatchResult, stripPhrases []string, s *pipelineSettings) {
	if res == nil || s == nil {
		return
	}

	canonicalURL := p.selectTrustedCanonicalURL(candidate, s)
	if canonicalURL == "" {
		return
	}

	observability.CanonicalSourceDetectedTotal.Inc()

	if !isWeakSummary(res.Summary) {
		return
	}

	canonicalItem, err := p.database.GetItemByCanonicalURL(ctx, canonicalURL, candidate.ID)
	if err != nil {
		logger.Warn().Err(err).Str(LogFieldMsgID, candidate.ID).Msg("failed to resolve canonical item")
		return
	}

	if canonicalItem == nil || strings.TrimSpace(canonicalItem.Summary) == "" {
		return
	}

	res.Summary = postProcessSummary(canonicalItem.Summary, stripPhrases)
	if canonicalItem.Topic != "" {
		res.Topic = canonicalItem.Topic
	}

	if canonicalItem.Language != "" {
		res.Language = canonicalItem.Language
	}

	logger.Info().
		Str(LogFieldMsgID, candidate.ID).
		Str("canonical_url", canonicalURL).
		Str("canonical_item_id", canonicalItem.ItemID).
		Msg("reused canonical item summary")
}

func (p *Pipeline) selectTrustedCanonicalURL(candidate llm.MessageInput, s *pipelineSettings) string {
	if len(candidate.ResolvedLinks) == 0 {
		return ""
	}

	linkCfg := linkscore.LinkContextConfig{
		PrimaryMinWords:      s.linkPrimaryMinWords,
		PrimaryShortMsgChars: s.linkPrimaryShortMsgChars,
		PrimaryAllowlist:     s.linkPrimaryAllowlist,
		PrimaryCTATerms:      s.linkPrimaryCTATerms,
		PrimaryMaxLinks:      s.linkPrimaryMaxLinks,
		DonationDenylist:     s.linkPrimaryDonationDeny,
	}

	primary, supplemental := linkscore.SelectLinkContexts(candidate.Text, candidate.PreviewText, candidate.ResolvedLinks, linkCfg)
	candidates := buildCanonicalLinkCandidates(candidate.ResolvedLinks, primary, supplemental)

	for _, link := range candidates {
		if canonicalURL, _ := linkscore.TrustedCanonical(link, s.linkCanonicalAllowlist, s.linkCanonicalTrusted, s.linkCanonicalDenylist); canonicalURL != "" {
			return canonicalURL
		}
	}

	return ""
}

func buildCanonicalLinkCandidates(links []domain.ResolvedLink, primary, supplemental *linkscore.LinkContext) []domain.ResolvedLink {
	if len(links) == 0 {
		return nil
	}

	urls := make(map[string]struct{})
	if primary != nil && primary.URL != "" {
		urls[primary.URL] = struct{}{}
	}

	if supplemental != nil && supplemental.URL != "" {
		urls[supplemental.URL] = struct{}{}
	}

	if len(urls) == 0 {
		return nil
	}

	candidates := make([]domain.ResolvedLink, 0, len(urls))
	for _, link := range links {
		if _, ok := urls[link.URL]; ok {
			candidates = append(candidates, link)
		}
	}

	return candidates
}

func (p *Pipeline) processBullets(ctx context.Context, logger zerolog.Logger, candidate llm.MessageInput, res *llm.BatchResult, s *pipelineSettings) ([]llm.ExtractedBullet, bulletScoreSummary) {
	if !s.bulletModeEnabled {
		return nil, bulletScoreSummary{}
	}

	extractedBullets := p.extractBullets(ctx, logger, candidate, res.Summary, s.digestLanguage, s)
	if len(extractedBullets) == 0 {
		return nil, bulletScoreSummary{}
	}

	messageText := strings.TrimSpace(candidate.Text)
	if messageText == "" {
		messageText = strings.TrimSpace(candidate.PreviewText)
	}

	extractedBullets = applyBulletLengthRules(extractedBullets, messageText, candidate.ResolvedLinks, candidate.PreviewText)
	if len(extractedBullets) == 0 {
		return nil, bulletScoreSummary{}
	}

	bulletSummary := summarizeBullets(extractedBullets, s.bulletMinImportance)
	res.RelevanceScore = bulletSummary.maxRelevance
	res.ImportanceScore = bulletSummary.maxImportance

	return extractedBullets, bulletSummary
}

func (p *Pipeline) applyBulletMetadata(item *db.Item, extractedBullets []llm.ExtractedBullet, bulletSummary bulletScoreSummary) {
	if len(extractedBullets) == 0 {
		return
	}

	if bulletSummary.includedCount == 0 {
		item.Status = StatusRejected
	}

	item.BulletTotalCount = len(extractedBullets)
	item.BulletIncludedCount = bulletSummary.includedCount
}

func (p *Pipeline) finalizeReadyItem(ctx context.Context, logger zerolog.Logger, msgID string, item *db.Item, s *pipelineSettings) {
	detectedLang := detectSummaryLanguage(item.Summary, item.Language)
	item.Summary = p.translateSummaryIfNeeded(ctx, logger, msgID, item.Summary, detectedLang, s)
	targetLang := normalizeLanguage(s.digestLanguage)
	item.Summary = postProcessSummary(item.Summary, s.summaryStripPhrasesFor(targetLang))
}

func (p *Pipeline) storeAndCount(ctx context.Context, logger zerolog.Logger, candidate llm.MessageInput, item *db.Item, embeddings map[string][]float32, extractedBullets []llm.ExtractedBullet, s *pipelineSettings) (ready, rejected int) {
	if !p.saveAndMarkProcessed(ctx, logger, candidate, item, embeddings, extractedBullets, s.digestLanguage, s.summaryCachePromptVersion) {
		return 0, 0
	}

	if item.Status == StatusReady {
		return 1, 0
	}

	return 0, 1
}

func (p *Pipeline) normalizeResults(candidates []llm.MessageInput, results []llm.BatchResult, s *pipelineSettings) {
	if !s.normalizeScores || s.channelStats == nil {
		return
	}

	for i := range results {
		if results[i].Summary == "" {
			continue
		}

		stats, ok := s.channelStats[candidates[i].ChannelID]
		if ok {
			if stats.StddevRelevance > NormalizationStddevMinimum {
				results[i].RelevanceScore = (results[i].RelevanceScore - stats.AvgRelevance) / stats.StddevRelevance
			}

			if stats.StddevImportance > NormalizationStddevMinimum {
				results[i].ImportanceScore = (results[i].ImportanceScore - stats.AvgImportance) / stats.StddevImportance
			}
		}
	}
}

func (p *Pipeline) translateSummaryIfNeeded(ctx context.Context, logger zerolog.Logger, msgID string, summary string, detectedLang string, s *pipelineSettings) string {
	targetLang := normalizeLanguage(s.digestLanguage)
	if !summaryNeedsTranslation(summary, detectedLang, targetLang) {
		return summary
	}

	// Pass empty model to let the LLM registry handle task-specific model selection
	// via LLM_TRANSLATE_MODEL env var or default task config
	translated, err := p.llmClient.TranslateText(ctx, summary, targetLang, "")
	if err != nil {
		logger.Warn().Err(err).Str(LogFieldMsgID, msgID).Msg("failed to translate summary")
		return summary
	}

	if strings.TrimSpace(translated) == "" {
		return summary
	}

	return translated
}

func summaryNeedsTranslation(summary string, detectedLang string, targetLang string) bool {
	if strings.TrimSpace(summary) == "" || targetLang == "" {
		return false
	}

	normalizedDetected := normalizeLanguage(detectedLang)
	if normalizedDetected != "" && normalizedDetected != targetLang {
		return true
	}

	return targetLang == "ru" && containsUkrainianLetters(summary)
}

func containsUkrainianLetters(text string) bool {
	for _, r := range text {
		switch r {
		case '\u0404', '\u0454', '\u0406', '\u0456', '\u0407', '\u0457', '\u0490', '\u0491':
			return true
		}
	}

	return false
}

func (p *Pipeline) handleEmptySummary(ctx context.Context, logger zerolog.Logger, msgID string, index int) {
	logger.Warn().Str(LogFieldMsgID, msgID).Int("index", index).Msg("LLM summary empty for item, marking as error")
	observability.PipelineProcessed.WithLabelValues(StatusError).Inc()

	p.saveItemError(ctx, logger, msgID, "empty summary from LLM")
	p.markProcessed(ctx, logger, msgID)
}

func (p *Pipeline) createItem(logger zerolog.Logger, c llm.MessageInput, res llm.BatchResult, bias float32, s *pipelineSettings) *db.Item {
	importance := p.calculateImportance(logger, c, res, bias, s)
	status := p.determineStatus(c, res.RelevanceScore, s)

	return &db.Item{
		RawMessageID:    c.ID,
		RelevanceScore:  res.RelevanceScore,
		ImportanceScore: importance,
		Topic:           res.Topic,
		Summary:         res.Summary,
		Language:        res.Language,
		Status:          status,
	}
}

func (p *Pipeline) calculateImportance(logger zerolog.Logger, c llm.MessageInput, res llm.BatchResult, bias float32, s *pipelineSettings) float32 {
	channelWeight := c.ImportanceWeight
	if channelWeight < MinChannelWeight {
		channelWeight = MaxImportanceScore
	} else if channelWeight > MaxChannelWeight {
		channelWeight = MaxChannelWeight
	}

	importance := res.ImportanceScore * channelWeight
	if importance > MaxImportanceScore {
		importance = MaxImportanceScore
	}

	if !p.hasUniqueInfo(res.Summary) {
		importance -= UniqueInfoPenalty
		if importance < 0 {
			importance = 0
		}

		logger.Debug().Str(LogFieldMsgID, c.ID).Msg("Applied penalty for lack of unique info")
	}

	importance = applyDomainBias(importance, c, s)

	if bias != 0 {
		importance = clampScore(importance + bias)
	}

	return importance
}

func (p *Pipeline) determineStatus(c llm.MessageInput, relevanceScore float32, s *pipelineSettings) string {
	itemRelThreshold := s.relevanceThreshold
	if c.RelevanceThreshold > 0 {
		itemRelThreshold = c.RelevanceThreshold
	}

	if c.AutoRelevanceEnabled {
		itemRelThreshold += c.RelevanceThresholdDelta
	}

	if itemRelThreshold < 0 {
		itemRelThreshold = 0
	} else if itemRelThreshold > 1 {
		itemRelThreshold = 1
	}

	if relevanceScore < itemRelThreshold {
		return StatusRejected
	}

	return StatusReady
}

func (p *Pipeline) saveAndMarkProcessed(ctx context.Context, logger zerolog.Logger, c llm.MessageInput, item *db.Item, embeddings map[string][]float32, extractedBullets []llm.ExtractedBullet, digestLanguage, promptVersion string) bool {
	if err := p.database.SaveItem(ctx, item); err != nil {
		logger.Error().Str(LogFieldMsgID, c.ID).Err(err).Msg("failed to save item")
		observability.PipelineProcessed.WithLabelValues(StatusError).Inc()

		p.saveItemError(ctx, logger, c.ID, fmt.Sprintf("failed to save item: %v", err))
		p.markProcessed(ctx, logger, c.ID)

		return false
	}

	observability.PipelineProcessed.WithLabelValues(item.Status).Inc()

	// Save embedding
	emb := embeddings[c.ID]
	if len(emb) > 0 {
		if err := p.database.SaveEmbedding(ctx, item.ID, emb); err != nil {
			logger.Error().Str(LogFieldItemID, item.ID).Err(err).Msg("failed to save embedding")
		}
	}

	if item.Status == StatusReady && len(extractedBullets) > 0 {
		p.storeBullets(ctx, logger, extractedBullets, item)
		logger.Debug().Str(LogFieldItemID, item.ID).Int(LogFieldCount, len(extractedBullets)).Msg("bullets stored")
	}

	if err := p.database.MarkAsProcessed(ctx, c.ID); err != nil {
		logger.Error().Str(LogFieldMsgID, c.ID).Err(err).Msg(LogMsgFailedToMarkProcessed)
	}

	cacheKey := summaryCacheKey(c, promptVersion)
	p.upsertSummaryCache(ctx, logger, cacheKey, digestLanguage, item)

	p.enqueueFactCheck(ctx, logger, item)
	p.enqueueEnrichment(ctx, logger, item)

	return true
}

func (p *Pipeline) enqueueFactCheck(ctx context.Context, logger zerolog.Logger, item *db.Item) {
	if !p.factCheckEnabled(item) {
		return
	}

	claim, normalized, ok := p.buildFactCheckClaim(item)
	if !ok {
		return
	}

	if !p.factCheckQueueHasCapacity(ctx, logger) {
		return
	}

	if err := p.database.EnqueueFactCheck(ctx, item.ID, claim, normalized); err != nil {
		logger.Warn().Err(err).Str(LogFieldItemID, item.ID).Msg("failed to enqueue fact check")
	}
}

func (p *Pipeline) factCheckEnabled(item *db.Item) bool {
	if !p.cfg.FactCheckGoogleEnabled || p.cfg.FactCheckGoogleAPIKey == "" {
		return false
	}

	return item.Status == StatusReady
}

func (p *Pipeline) buildFactCheckClaim(item *db.Item) (string, string, bool) {
	minLen := p.cfg.FactCheckMinClaimLength
	if minLen <= 0 {
		minLen = factcheck.DefaultMinClaimLength
	}

	claim := factcheck.BuildClaimFromSummary(item.Summary)
	if len(claim) < minLen {
		return "", "", false
	}

	normalized := factcheck.NormalizeClaim(claim)
	if normalized == "" {
		return "", "", false
	}

	return claim, normalized, true
}

func (p *Pipeline) factCheckQueueHasCapacity(ctx context.Context, logger zerolog.Logger) bool {
	if p.cfg.FactCheckQueueMax <= 0 {
		return true
	}

	pending, err := p.database.CountPendingFactChecks(ctx)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to count fact check queue")
		return true
	}

	return pending < p.cfg.FactCheckQueueMax
}

func (p *Pipeline) upsertSummaryCache(ctx context.Context, logger zerolog.Logger, cacheKey, digestLanguage string, item *db.Item) {
	if cacheKey == "" || item == nil || strings.TrimSpace(item.Summary) == "" {
		return
	}

	entry := &db.SummaryCacheEntry{
		CanonicalHash:   cacheKey,
		DigestLanguage:  normalizeLanguage(digestLanguage),
		Summary:         item.Summary,
		Topic:           item.Topic,
		Language:        item.Language,
		RelevanceScore:  item.RelevanceScore,
		ImportanceScore: item.ImportanceScore,
	}

	if err := p.database.UpsertSummaryCache(ctx, entry); err != nil {
		logger.Warn().Err(err).Str(LogFieldMsgID, item.RawMessageID).Msg("failed to upsert summary cache")
	}
}

func previewTextForCache(c llm.MessageInput) string {
	preview := strings.TrimSpace(c.PreviewText)
	if preview == "" {
		preview = extractPreviewText(c.MediaJSON)
	}

	return preview
}

func summaryCacheKey(c llm.MessageInput, promptVersion string) string {
	base := strings.TrimSpace(c.CanonicalHash)
	if base == "" {
		return ""
	}

	version := strings.TrimSpace(promptVersion)
	if version == "" {
		version = defaultPromptVersion
	}

	preview := previewTextForCache(c)
	if preview == "" {
		return base + ":" + version
	}

	hash := sha256.Sum256([]byte(preview))

	return base + ":" + version + ":" + hex.EncodeToString(hash[:])
}

func (p *Pipeline) enqueueEnrichment(ctx context.Context, logger zerolog.Logger, item *db.Item) {
	if !p.enrichmentEnabled(item) {
		return
	}

	if !p.enrichmentQueueHasCapacity(ctx, logger) {
		return
	}

	if err := p.database.EnqueueEnrichment(ctx, item.ID, item.Summary); err != nil {
		logger.Warn().Err(err).Str(LogFieldItemID, item.ID).Msg("failed to enqueue enrichment")
	}
}

func (p *Pipeline) enrichmentEnabled(item *db.Item) bool {
	if !p.cfg.EnrichmentEnabled {
		return false
	}

	return item.Status == StatusReady && item.Summary != ""
}

func (p *Pipeline) enrichmentQueueHasCapacity(ctx context.Context, logger zerolog.Logger) bool {
	if p.cfg.EnrichmentQueueMax <= 0 {
		return true
	}

	pending, err := p.database.CountPendingEnrichments(ctx)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to count enrichment queue")
		return true
	}

	return pending < p.cfg.EnrichmentQueueMax
}
