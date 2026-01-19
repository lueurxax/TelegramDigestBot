package digest

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/lueurxax/telegram-digest-bot/internal/process/dedup"
	"github.com/lueurxax/telegram-digest-bot/internal/storage"
)

func (s *Scheduler) clusterItems(ctx context.Context, items []db.Item, start, end time.Time, logger *zerolog.Logger) error {
	if len(items) == 0 {
		return nil
	}

	items = s.limitClusterItems(items, logger)

	// Clean up old clusters for this window to prevent duplicates on retries
	if err := s.database.DeleteClustersForWindow(ctx, start, end); err != nil {
		logger.Error().Err(err).Msg("failed to delete old clusters")
	}

	cfg := s.getClusteringConfig(ctx, logger)
	clusterCtx := &clusterBuildContext{
		topicIndex:  s.getTopicIndex(items),
		topicGroups: s.getTopicGroups(items),
		embeddings:  s.getEmbeddings(ctx, items, logger),
		evidenceMap: s.getEvidenceMap(ctx, items, cfg, logger),
		assigned:    make(map[string]bool),
		allItems:    items,
		cfg:         cfg,
	}

	for topic, groupItems := range clusterCtx.topicGroups {
		if err := s.processTopicGroup(ctx, topic, groupItems, clusterCtx, start, end, logger); err != nil {
			return err
		}
	}

	return nil
}

type clusterBuildContext struct {
	topicIndex  map[string]string
	topicGroups map[string][]db.Item
	embeddings  map[string][]float32
	evidenceMap map[string][]db.ItemEvidenceWithSource
	assigned    map[string]bool
	allItems    []db.Item
	cfg         clusteringConfig
}

func (s *Scheduler) limitClusterItems(items []db.Item, logger *zerolog.Logger) []db.Item {
	if len(items) > ClusterMaxItemsLimit {
		logger.Warn().Int(LogFieldCount, len(items)).Msg("Too many items to cluster, limiting to first 500")
		return items[:ClusterMaxItemsLimit]
	}

	return items
}

func (s *Scheduler) processTopicGroup(ctx context.Context, topic string, groupItems []db.Item, bc *clusterBuildContext, start, end time.Time, logger *zerolog.Logger) error {
	for _, itemA := range groupItems {
		if bc.assigned[itemA.ID] {
			continue
		}

		clusterItemsList := s.findClusterItems(itemA, groupItems, bc.allItems, bc.assigned, bc.embeddings, bc.topicIndex, bc.evidenceMap, bc.cfg)
		bc.assigned[itemA.ID] = true

		clusterItemsList = s.validateClusterCoherence(clusterItemsList, bc, logger)
		if len(clusterItemsList) <= 1 {
			continue
		}

		if err := s.persistCluster(ctx, clusterItemsList, topic, bc.cfg, start, end, logger); err != nil {
			return err
		}
	}

	return nil
}

func (s *Scheduler) validateClusterCoherence(clusterItemsList []db.Item, bc *clusterBuildContext, logger *zerolog.Logger) []db.Item {
	coherence := s.calculateCoherence(clusterItemsList, bc.embeddings)

	if len(clusterItemsList) > 2 && coherence < bc.cfg.coherenceThreshold {
		logger.Debug().Float32("coherence", coherence).Int("size", len(clusterItemsList)).Msg("Rejecting cluster due to low coherence")

		for _, item := range clusterItemsList[1:] {
			bc.assigned[item.ID] = false
		}

		return clusterItemsList[:1]
	}

	return clusterItemsList
}

func (s *Scheduler) persistCluster(ctx context.Context, clusterItemsList []db.Item, topic string, cfg clusteringConfig, start, end time.Time, logger *zerolog.Logger) error {
	s.sortClusterItems(clusterItemsList)

	logger.Debug().
		Int("cluster_size", len(clusterItemsList)).
		Str("representative", clusterItemsList[0].ID).
		Float32("rep_importance", clusterItemsList[0].ImportanceScore).
		Msg("Cluster representative selected")

	clusterTopic := s.generateClusterTopic(ctx, clusterItemsList, topic, cfg.digestLanguage, cfg.smartLLMModel)

	clusterID, err := s.database.CreateCluster(ctx, start, end, clusterTopic)
	if err != nil {
		return fmt.Errorf("failed to create cluster: %w", err)
	}

	for _, it := range clusterItemsList {
		if err := s.database.AddToCluster(ctx, clusterID, it.ID); err != nil {
			logger.Error().Err(err).Msg("failed to add item to cluster")
		}
	}

	return nil
}

