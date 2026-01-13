package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/config"
	"github.com/lueurxax/telegram-digest-bot/internal/db"
	"github.com/lueurxax/telegram-digest-bot/internal/dedup"
	"github.com/lueurxax/telegram-digest-bot/internal/filters"
	"github.com/lueurxax/telegram-digest-bot/internal/linkresolver"
	"github.com/lueurxax/telegram-digest-bot/internal/llm"
	"github.com/lueurxax/telegram-digest-bot/internal/observability"
)

type Repository interface {
	GetSetting(ctx context.Context, key string, target interface{}) error
	GetUnprocessedMessages(ctx context.Context, limit int) ([]db.RawMessage, error)
	GetBacklogCount(ctx context.Context) (int, error)
	GetActiveFilters(ctx context.Context) ([]db.Filter, error)
	MarkAsProcessed(ctx context.Context, id string) error
	GetRecentMessagesForChannel(ctx context.Context, channelID string, before time.Time, limit int) ([]string, error)
	GetChannelStats(ctx context.Context) (map[string]db.ChannelStats, error)
	SaveItem(ctx context.Context, item *db.Item) error
	SaveItemError(ctx context.Context, rawMsgID string, errJSON []byte) error
	SaveRelevanceGateLog(ctx context.Context, rawMsgID string, decision string, confidence *float32, reason, model, gateVersion string) error
	SaveRawMessageDropLog(ctx context.Context, rawMsgID, reason, detail string) error
	SaveEmbedding(ctx context.Context, itemID string, embedding []float32) error
	CheckStrictDuplicate(ctx context.Context, hash string, id string) (bool, error)
	FindSimilarItem(ctx context.Context, embedding []float32, threshold float32) (string, error)
	LinkMessageToLink(ctx context.Context, rawMsgID, linkCacheID string, position int) error
}

type Pipeline struct {
	cfg          *config.Config
	database     Repository
	llmClient    llm.Client
	linkResolver *linkresolver.Resolver
	logger       *zerolog.Logger
}

type pipelineSettings struct {
	batchSize               int
	filterList              []db.Filter
	adsFilterEnabled        bool
	minLength               int
	adsKeywords             []string
	skipForwards            bool
	filtersMode             string
	dedupMode               string
	topicsEnabled           bool
	relevanceThreshold      float32
	digestLanguage          string
	llmModel                string
	smartLLMModel           string
	visionRoutingEnabled    bool
	tieredImportanceEnabled bool
	digestTone              string
	normalizeScores         bool
	relevanceGateEnabled    bool
	relevanceGateMode       string
	relevanceGateModel      string
	channelStats            map[string]db.ChannelStats
	linkEnrichmentEnabled   bool
	maxLinks                int
	linkCacheTTL            time.Duration
	tgLinkCacheTTL          time.Duration
}

const (
	dropReasonDuplicateBatch      = "duplicate_batch"
	dropReasonForwarded           = "forwarded"
	dropReasonRelevanceGate       = "relevance_gate"
	dropReasonDedupSemanticBatch  = "dedup_semantic_batch"
	dropReasonDedupSemanticGlobal = "dedup_semantic_global"
	dropReasonDedupStrictGlobal   = "dedup_strict_global"
)

func New(cfg *config.Config, database Repository, llmClient llm.Client, linkResolver *linkresolver.Resolver, logger *zerolog.Logger) *Pipeline {
	return &Pipeline{
		cfg:          cfg,
		database:     database,
		llmClient:    llmClient,
		linkResolver: linkResolver,
		logger:       logger,
	}
}

