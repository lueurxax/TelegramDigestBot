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
		pollInterval = 10 * time.Second
	}

	for {
		correlationID := uuid.New().String()
		p.logger.Info().Str("correlation_id", correlationID).Msg("Starting pipeline batch")
		if err := p.processNextBatch(ctx, correlationID); err != nil {
			p.logger.Error().Err(err).Str("correlation_id", correlationID).Msg("failed to process batch")
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func (p *Pipeline) processNextBatch(ctx context.Context, correlationID string) error {
	logger := p.logger.With().Str("correlation_id", correlationID).Logger()

	batchSize := p.cfg.WorkerBatchSize
	if err := p.database.GetSetting(ctx, "worker_batch_size", &batchSize); err != nil {
		logger.Debug().Err(err).Msg("could not get worker_batch_size from DB")
	}

	messages, err := p.database.GetUnprocessedMessages(ctx, batchSize)
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

	filterList, err := p.database.GetActiveFilters(ctx)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to get active filters")
	}

	var adsFilterEnabled bool
	if err := p.database.GetSetting(ctx, "filters_ads", &adsFilterEnabled); err != nil {
		logger.Debug().Err(err).Msg("could not get filters_ads from DB")
	}

	var minLength = 20
	if err := p.database.GetSetting(ctx, "filters_min_length", &minLength); err != nil {
		logger.Debug().Err(err).Msg("could not get filters_min_length from DB")
	}

	var adsKeywords []string
	if err := p.database.GetSetting(ctx, "filters_ads_keywords", &adsKeywords); err != nil {
		logger.Debug().Err(err).Msg("could not get filters_ads_keywords from DB")
	}

	var skipForwards bool
	if err := p.database.GetSetting(ctx, "filters_skip_forwards", &skipForwards); err != nil {
		logger.Debug().Err(err).Msg("could not get filters_skip_forwards from DB")
	}

	var filtersMode = "mixed"
	if err := p.database.GetSetting(ctx, "filters_mode", &filtersMode); err != nil {
		logger.Debug().Err(err).Msg("could not get filters_mode from DB")
	}

	var dedupMode = "semantic"
	if err := p.database.GetSetting(ctx, "dedup_mode", &dedupMode); err != nil {
		logger.Debug().Err(err).Msg("could not get dedup_mode from DB")
	}

	var topicsEnabled = true
	if err := p.database.GetSetting(ctx, "topics_enabled", &topicsEnabled); err != nil {
		logger.Debug().Err(err).Msg("could not get topics_enabled from DB")
	}

	relevanceThreshold := p.cfg.RelevanceThreshold
	if err := p.database.GetSetting(ctx, "relevance_threshold", &relevanceThreshold); err != nil {
		logger.Debug().Err(err).Msg("could not get relevance_threshold from DB")
	}

	var digestLanguage string
	if err := p.database.GetSetting(ctx, "digest_language", &digestLanguage); err != nil {
		logger.Debug().Err(err).Msg("could not get digest_language from DB")
	}
	var llmModel string
	if err := p.database.GetSetting(ctx, "llm_model", &llmModel); err != nil {
		logger.Debug().Err(err).Msg("could not get llm_model from DB")
	}
	var smartLLMModel string
	if err := p.database.GetSetting(ctx, "smart_llm_model", &smartLLMModel); err != nil {
		logger.Debug().Err(err).Msg("could not get smart_llm_model from DB")
	}
	var visionRoutingEnabled bool
	if err := p.database.GetSetting(ctx, "vision_routing_enabled", &visionRoutingEnabled); err != nil {
		logger.Debug().Err(err).Msg("could not get vision_routing_enabled from DB")
	}
	var tieredImportanceEnabled bool
	if err := p.database.GetSetting(ctx, "tiered_importance_enabled", &tieredImportanceEnabled); err != nil {
		logger.Debug().Err(err).Msg("could not get tiered_importance_enabled from DB")
	}

	var digestTone string
	if err := p.database.GetSetting(ctx, "digest_tone", &digestTone); err != nil {
		logger.Debug().Err(err).Msg("could not get digest_tone from DB")
	}

	var normalizeScores bool
	if err := p.database.GetSetting(ctx, "normalize_scores", &normalizeScores); err != nil {
		logger.Debug().Err(err).Msg("could not get normalize_scores from DB")
	}

	relevanceGateEnabled := p.cfg.RelevanceGateEnabled
	if err := p.database.GetSetting(ctx, "relevance_gate_enabled", &relevanceGateEnabled); err != nil {
		logger.Debug().Err(err).Msg("could not get relevance_gate_enabled from DB")
	}

	var channelStats map[string]db.ChannelStats
	if normalizeScores {
		var err error
		channelStats, err = p.database.GetChannelStats(ctx)
		if err != nil {
			logger.Warn().Err(err).Msg("failed to fetch channel stats for normalization")
			normalizeScores = false
		}
	}

	var linkEnrichmentEnabled bool
	if err := p.database.GetSetting(ctx, "link_enrichment_enabled", &linkEnrichmentEnabled); err != nil {
		logger.Debug().Err(err).Msg("could not get link_enrichment_enabled from DB")
		linkEnrichmentEnabled = p.cfg.LinkEnrichmentEnabled
	}

	var maxLinks = p.cfg.MaxLinksPerMessage
	if err := p.database.GetSetting(ctx, "max_links_per_message", &maxLinks); err != nil {
		logger.Debug().Err(err).Msg("could not get max_links_per_message from DB")
	}

	var linkCacheTTLStr = p.cfg.LinkCacheTTL.String()
	if err := p.database.GetSetting(ctx, "link_cache_ttl", &linkCacheTTLStr); err != nil {
		logger.Debug().Err(err).Msg("could not get link_cache_ttl from DB")
	}
	linkCacheTTL, _ := time.ParseDuration(linkCacheTTLStr)

	var tgLinkCacheTTLStr = p.cfg.TelegramLinkCacheTTL.String()
	if err := p.database.GetSetting(ctx, "tg_link_cache_ttl", &tgLinkCacheTTLStr); err != nil {
		logger.Debug().Err(err).Msg("could not get tg_link_cache_ttl from DB")
	}
	tgLinkCacheTTL, _ := time.ParseDuration(tgLinkCacheTTLStr)

	// 1. Filtering (MVP)
	f := filters.New(filterList, adsFilterEnabled, minLength, adsKeywords, filtersMode)
	var deduplicator dedup.Deduplicator
	if dedupMode == "semantic" {
		deduplicator = dedup.NewSemantic(p.database, p.cfg.SimilarityThreshold)
	} else {
		deduplicator = dedup.NewStrict(p.database)
	}

	var candidates []llm.MessageInput
	embeddings := make(map[string][]float32)
	seenHashes := make(map[string]string) // hash -> msg_id

	for _, m := range messages {
		if dupID, seen := seenHashes[m.CanonicalHash]; seen {
			logger.Info().Str("msg_id", m.ID).Str("duplicate_id", dupID).Msg("skipping strict duplicate in batch")
			if err := p.database.MarkAsProcessed(ctx, m.ID); err != nil {
				logger.Error().Str("msg_id", m.ID).Err(err).Msg("failed to mark message as processed")
			}
			continue
		}

		if skipForwards && m.IsForward {
			logger.Info().Str("msg_id", m.ID).Msg("skipping forwarded message")
			if err := p.database.MarkAsProcessed(ctx, m.ID); err != nil {
				logger.Error().Str("msg_id", m.ID).Err(err).Msg("failed to mark message as processed")
			}
			continue
		}

		if f.IsFiltered(m.Text) {
			if err := p.database.MarkAsProcessed(ctx, m.ID); err != nil {
				logger.Error().Str("msg_id", m.ID).Err(err).Msg("failed to mark message as processed")
			}
			continue
		}

		if relevanceGateEnabled {
			decision := evaluateRelevanceGate(m.Text)
			confidence := decision.confidence
			if err := p.database.SaveRelevanceGateLog(ctx, m.ID, decision.decision, &confidence, decision.reason, gateModel, gateVersion); err != nil {
				logger.Warn().Str("msg_id", m.ID).Err(err).Msg("failed to save relevance gate log")
			}
			if decision.decision == "irrelevant" {
				logger.Info().Str("msg_id", m.ID).Str("reason", decision.reason).Msg("skipping message by relevance gate")
				if err := p.database.MarkAsProcessed(ctx, m.ID); err != nil {
					logger.Error().Str("msg_id", m.ID).Err(err).Msg("failed to mark message as processed")
				}
				continue
			}
		}

		// 2. Deduplication
		var emb []float32
		if dedupMode == "semantic" || topicsEnabled {
			var err error
			emb, err = p.llmClient.GetEmbedding(ctx, m.Text)
			if err != nil {
				logger.Error().Str("msg_id", m.ID).Err(err).Msg("failed to get embedding")
				continue
			}
			embeddings[m.ID] = emb
		}

		// Semantic check in batch
		if dedupMode == "semantic" {
			foundInBatch := false
			for _, cand := range candidates {
				if dedup.CosineSimilarity(embeddings[cand.ID], emb) > p.cfg.SimilarityThreshold {
					logger.Info().Str("msg_id", m.ID).Str("duplicate_id", cand.ID).Msg("skipping semantic duplicate in batch")
					if err := p.database.MarkAsProcessed(ctx, m.ID); err != nil {
						logger.Error().Str("msg_id", m.ID).Err(err).Msg("failed to mark message as processed")
					}
					foundInBatch = true
					break
				}
			}
			if foundInBatch {
				continue
			}
		}

		isDup, dupID, dErr := deduplicator.IsDuplicate(ctx, m, emb)
		if dErr == nil && isDup {
			logger.Info().Str("msg_id", m.ID).Str("duplicate_id", dupID).Msg("skipping duplicate message")
			if err := p.database.MarkAsProcessed(ctx, m.ID); err != nil {
				logger.Error().Str("msg_id", m.ID).Err(err).Msg("failed to mark message as processed")
			}
			continue
		} else if dErr != nil {
			logger.Error().Str("msg_id", m.ID).Err(dErr).Msg("failed to check for duplicates")
		}

		// Fetch channel context for better quality
		channelCtx, cErr := p.database.GetRecentMessagesForChannel(ctx, m.ChannelID, m.TGDate, 5)
		if cErr != nil {
			logger.Warn().Err(cErr).Str("msg_id", m.ID).Msg("failed to fetch channel context")
		}

		resolvedLinks, eErr := p.enrichWithLinks(ctx, &m, linkEnrichmentEnabled, maxLinks, linkCacheTTL, tgLinkCacheTTL)
		if eErr != nil {
			logger.Warn().Err(eErr).Str("msg_id", m.ID).Msg("link enrichment failed")
		}

		candidates = append(candidates, llm.MessageInput{
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
		})
		seenHashes[m.CanonicalHash] = m.ID
	}

	if len(candidates) == 0 {
		return nil
	}

	// 2. Batched LLM processing
	start := time.Now()
	results := make([]llm.BatchResult, len(candidates))
	modelUsed := make([]string, len(candidates))

	// Group indices by model for Vision Routing
	modelGroups := make(map[string][]int)
	for i, c := range candidates {
		model := llmModel
		if visionRoutingEnabled && len(c.MediaData) > 0 && smartLLMModel != "" {
			model = smartLLMModel
		}
		modelGroups[model] = append(modelGroups[model], i)
	}

	for model, indices := range modelGroups {
		groupCandidates := make([]llm.MessageInput, len(indices))
		for j, idx := range indices {
			groupCandidates[j] = candidates[idx]
		}

		llmStart := time.Now()
		groupResults, err := p.llmClient.ProcessBatch(ctx, groupCandidates, digestLanguage, model, digestTone)
		if err != nil {
			logger.Error().Err(err).Str("model", model).Msg("LLM batch processing failed")
			observability.PipelineProcessed.WithLabelValues("error").Add(float64(len(indices)))
			for _, idx := range indices {
				errJSON, _ := json.Marshal(map[string]string{"error": err.Error()})
				_ = p.database.SaveItemError(ctx, candidates[idx].ID, errJSON)
				_ = p.database.MarkAsProcessed(ctx, candidates[idx].ID)
			}
			return err
		}
		observability.LLMRequestDuration.WithLabelValues(model).Observe(time.Since(llmStart).Seconds())

		if len(groupResults) != len(indices) {
			logger.Warn().Int("expected", len(indices)).Int("actual", len(groupResults)).Str("model", model).Msg("LLM batch size mismatch, results might be misaligned")
			// We proceed anyway, but results will be aligned based on whatever ProcessBatch returned.
			// openaiClient usually guarantees alignment by index.
		}

		for j, idx := range indices {
			if j < len(groupResults) {
				results[idx] = groupResults[j]
				modelUsed[idx] = model
			}
		}
	}

	// 2.1 Tiered Importance Analysis
	if tieredImportanceEnabled && smartLLMModel != "" {
		var tieredIndices []int
		var tieredCandidates []llm.MessageInput
		for i, res := range results {
			if res.ImportanceScore > 0.8 && modelUsed[i] != smartLLMModel {
				tieredIndices = append(tieredIndices, i)
				tieredCandidates = append(tieredCandidates, candidates[i])
			}
		}
		if len(tieredCandidates) > 0 {
			logger.Info().Int("count", len(tieredCandidates)).Msg("Performing tiered importance analysis with smart model")
			llmStart := time.Now()
			tieredResults, err := p.llmClient.ProcessBatch(ctx, tieredCandidates, digestLanguage, smartLLMModel, digestTone)
			if err == nil && len(tieredResults) == len(tieredCandidates) {
				observability.LLMRequestDuration.WithLabelValues(smartLLMModel).Observe(time.Since(llmStart).Seconds())
				for j, idx := range tieredIndices {
					results[idx] = tieredResults[j]
				}
			} else if err != nil {
				logger.Warn().Err(err).Msg("Tiered importance analysis failed, keeping original results")
			}
		}
	}

	duration := time.Since(start)
	logger.Info().Int("count", len(candidates)).Dur("duration", duration).Msg("LLM processing finished")

	// 3. Normalization (Optional)
	if normalizeScores && channelStats != nil {
		for i := range results {
			if results[i].Summary == "" {
				continue
			}
			stats, ok := channelStats[candidates[i].ChannelID]
			if ok {
				if stats.StddevRelevance > 0.01 {
					results[i].RelevanceScore = (results[i].RelevanceScore - stats.AvgRelevance) / stats.StddevRelevance
				}
				if stats.StddevImportance > 0.01 {
					results[i].ImportanceScore = (results[i].ImportanceScore - stats.AvgImportance) / stats.StddevImportance
				}
			}
		}
	}

	// 4. Store results
	readyCount := 0
	rejectedCount := 0
	for i, res := range results {
		// If summary is empty, it means LLM failed to process this specific item
		if res.Summary == "" {
			logger.Warn().Str("msg_id", candidates[i].ID).Int("index", i).Msg("LLM summary empty for item, marking as error")
			observability.PipelineProcessed.WithLabelValues("error").Inc()
			errJSON, _ := json.Marshal(map[string]string{"error": "empty summary from LLM"})
			_ = p.database.SaveItemError(ctx, candidates[i].ID, errJSON)
			_ = p.database.MarkAsProcessed(ctx, candidates[i].ID)
			continue
		}

		// Apply channel importance weight multiplier
		channelWeight := candidates[i].ImportanceWeight
		// Clamp weight to valid range [0.1, 2.0], default to 1.0 if invalid
		if channelWeight < 0.1 {
			channelWeight = 1.0
		} else if channelWeight > 2.0 {
			channelWeight = 2.0
		}
		importance := res.ImportanceScore * channelWeight
		// Cap at 1.0 to maintain valid range
		if importance > 1.0 {
			importance = 1.0
		}

		if !p.hasUniqueInfo(res.Summary) {
			importance = importance - 0.2
			if importance < 0 {
				importance = 0
			}
			logger.Debug().Str("msg_id", candidates[i].ID).Msg("Applied penalty for lack of unique info")
		}

		item := &db.Item{
			RawMessageID:    candidates[i].ID,
			RelevanceScore:  res.RelevanceScore,
			ImportanceScore: importance,
			Topic:           res.Topic,
			Summary:         res.Summary,
			Language:        res.Language,
			Status:          "ready",
		}
		itemRelThreshold := relevanceThreshold
		if candidates[i].RelevanceThreshold > 0 {
			itemRelThreshold = candidates[i].RelevanceThreshold
		}
		if candidates[i].AutoRelevanceEnabled {
			itemRelThreshold += candidates[i].RelevanceThresholdDelta
		}
		if itemRelThreshold < 0 {
			itemRelThreshold = 0
		} else if itemRelThreshold > 1 {
			itemRelThreshold = 1
		}

		if res.RelevanceScore < itemRelThreshold {
			item.Status = "rejected"
			rejectedCount++
			observability.PipelineProcessed.WithLabelValues("rejected").Inc()
		} else {
			readyCount++
			observability.PipelineProcessed.WithLabelValues("ready").Inc()
		}

		if err := p.database.SaveItem(ctx, item); err != nil {
			logger.Error().Str("msg_id", candidates[i].ID).Err(err).Msg("failed to save item")
			observability.PipelineProcessed.WithLabelValues("error").Inc()
			errJSON, _ := json.Marshal(map[string]string{"error": fmt.Sprintf("failed to save item: %v", err)})
			_ = p.database.SaveItemError(ctx, candidates[i].ID, errJSON)
			_ = p.database.MarkAsProcessed(ctx, candidates[i].ID)
			continue
		}

		// Save embedding
		emb := embeddings[candidates[i].ID]
		if len(emb) > 0 {
			if err := p.database.SaveEmbedding(ctx, item.ID, emb); err != nil {
				logger.Error().Str("item_id", item.ID).Err(err).Msg("failed to save embedding")
			}
		}

		if err := p.database.MarkAsProcessed(ctx, candidates[i].ID); err != nil {
			logger.Error().Str("msg_id", candidates[i].ID).Err(err).Msg("failed to mark message as processed")
		}
	}

	logger.Info().Int("ready", readyCount).Int("rejected", rejectedCount).Msg("Batch results stored")

	return nil
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