type clusteringConfig struct {
	similarityThreshold  float64
	crossTopicEnabled    bool
	crossTopicThreshold  float64
	coherenceThreshold   float32
	clusterWindow        time.Duration
	digestLanguage       string
	smartLLMModel        string
	evidenceEnabled      bool
	evidenceBoost        float32
	evidenceMinAgreement float32
}

func (s *Scheduler) getClusteringConfig(ctx context.Context, logger *zerolog.Logger) clusteringConfig {
	cfg := clusteringConfig{
		similarityThreshold:  float64(s.cfg.ClusterSimilarityThreshold),
		crossTopicEnabled:    s.cfg.CrossTopicClusteringEnabled,
		crossTopicThreshold:  float64(s.cfg.CrossTopicSimilarityThreshold),
		coherenceThreshold:   float32(s.cfg.ClusterCoherenceThreshold),
		evidenceEnabled:      s.cfg.EvidenceClusteringEnabled,
		evidenceBoost:        s.cfg.EvidenceClusteringBoost,
		evidenceMinAgreement: s.cfg.EvidenceClusteringMinScore,
	}

	s.loadClusteringConfigFromDB(ctx, logger, &cfg)
	s.applyClusteringDefaults(&cfg)

	return cfg
}

func (s *Scheduler) loadClusteringConfigFromDB(ctx context.Context, logger *zerolog.Logger, cfg *clusteringConfig) {
	loadSetting := func(key string, target interface{}, logMsg string) {
		if err := s.database.GetSetting(ctx, key, target); err != nil {
			logger.Debug().Err(err).Msg(logMsg)
		}
	}

	loadSetting("cluster_similarity_threshold", &cfg.similarityThreshold, "could not get cluster_similarity_threshold from DB")
	loadSetting("cross_topic_clustering_enabled", &cfg.crossTopicEnabled, "could not get cross_topic_clustering_enabled from DB")
	loadSetting("cross_topic_similarity_threshold", &cfg.crossTopicThreshold, "could not get cross_topic_similarity_threshold from DB")

	var coherence float64

	loadSetting("cluster_coherence_threshold", &coherence, "could not get cluster_coherence_threshold from DB")

	if coherence > 0 {
		cfg.coherenceThreshold = float32(coherence)
	}

	var clusterWindowHours float64

	loadSetting("cluster_time_window_hours", &clusterWindowHours, "could not get cluster_time_window_hours from DB")

	if clusterWindowHours > 0 {
		cfg.clusterWindow = time.Duration(clusterWindowHours) * time.Hour
	}

	loadSetting(SettingDigestLanguage, &cfg.digestLanguage, MsgCouldNotGetDigestLanguage)
	loadSetting(SettingSmartLLMModel, &cfg.smartLLMModel, MsgCouldNotGetSmartLLMModel)
}

func (s *Scheduler) applyClusteringDefaults(cfg *clusteringConfig) {
	if cfg.similarityThreshold <= 0 {
		cfg.similarityThreshold = float64(s.cfg.SimilarityThreshold)
	}

	if cfg.crossTopicThreshold <= 0 {
		cfg.crossTopicThreshold = cfg.similarityThreshold
	}

	if cfg.coherenceThreshold <= 0 {
		cfg.coherenceThreshold = float32(ClusterDefaultCoherenceThreshold)
	}
}

func (s *Scheduler) getTopicIndex(items []db.Item) map[string]string {
	topicIndex := make(map[string]string)
	for _, item := range items {
		topicIndex[item.ID] = normalizeClusterTopic(item.Topic)
	}

	return topicIndex
}

func (s *Scheduler) getTopicGroups(items []db.Item) map[string][]db.Item {
	topicGroups := make(map[string][]db.Item)

	for _, item := range items {
		topic := normalizeClusterTopic(item.Topic)
		topicGroups[topic] = append(topicGroups[topic], item)
	}

	return topicGroups
}

func (s *Scheduler) getEmbeddings(ctx context.Context, items []db.Item, logger *zerolog.Logger) map[string][]float32 {
	embeddings := make(map[string][]float32)

	for _, item := range items {
		emb, err := s.database.GetItemEmbedding(ctx, item.ID)
		if err != nil {
			logger.Warn().Str("item_id", item.ID).Err(err).Msg("failed to get embedding for item")
			continue
		}

		embeddings[item.ID] = emb
	}

	return embeddings
}

