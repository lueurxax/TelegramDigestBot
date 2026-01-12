package digest

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/lueurxax/telegram-digest-bot/internal/db"
	"github.com/lueurxax/telegram-digest-bot/internal/dedup"
	"github.com/rs/zerolog"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

func (s *Scheduler) clusterItems(ctx context.Context, items []db.Item, start, end time.Time, logger *zerolog.Logger) error {
	if len(items) == 0 {
		return nil
	}

	// Safety cap to avoid O(n²) explosion if TopN is misconfigured
	if len(items) > ClusterMaxItemsLimit {
		logger.Warn().Int(LogFieldCount, len(items)).Msg("Too many items to cluster, limiting to first 500")
		items = items[:ClusterMaxItemsLimit]
	}

	// Clean up old clusters for this window to prevent duplicates on retries
	if err := s.database.DeleteClustersForWindow(ctx, start, end); err != nil {
		logger.Error().Err(err).Msg("failed to delete old clusters")
	}

	cfg := s.getClusteringConfig(ctx, logger)
	topicIndex := s.getTopicIndex(items)
	topicGroups := s.getTopicGroups(items)
	embeddings := s.getEmbeddings(ctx, items, logger)
	assigned := make(map[string]bool)

	for topic, groupItems := range topicGroups {
		for _, itemA := range groupItems {
			if assigned[itemA.ID] {
				continue
			}

			clusterItemsList := s.findClusterItems(itemA, groupItems, items, assigned, embeddings, topicIndex, cfg)

			assigned[itemA.ID] = true

			// Reject clusters with coherence < 0.7 if they have more than 2 items
			coherence := s.calculateCoherence(clusterItemsList, embeddings)

			if len(clusterItemsList) > 2 && coherence < cfg.coherenceThreshold {
				logger.Debug().Float32("coherence", coherence).Int("size", len(clusterItemsList)).Msg("Rejecting cluster due to low coherence")
				// Only keep the first item, mark others as unassigned for later
				for k := 1; k < len(clusterItemsList); k++ {
					assigned[clusterItemsList[k].ID] = false
				}

				clusterItemsList = clusterItemsList[:1]
			}

			if len(clusterItemsList) <= 1 {
				continue
			}

			s.sortClusterItems(clusterItemsList)

			logger.Debug().
				Int("cluster_size", len(clusterItemsList)).
				Str("representative", clusterItemsList[0].ID).
				Float32("rep_importance", clusterItemsList[0].ImportanceScore).
				Msg("Cluster representative selected")

			clusterTopic := s.generateClusterTopic(ctx, clusterItemsList, topic, cfg.digestLanguage, cfg.smartLLMModel)

			clusterID, err := s.database.CreateCluster(ctx, start, end, clusterTopic)
			if err != nil {
				return err
			}

			for _, it := range clusterItemsList {
				if err := s.database.AddToCluster(ctx, clusterID, it.ID); err != nil {
					logger.Error().Err(err).Msg("failed to add item to cluster")
				}
			}
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

	if err := s.database.GetSetting(ctx, "cluster_similarity_threshold", &cfg.similarityThreshold); err != nil {
		logger.Debug().Err(err).Msg("could not get cluster_similarity_threshold from DB")
	}

	if cfg.similarityThreshold <= 0 {
		cfg.similarityThreshold = float64(s.cfg.SimilarityThreshold)
	}

	if err := s.database.GetSetting(ctx, "cross_topic_clustering_enabled", &cfg.crossTopicEnabled); err != nil {
		logger.Debug().Err(err).Msg("could not get cross_topic_clustering_enabled from DB")
	}

	if err := s.database.GetSetting(ctx, "cross_topic_similarity_threshold", &cfg.crossTopicThreshold); err != nil {
		logger.Debug().Err(err).Msg("could not get cross_topic_similarity_threshold from DB")
	}

	if cfg.crossTopicThreshold <= 0 {
		cfg.crossTopicThreshold = cfg.similarityThreshold
	}

	var coherence float64
	if err := s.database.GetSetting(ctx, "cluster_coherence_threshold", &coherence); err != nil {
		logger.Debug().Err(err).Msg("could not get cluster_coherence_threshold from DB")
	} else if coherence > 0 {
		cfg.coherenceThreshold = float32(coherence)
	}

	if cfg.coherenceThreshold <= 0 {
		cfg.coherenceThreshold = float32(ClusterDefaultCoherenceThreshold)
	}

	var clusterWindowHours float64
	if err := s.database.GetSetting(ctx, "cluster_time_window_hours", &clusterWindowHours); err != nil {
		logger.Debug().Err(err).Msg("could not get cluster_time_window_hours from DB")
	} else if clusterWindowHours > 0 {
		cfg.clusterWindow = time.Duration(clusterWindowHours) * time.Hour
	}

	if err := s.database.GetSetting(ctx, SettingDigestLanguage, &cfg.digestLanguage); err != nil {
		logger.Debug().Err(err).Msg(MsgCouldNotGetDigestLanguage)
	}

	if err := s.database.GetSetting(ctx, SettingSmartLLMModel, &cfg.smartLLMModel); err != nil {
		logger.Debug().Err(err).Msg(MsgCouldNotGetSmartLLMModel)
	}

	return cfg
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

	for _, itemB := range candidateItems {
		if itemB.ID == itemA.ID || assigned[itemB.ID] {
			continue
		}

		embB, okB := embeddings[itemB.ID]
		if !okB {
			continue
		}

		topicA := topicIndex[itemA.ID]
		topicB := topicIndex[itemB.ID]

		if !cfg.crossTopicEnabled && topicA != topicB {
			continue
		}

		if cfg.clusterWindow > 0 && !withinClusterWindow(itemA.TGDate, itemB.TGDate, cfg.clusterWindow) {
			continue
		}

		threshold := cfg.similarityThreshold
		if topicA != topicB {
			threshold = cfg.crossTopicThreshold
		}

		if dedup.CosineSimilarity(embA, embB) > float32(threshold) {
			clusterItemsList = append(clusterItemsList, itemB)
			assigned[itemB.ID] = true
		}
	}

	return clusterItemsList
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
		return 1.0
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
