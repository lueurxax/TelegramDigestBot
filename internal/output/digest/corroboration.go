package digest

import (
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

func (s *Scheduler) applyCorroborationAdjustments(items []db.Item, clusters []db.ClusterWithItems, settings digestSettings) ([]db.Item, []db.ClusterWithItems) {
	if len(clusters) == 0 {
		return items, clusters
	}

	if settings.corroborationBoost <= 0 && settings.singleSourcePenalty <= 0 {
		return items, clusters
	}

	itemIndex := buildItemIndex(items)

	for ci := range clusters {
		s.adjustClusterScores(&clusters[ci], itemIndex, settings)
	}

	return items, clusters
}

func buildItemIndex(items []db.Item) map[string]*db.Item {
	index := make(map[string]*db.Item, len(items))

	for i := range items {
		index[items[i].ID] = &items[i]
	}

	return index
}

func (s *Scheduler) adjustClusterScores(cluster *db.ClusterWithItems, itemIndex map[string]*db.Item, settings digestSettings) {
	channelCount := countUniqueChannels(cluster.Items)
	if channelCount == 0 {
		return
	}

	boost, penalty := calculateBoostPenalty(channelCount, len(cluster.Items), settings)

	for i := range cluster.Items {
		applyScoreAdjustment(&cluster.Items[i], itemIndex, boost, penalty)
	}
}

func countUniqueChannels(items []db.Item) int {
	channelSet := make(map[string]struct{})

	for _, it := range items {
		if it.SourceChannel != "" {
			channelSet[it.SourceChannel] = struct{}{}
		}
	}

	return len(channelSet)
}

func calculateBoostPenalty(channelCount, itemCount int, settings digestSettings) (boost, penalty float32) {
	if channelCount > 1 && settings.corroborationBoost > 0 {
		boost = float32(channelCount-1) * settings.corroborationBoost
	}

	if channelCount == 1 && itemCount > 1 && settings.singleSourcePenalty > 0 {
		penalty = settings.singleSourcePenalty
	}

	return boost, penalty
}

func applyScoreAdjustment(clusterItem *db.Item, itemIndex map[string]*db.Item, boost, penalty float32) {
	baseScore := clusterItem.ImportanceScore

	if item, ok := itemIndex[clusterItem.ID]; ok {
		baseScore = item.ImportanceScore
		clusterItem.RelevanceScore = item.RelevanceScore

		clusterItem.Topic = item.Topic
		if clusterItem.Summary == "" {
			clusterItem.Summary = item.Summary
		}
	}

	newScore := clampScore(baseScore + boost - penalty)

	if item, ok := itemIndex[clusterItem.ID]; ok {
		item.ImportanceScore = newScore
	}

	clusterItem.ImportanceScore = newScore
}

func clampScore(score float32) float32 {
	if score < 0 {
		return 0
	}

	if score > 1 {
		return 1
	}

	return score
}

// buildCorroborationLine returns an empty string (corroboration display disabled).
// Reserved for future implementation of multi-source verification display.
func (s *Scheduler) buildCorroborationLine(_ []db.Item, _ db.Item) string {
	return ""
}