func (s *Scheduler) getEvidenceMap(ctx context.Context, items []db.Item, cfg clusteringConfig, logger *zerolog.Logger) map[string][]db.ItemEvidenceWithSource {
	if !cfg.evidenceEnabled {
		return nil
	}

	itemIDs := make([]string, len(items))
	for i, item := range items {
		itemIDs[i] = item.ID
	}

	evidenceMap, err := s.database.GetEvidenceForItems(ctx, itemIDs)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to get evidence for items, proceeding without evidence boost")
		return nil
	}

	if len(evidenceMap) > 0 {
		logger.Debug().Int(LogFieldItemsWithEvidence, len(evidenceMap)).Msg("loaded evidence for clustering")
	}

	return evidenceMap
}

func (s *Scheduler) findClusterItems(itemA db.Item, groupItems, allItems []db.Item, assigned map[string]bool, embeddings map[string][]float32, topicIndex map[string]string, evidenceMap map[string][]db.ItemEvidenceWithSource, cfg clusteringConfig) []db.Item {
	clusterItemsList := []db.Item{itemA}

	embA, okA := embeddings[itemA.ID]
	if !okA {
		return clusterItemsList
	}

	candidateItems := groupItems
	if cfg.crossTopicEnabled {
		candidateItems = allItems
	}

	topicA := topicIndex[itemA.ID]

	for _, itemB := range candidateItems {
		if s.shouldAddToCluster(itemA, itemB, topicA, assigned, embeddings, topicIndex, evidenceMap, cfg, embA) {
			clusterItemsList = append(clusterItemsList, itemB)
			assigned[itemB.ID] = true
		}
	}

	return clusterItemsList
}

func (s *Scheduler) shouldAddToCluster(itemA, itemB db.Item, topicA string, assigned map[string]bool, embeddings map[string][]float32, topicIndex map[string]string, evidenceMap map[string][]db.ItemEvidenceWithSource, cfg clusteringConfig, embA []float32) bool {
	embB, topicB, ok := s.validateClusterCandidate(itemA, itemB, topicA, assigned, embeddings, topicIndex, cfg)
	if !ok {
		return false
	}

	threshold := getClusterThreshold(topicA, topicB, cfg)
	similarity := calculateBoostedSimilarity(itemA.ID, itemB.ID, embA, embB, evidenceMap, cfg)

	return similarity > float32(threshold)
}

func (s *Scheduler) validateClusterCandidate(itemA, itemB db.Item, topicA string, assigned map[string]bool, embeddings map[string][]float32, topicIndex map[string]string, cfg clusteringConfig) ([]float32, string, bool) {
	if itemB.ID == itemA.ID || assigned[itemB.ID] {
		return nil, "", false
	}

	embB, okB := embeddings[itemB.ID]
	if !okB {
		return nil, "", false
	}

	topicB := topicIndex[itemB.ID]

	if !cfg.crossTopicEnabled && topicA != topicB {
		return nil, "", false
	}

	if cfg.clusterWindow > 0 && !withinClusterWindow(itemA.TGDate, itemB.TGDate, cfg.clusterWindow) {
		return nil, "", false
	}

	return embB, topicB, true
}

func getClusterThreshold(topicA, topicB string, cfg clusteringConfig) float64 {
	if topicA != topicB {
		return cfg.crossTopicThreshold
	}

	return cfg.similarityThreshold
}

func calculateBoostedSimilarity(itemAID, itemBID string, embA, embB []float32, evidenceMap map[string][]db.ItemEvidenceWithSource, cfg clusteringConfig) float32 {
	similarity := dedup.CosineSimilarity(embA, embB)

	if cfg.evidenceEnabled && evidenceMap != nil {
		boost := calculateEvidenceBoost(itemAID, itemBID, evidenceMap, cfg)
		similarity += boost
	}

	return similarity
}

