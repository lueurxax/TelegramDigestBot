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
	if len(items) > 500 {
		logger.Warn().Int("count", len(items)).Msg("Too many items to cluster, limiting to first 500")
		items = items[:500]
	}

	// Clean up old clusters for this window to prevent duplicates on retries
	if err := s.database.DeleteClustersForWindow(ctx, start, end); err != nil {
		logger.Error().Err(err).Msg("failed to delete old clusters")
	}

	// 1. Group items by topic for hierarchical clustering (with normalization)
	topicGroups := make(map[string][]db.Item)
	for _, item := range items {
		topic := strings.TrimSpace(cases.Title(language.English).String(strings.ToLower(item.Topic)))
		if topic == "" {
			topic = DefaultTopic
		}
		topicGroups[topic] = append(topicGroups[topic], item)
	}

	embeddings := make(map[string][]float32)
	for _, item := range items {
		emb, err := s.database.GetItemEmbedding(ctx, item.ID)
		if err != nil {
			logger.Warn().Str("item_id", item.ID).Err(err).Msg("failed to get embedding for item")
			continue
		}
		embeddings[item.ID] = emb
	}

	var digestLanguage string
	if err := s.database.GetSetting(ctx, "digest_language", &digestLanguage); err != nil {
		logger.Debug().Err(err).Msg("could not get digest_language from DB")
	}

	var smartLLMModel string
	if err := s.database.GetSetting(ctx, "smart_llm_model", &smartLLMModel); err != nil {
		logger.Debug().Err(err).Msg("could not get smart_llm_model from DB")
	}

	for topic, groupItems := range topicGroups {
		assigned := make(map[string]bool)
		for i, itemA := range groupItems {
			if assigned[itemA.ID] {
				continue
			}

			clusterItemsList := []db.Item{itemA}
			embA, okA := embeddings[itemA.ID]

			if okA {
				for j := i + 1; j < len(groupItems); j++ {
					itemB := groupItems[j]
					if assigned[itemB.ID] {
						continue
					}

					embB, okB := embeddings[itemB.ID]
					if !okB {
						continue
					}

					if dedup.CosineSimilarity(embA, embB) > s.cfg.SimilarityThreshold {
						clusterItemsList = append(clusterItemsList, itemB)
						assigned[itemB.ID] = true
					}
				}
			}
			assigned[itemA.ID] = true

			// Reject clusters with coherence < 0.7 if they have more than 2 items
			coherence := s.calculateCoherence(clusterItemsList, embeddings)
			if len(clusterItemsList) > 2 && coherence < 0.7 {
				logger.Debug().Float32("coherence", coherence).Int("size", len(clusterItemsList)).Msg("Rejecting cluster due to low coherence")
				// Only keep the first item, mark others as unassigned for later
				for k := 1; k < len(clusterItemsList); k++ {
					assigned[clusterItemsList[k].ID] = false
				}
				clusterItemsList = clusterItemsList[:1]
			}

			if len(clusterItemsList) <= 1 {
				// Don't create single-item clusters in the database,
				// they will be rendered as regular items
				continue
			}

			// Sort cluster items by importance score (descending)
			// The first item becomes the "representative" of the cluster
			sort.Slice(clusterItemsList, func(i, j int) bool {
				if clusterItemsList[i].ImportanceScore != clusterItemsList[j].ImportanceScore {
					return clusterItemsList[i].ImportanceScore > clusterItemsList[j].ImportanceScore
				}
				// Tie-breaker: prefer longer summaries (more detailed)
				return len(clusterItemsList[i].Summary) > len(clusterItemsList[j].Summary)
			})

			logger.Debug().
				Int("cluster_size", len(clusterItemsList)).
				Str("representative", clusterItemsList[0].ID).
				Float32("rep_importance", clusterItemsList[0].ImportanceScore).
				Msg("Cluster representative selected")

			// Smart Cluster Naming
			clusterTopic := topic

			if smartLLMModel != "" && len(clusterItemsList) > 1 {
				if betterTopic, err := s.llmClient.GenerateClusterTopic(ctx, clusterItemsList, digestLanguage, smartLLMModel); err == nil && betterTopic != "" {
					clusterTopic = betterTopic
				}
			}

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
