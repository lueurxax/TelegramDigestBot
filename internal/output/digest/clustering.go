package digest

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lueurxax/telegram-digest-bot/internal/storage"
	"github.com/lueurxax/telegram-digest-bot/internal/process/dedup"
	"github.com/rs/zerolog"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
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
		topicIndex: s.getTopicIndex(items),
		topicGroups: s.getTopicGroups(items),
		embeddings: s.getEmbeddings(ctx, items, logger),
		assigned:   make(map[string]bool),
		allItems:   items,
		cfg:        cfg,
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

		clusterItemsList := s.findClusterItems(itemA, groupItems, bc.allItems, bc.assigned, bc.embeddings, bc.topicIndex, bc.cfg)
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

		for k := 1; k < len(clusterItemsList); k++ {
			bc.assigned[clusterItemsList[k].ID] = false
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
	similarityThreshold float64
	crossTopicEnabled   bool
	crossTopicThreshold float64
	coherenceThreshold  float32
	clusterWindow       time.Duration
	digestLanguage      string
	smartLLMModel       string
}

func (s *Scheduler) getClusteringConfig(ctx context.Context, logger *zerolog.Logger) clusteringConfig {
	cfg := clusteringConfig{
		similarityThreshold: float64(s.cfg.ClusterSimilarityThreshold),
		crossTopicEnabled:   s.cfg.CrossTopicClusteringEnabled,
		crossTopicThreshold: float64(s.cfg.CrossTopicSimilarityThreshold),
		coherenceThreshold:  float32(s.cfg.ClusterCoherenceThreshold),
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

func (s *Scheduler) findClusterItems(itemA db.Item, groupItems, allItems []db.Item, assigned map[string]bool, embeddings map[string][]float32, topicIndex map[string]string, cfg clusteringConfig) []db.Item {
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
		if s.shouldAddToCluster(itemA, itemB, topicA, assigned, embeddings, topicIndex, cfg, embA) {
			clusterItemsList = append(clusterItemsList, itemB)
			assigned[itemB.ID] = true
		}
	}

	return clusterItemsList
}

func (s *Scheduler) shouldAddToCluster(itemA, itemB db.Item, topicA string, assigned map[string]bool, embeddings map[string][]float32, topicIndex map[string]string, cfg clusteringConfig, embA []float32) bool {
	if itemB.ID == itemA.ID || assigned[itemB.ID] {
		return false
	}

	embB, okB := embeddings[itemB.ID]
	if !okB {
		return false
	}

	topicB := topicIndex[itemB.ID]

	if !cfg.crossTopicEnabled && topicA != topicB {
		return false
	}

	if cfg.clusterWindow > 0 && !withinClusterWindow(itemA.TGDate, itemB.TGDate, cfg.clusterWindow) {
		return false
	}

	threshold := cfg.similarityThreshold
	if topicA != topicB {
		threshold = cfg.crossTopicThreshold
	}

	return dedup.CosineSimilarity(embA, embB) > float32(threshold)
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
		if betterTopic, err := s.llmClient.GenerateClusterTopic(ctx, clusterItemsList, digestLanguage, smartLLMModel); err == nil && betterTopic != "" {
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

	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			embI, okI := embeddings[items[i].ID]
			embJ, okJ := embeddings[items[j].ID]

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