// calculateEvidenceBoost calculates a similarity boost when two items share
// evidence sources with high agreement scores. This helps cluster items that
// are reporting on the same underlying story from different angles.
func calculateEvidenceBoost(itemAID, itemBID string, evidenceMap map[string][]db.ItemEvidenceWithSource, cfg clusteringConfig) float32 {
	evidenceA := evidenceMap[itemAID]
	evidenceB := evidenceMap[itemBID]

	if len(evidenceA) == 0 || len(evidenceB) == 0 {
		return 0
	}

	evidenceAURLs := buildEvidenceURLMap(evidenceA, cfg.evidenceMinAgreement)
	if len(evidenceAURLs) == 0 {
		return 0
	}

	maxAgreement := findMaxSharedAgreement(evidenceB, evidenceAURLs, cfg.evidenceMinAgreement)
	if maxAgreement == 0 {
		return 0
	}

	boost := maxAgreement * cfg.evidenceBoost
	if boost > cfg.evidenceBoost {
		boost = cfg.evidenceBoost
	}

	return boost
}

// buildEvidenceURLMap creates a map of URLs to agreement scores for evidence
// that meets the minimum agreement threshold.
func buildEvidenceURLMap(evidence []db.ItemEvidenceWithSource, minAgreement float32) map[string]float32 {
	urlMap := make(map[string]float32)

	for _, ev := range evidence {
		if ev.AgreementScore >= minAgreement {
			urlMap[ev.Source.URL] = ev.AgreementScore
		}
	}

	return urlMap
}

// findMaxSharedAgreement finds the maximum agreement score among shared evidence
// sources between two items.
func findMaxSharedAgreement(evidenceB []db.ItemEvidenceWithSource, evidenceAURLs map[string]float32, minAgreement float32) float32 {
	var maxAgreement float32

	for _, ev := range evidenceB {
		if ev.AgreementScore < minAgreement {
			continue
		}

		if scoreA, ok := evidenceAURLs[ev.Source.URL]; ok {
			minScore := minFloat32(scoreA, ev.AgreementScore)
			if minScore > maxAgreement {
				maxAgreement = minScore
			}
		}
	}

	return maxAgreement
}

func minFloat32(a, b float32) float32 {
	if a < b {
		return a
	}

	return b
}

func (s *Scheduler) sortClusterItems(clusterItemsList []db.Item) {
	sort.Slice(clusterItemsList, func(i, j int) bool {
		if clusterItemsList[i].ImportanceScore != clusterItemsList[j].ImportanceScore {
			return clusterItemsList[i].ImportanceScore > clusterItemsList[j].ImportanceScore
		}

		return len(clusterItemsList[i].Summary) > len(clusterItemsList[j].Summary)
	})
}

func (s *Scheduler) generateClusterTopic(ctx context.Context, clusterItemsList []db.Item, defaultTopic, digestLanguage, smartLLMModel string) string {
	if smartLLMModel != "" && len(clusterItemsList) > 1 {
		// Augment vague summaries with link context for better topic generation
		augmentedItems := make([]db.Item, len(clusterItemsList))
		for i, item := range clusterItemsList {
			augmentedItems[i] = item
			if len(item.Summary) < 100 {
				links, err := s.database.GetLinksForMessage(ctx, item.RawMessageID)
				if err == nil && len(links) > 0 {
					augmentedItems[i].Summary += " (Context: " + links[0].Title + ")"
				}
			}
		}

		if betterTopic, err := s.llmClient.GenerateClusterTopic(ctx, augmentedItems, digestLanguage, smartLLMModel); err == nil && betterTopic != "" {
			return betterTopic
		}
	}

	return defaultTopic
}

func (s *Scheduler) calculateCoherence(items []db.Item, embeddings map[string][]float32) float32 {
	if len(items) < 2 {
		return PerfectSimilarityScore
	}

	var sum float32

	var count int

	for i, itemI := range items {
		for _, itemJ := range items[i+1:] {
			embI, okI := embeddings[itemI.ID]
			embJ, okJ := embeddings[itemJ.ID]

			if okI && okJ {
				sum += dedup.CosineSimilarity(embI, embJ)
				count++
			}
		}
	}

	if count == 0 {
		return 0
	}

	return sum / float32(count)
}

func normalizeClusterTopic(topic string) string {
	normalized := strings.TrimSpace(cases.Title(language.English).String(strings.ToLower(topic)))
	if normalized == "" {
		return DefaultTopic
	}

	return normalized
}

func withinClusterWindow(a, b time.Time, window time.Duration) bool {
	if a.IsZero() || b.IsZero() {
		return true
	}

	diff := a.Sub(b)
	if diff < 0 {
		diff = -diff
	}

	return diff <= window
}