func (p *Pipeline) Run(ctx context.Context) error {
	pollInterval, err := time.ParseDuration(p.cfg.WorkerPollInterval)
	if err != nil {
		p.logger.Error().Err(err).Str("interval", p.cfg.WorkerPollInterval).Msg("invalid worker poll interval, using 10s")

		pollInterval = DefaultPollInterval
	}

	for {
		correlationID := uuid.New().String()
		p.logger.Info().Str(LogFieldCorrelationID, correlationID).Msg("Starting pipeline batch")

		if err := p.processNextBatch(ctx, correlationID); err != nil {
			p.logger.Error().Err(err).Str(LogFieldCorrelationID, correlationID).Msg("failed to process batch")
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
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
		return err
	}

	if len(messages) == 0 {
		return nil
	}

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

func (p *Pipeline) loadPipelineSettings(ctx context.Context, logger zerolog.Logger) (*pipelineSettings, error) {
	s := &pipelineSettings{
		batchSize:             p.cfg.WorkerBatchSize,
		relevanceThreshold:    p.cfg.RelevanceThreshold,
		relevanceGateEnabled:  p.cfg.RelevanceGateEnabled,
		relevanceGateMode:     p.cfg.RelevanceGateMode,
		relevanceGateModel:    p.cfg.RelevanceGateModel,
		linkEnrichmentEnabled: p.cfg.LinkEnrichmentEnabled,
		maxLinks:              p.cfg.MaxLinksPerMessage,
		linkCacheTTL:          p.cfg.LinkCacheTTL,
		tgLinkCacheTTL:        p.cfg.TelegramLinkCacheTTL,
		filtersMode:           FilterModeMixed,
		dedupMode:             DedupModeSemantic,
		topicsEnabled:         true,
		minLength:             DefaultMinLength,
	}

	p.getSetting(ctx, "worker_batch_size", &s.batchSize, logger)

	filterList, err := p.database.GetActiveFilters(ctx)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to get active filters")
	}

	s.filterList = filterList

	p.getSetting(ctx, "filters_ads", &s.adsFilterEnabled, logger)
	p.getSetting(ctx, "filters_min_length", &s.minLength, logger)
	p.getSetting(ctx, "filters_ads_keywords", &s.adsKeywords, logger)
	p.getSetting(ctx, "filters_skip_forwards", &s.skipForwards, logger)
	p.getSetting(ctx, "filters_mode", &s.filtersMode, logger)
	p.getSetting(ctx, "dedup_mode", &s.dedupMode, logger)
	p.getSetting(ctx, "topics_enabled", &s.topicsEnabled, logger)
	p.getSetting(ctx, "relevance_threshold", &s.relevanceThreshold, logger)
	p.getSetting(ctx, "digest_language", &s.digestLanguage, logger)
	p.getSetting(ctx, "llm_model", &s.llmModel, logger)
	p.getSetting(ctx, "smart_llm_model", &s.smartLLMModel, logger)
	p.getSetting(ctx, "vision_routing_enabled", &s.visionRoutingEnabled, logger)
	p.getSetting(ctx, "tiered_importance_enabled", &s.tieredImportanceEnabled, logger)
	p.getSetting(ctx, "digest_tone", &s.digestTone, logger)
	p.getSetting(ctx, "normalize_scores", &s.normalizeScores, logger)
	p.getSetting(ctx, "relevance_gate_enabled", &s.relevanceGateEnabled, logger)
	p.getSetting(ctx, "relevance_gate_mode", &s.relevanceGateMode, logger)
	p.getSetting(ctx, "relevance_gate_model", &s.relevanceGateModel, logger)

	if s.normalizeScores {
		var err error

		s.channelStats, err = p.database.GetChannelStats(ctx)
		if err != nil {
			logger.Warn().Err(err).Msg("failed to fetch channel stats for normalization")

			s.normalizeScores = false
		}
	}

	p.getSetting(ctx, "link_enrichment_enabled", &s.linkEnrichmentEnabled, logger)
	p.getSetting(ctx, "max_links_per_message", &s.maxLinks, logger)
	s.linkCacheTTL = p.getDurationSetting(ctx, "link_cache_ttl", p.cfg.LinkCacheTTL, logger)
	s.tgLinkCacheTTL = p.getDurationSetting(ctx, "tg_link_cache_ttl", p.cfg.TelegramLinkCacheTTL, logger)

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

func (p *Pipeline) markProcessed(ctx context.Context, logger zerolog.Logger, msgID string) {
	if err := p.database.MarkAsProcessed(ctx, msgID); err != nil {
		logger.Error().Str(LogFieldMsgID, msgID).Err(err).Msg(LogMsgFailedToMarkProcessed)
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
	f := filters.New(s.filterList, s.adsFilterEnabled, s.minLength, s.adsKeywords, s.filtersMode)

	var deduplicator dedup.Deduplicator
	if s.dedupMode == DedupModeSemantic {
		deduplicator = dedup.NewSemantic(p.database, p.cfg.SimilarityThreshold)
	} else {
		deduplicator = dedup.NewStrict(p.database)
	}

	var candidates []llm.MessageInput

	embeddings := make(map[string][]float32)
	seenHashes := make(map[string]string) // hash -> msg_id

	for _, m := range messages {
		if p.skipMessage(ctx, logger, &m, s, seenHashes, f) {
			continue
		}

		// 2. Deduplication
		_, skip := p.handleDeduplication(ctx, logger, &m, s, candidates, embeddings, deduplicator)
		if skip {
			continue
		}

		// Fetch channel context and enrich
		candidates = append(candidates, p.enrichMessage(ctx, logger, m, s))
		seenHashes[m.CanonicalHash] = m.ID
	}

	return candidates, embeddings, nil
}

func (p *Pipeline) skipMessage(ctx context.Context, logger zerolog.Logger, m *db.RawMessage, s *pipelineSettings, seenHashes map[string]string, f *filters.Filterer) bool {
	if dupID, seen := seenHashes[m.CanonicalHash]; seen {
		logger.Info().Str(LogFieldMsgID, m.ID).Str(LogFieldDuplicateID, dupID).Msg("skipping strict duplicate in batch")
		p.recordDrop(ctx, logger, m.ID, dropReasonDuplicateBatch, dupID)
		p.markProcessed(ctx, logger, m.ID)

		return true
	}

	if s.skipForwards && m.IsForward {
		logger.Info().Str(LogFieldMsgID, m.ID).Msg("skipping forwarded message")
		p.recordDrop(ctx, logger, m.ID, dropReasonForwarded, "")
		p.markProcessed(ctx, logger, m.ID)

		return true
	}

	if filtered, reason := f.FilterReason(m.Text); filtered {
		p.recordDrop(ctx, logger, m.ID, reason, "")
		p.markProcessed(ctx, logger, m.ID)

		return true
	}

	if s.relevanceGateEnabled {
		decision := p.evaluateRelevanceGate(ctx, logger, m.Text, s)
		p.recordRelevanceGateDecision(ctx, logger, m.ID, decision)

		if decision.decision == DecisionIrrelevant {
			logger.Info().Str(LogFieldMsgID, m.ID).Str("reason", decision.reason).Msg("skipping message by relevance gate")
			p.recordDrop(ctx, logger, m.ID, dropReasonRelevanceGate, decision.reason)
			p.markProcessed(ctx, logger, m.ID)

			return true
		}
	}

	return false
}

func (p *Pipeline) handleDeduplication(ctx context.Context, logger zerolog.Logger, m *db.RawMessage, s *pipelineSettings, candidates []llm.MessageInput, embeddings map[string][]float32, deduplicator dedup.Deduplicator) ([]float32, bool) {
	emb, skip := p.generateEmbeddingIfNeeded(ctx, logger, m, s, embeddings)
	if skip {
		return nil, true
	}

	if p.checkBatchDuplicate(ctx, logger, m, s, candidates, embeddings, emb) {
		return nil, true
	}

	if p.checkGlobalDuplicate(ctx, logger, m, s, emb, deduplicator) {
		return nil, true
	}

	return emb, false
}

func (p *Pipeline) generateEmbeddingIfNeeded(ctx context.Context, logger zerolog.Logger, m *db.RawMessage, s *pipelineSettings, embeddings map[string][]float32) ([]float32, bool) {
	if s.dedupMode != DedupModeSemantic && !s.topicsEnabled {
		return nil, false
	}

	emb, err := p.llmClient.GetEmbedding(ctx, m.Text)
	if err != nil {
		logger.Error().Str(LogFieldMsgID, m.ID).Err(err).Msg("failed to get embedding")

		return nil, true
	}

	embeddings[m.ID] = emb

	return emb, false
}

func (p *Pipeline) checkBatchDuplicate(ctx context.Context, logger zerolog.Logger, m *db.RawMessage, s *pipelineSettings, candidates []llm.MessageInput, embeddings map[string][]float32, emb []float32) bool {
	if s.dedupMode != DedupModeSemantic {
		return false
	}

	for _, cand := range candidates {
		if dedup.CosineSimilarity(embeddings[cand.ID], emb) > p.cfg.SimilarityThreshold {
			logger.Info().Str(LogFieldMsgID, m.ID).Str(LogFieldDuplicateID, cand.ID).Msg("skipping semantic duplicate in batch")
			p.recordDrop(ctx, logger, m.ID, dropReasonDedupSemanticBatch, cand.ID)
			p.markProcessed(ctx, logger, m.ID)

			return true
		}
	}

	return false
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

	return llm.MessageInput{
		RawMessage:          m,
		ChannelTitle:        m.ChannelTitle,
		ChannelContext:      m.ChannelContext,
		ChannelDescription:  m.ChannelDescription,
		ChannelCategory:     m.ChannelCategory,
		ChannelTone:         m.ChannelTone,
		ChannelUpdateFreq:   m.ChannelUpdateFreq,
		RelevanceThreshold:  m.RelevanceThreshold,
		ImportanceThreshold: m.ImportanceThreshold,
		ImportanceWeight:    m.ImportanceWeight,
		Context:             channelCtx,
		ResolvedLinks:       resolvedLinks,
	}
}

func (p *Pipeline) runLLMProcessing(ctx context.Context, logger zerolog.Logger, candidates []llm.MessageInput, s *pipelineSettings) ([]llm.BatchResult, error) {
	start := time.Now()

	results := make([]llm.BatchResult, len(candidates))
	modelUsed := make([]string, len(candidates))

	// Group indices by model for Vision Routing
	modelGroups := p.groupIndicesByModel(candidates, s)

	for model, indices := range modelGroups {
		if err := p.processModelBatch(ctx, logger, candidates, results, modelUsed, model, indices, s); err != nil {
			return nil, err
		}
	}

	// 2.1 Tiered Importance Analysis
	p.performTieredImportanceAnalysis(ctx, logger, candidates, results, modelUsed, s)

	logger.Info().Int(LogFieldCount, len(candidates)).Dur("duration", time.Since(start)).Msg("LLM processing finished")

	return results, nil
}

func (p *Pipeline) groupIndicesByModel(candidates []llm.MessageInput, s *pipelineSettings) map[string][]int {
	modelGroups := make(map[string][]int)

	for i, c := range candidates {
		model := s.llmModel

		if s.visionRoutingEnabled && len(c.MediaData) > 0 && s.smartLLMModel != "" {
			model = s.smartLLMModel
		}

		modelGroups[model] = append(modelGroups[model], i)
	}

	return modelGroups
}

func (p *Pipeline) processModelBatch(ctx context.Context, logger zerolog.Logger, candidates []llm.MessageInput, results []llm.BatchResult, modelUsed []string, model string, indices []int, s *pipelineSettings) error {
	groupCandidates := make([]llm.MessageInput, len(indices))
	for j, idx := range indices {
		groupCandidates[j] = candidates[idx]
	}

	llmStart := time.Now()

	groupResults, err := p.llmClient.ProcessBatch(ctx, groupCandidates, s.digestLanguage, model, s.digestTone)
	if err != nil {
		logger.Error().Err(err).Str(LogFieldModel, model).Msg("LLM batch processing failed")
		observability.PipelineProcessed.WithLabelValues(StatusError).Add(float64(len(indices)))

		for _, idx := range indices {
			p.saveItemError(ctx, logger, candidates[idx].ID, err.Error())
			p.markProcessed(ctx, logger, candidates[idx].ID)
		}

		return err
	}

	observability.LLMRequestDuration.WithLabelValues(model).Observe(time.Since(llmStart).Seconds())

	if len(groupResults) != len(indices) {
		logger.Warn().Int("expected", len(indices)).Int("actual", len(groupResults)).Str(LogFieldModel, model).Msg("LLM batch size mismatch, results might be misaligned")
	}

	for j, idx := range indices {
		if j < len(groupResults) {
			results[idx] = groupResults[j]
			modelUsed[idx] = model
		}
	}

	return nil
}

func (p *Pipeline) performTieredImportanceAnalysis(ctx context.Context, logger zerolog.Logger, candidates []llm.MessageInput, results []llm.BatchResult, modelUsed []string, s *pipelineSettings) {
	if !s.tieredImportanceEnabled || s.smartLLMModel == "" {
		return
	}

	tieredIndices, tieredCandidates := selectTieredCandidates(candidates, results, modelUsed, s.smartLLMModel)
	if len(tieredCandidates) == 0 {
		return
	}

	logger.Info().Int(LogFieldCount, len(tieredCandidates)).Msg("Performing tiered importance analysis with smart model")

	llmStart := time.Now()

	tieredResults, err := p.llmClient.ProcessBatch(ctx, tieredCandidates, s.digestLanguage, s.smartLLMModel, s.digestTone)
	if err != nil {
		logger.Warn().Err(err).Msg("Tiered importance analysis failed, keeping original results")

		return
	}

	if len(tieredResults) != len(tieredCandidates) {
		return
	}

	observability.LLMRequestDuration.WithLabelValues(s.smartLLMModel).Observe(time.Since(llmStart).Seconds())
	applyTieredResults(results, tieredResults, tieredIndices)
}

func selectTieredCandidates(candidates []llm.MessageInput, results []llm.BatchResult, modelUsed []string, smartModel string) ([]int, []llm.MessageInput) {
	var tieredIndices []int

	var tieredCandidates []llm.MessageInput

	for i, res := range results {
		if res.ImportanceScore > TieredImportanceThreshold && modelUsed[i] != smartModel {
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

	readyCount := 0
	rejectedCount := 0

	for i, res := range results {
		if res.Summary == "" {
			p.handleEmptySummary(ctx, logger, candidates[i].ID, i)

			continue
		}

		item := p.createItem(logger, candidates[i], res, s)

		if p.saveAndMarkProcessed(ctx, logger, candidates[i], item, embeddings) {
			if item.Status == StatusReady {
				readyCount++
			} else {
				rejectedCount++
			}
		}
	}

	logger.Info().Int("ready", readyCount).Int("rejected", rejectedCount).Msg("Batch results stored")

	return nil
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

func (p *Pipeline) handleEmptySummary(ctx context.Context, logger zerolog.Logger, msgID string, index int) {
	logger.Warn().Str(LogFieldMsgID, msgID).Int("index", index).Msg("LLM summary empty for item, marking as error")
	observability.PipelineProcessed.WithLabelValues(StatusError).Inc()

	p.saveItemError(ctx, logger, msgID, "empty summary from LLM")
	p.markProcessed(ctx, logger, msgID)
}

func (p *Pipeline) createItem(logger zerolog.Logger, c llm.MessageInput, res llm.BatchResult, s *pipelineSettings) *db.Item {
	importance := p.calculateImportance(logger, c, res)
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

func (p *Pipeline) calculateImportance(logger zerolog.Logger, c llm.MessageInput, res llm.BatchResult) float32 {
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

func (p *Pipeline) saveAndMarkProcessed(ctx context.Context, logger zerolog.Logger, c llm.MessageInput, item *db.Item, embeddings map[string][]float32) bool {
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
			logger.Error().Str("item_id", item.ID).Err(err).Msg("failed to save embedding")
		}
	}

	if err := p.database.MarkAsProcessed(ctx, c.ID); err != nil {
		logger.Error().Str(LogFieldMsgID, c.ID).Err(err).Msg(LogMsgFailedToMarkProcessed)
	}

	return true
}
