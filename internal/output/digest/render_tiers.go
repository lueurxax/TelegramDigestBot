package digest

import (
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

// categorizeByImportance categorizes items or clusters into breaking, notable, and also groups.
func (rc *digestRenderContext) categorizeByImportance() (breaking, notable, also clusterGroup) {
	if rc.settings.topicsEnabled && len(rc.clusters) > 0 {
		return categorizeClusters(rc.clusters)
	}

	return categorizeItems(rc.items)
}

// categorizeClusters categorizes clusters by their maximum importance score.
func categorizeClusters(clusters []db.ClusterWithItems) (breaking, notable, also clusterGroup) {
	for _, c := range clusters {
		maxImp := clusterMaxImportance(c)

		if maxImp >= ImportanceScoreBreaking {
			breaking.clusters = append(breaking.clusters, c)
		} else if maxImp >= ImportanceScoreNotable {
			notable.clusters = append(notable.clusters, c)
		} else {
			also.clusters = append(also.clusters, c)
		}
	}

	return breaking, notable, also
}

// clusterMaxImportance returns the maximum importance score among items in a cluster.
func clusterMaxImportance(c db.ClusterWithItems) float32 {
	maxImp := float32(0)

	for _, it := range c.Items {
		if it.ImportanceScore > maxImp {
			maxImp = it.ImportanceScore
		}
	}

	return maxImp
}

// categorizeItems categorizes items by their importance score.
func categorizeItems(items []db.Item) (breaking, notable, also clusterGroup) {
	for _, it := range items {
		if it.ImportanceScore >= ImportanceScoreBreaking {
			breaking.items = append(breaking.items, it)
		} else if it.ImportanceScore >= ImportanceScoreNotable {
			notable.items = append(notable.items, it)
		} else {
			also.items = append(also.items, it)
		}
	}

	return breaking, notable, also
}

// getImportancePrefix returns the emoji prefix for a given importance score.
func getImportancePrefix(score float32) string {
	switch {
	case score >= ImportanceScoreBreaking:
		return EmojiBreaking // Breaking/Critical
	case score >= ImportanceScoreNotable:
		return EmojiNotable // Notable
	case score >= ImportanceScoreStandard:
		return EmojiStandard // Standard
	default:
		return EmojiBullet // Minor
	}
}

// collectAllItems collects all items from a clusterGroup (both cluster items and direct items).
func collectAllItems(group clusterGroup) []db.Item {
	totalItems := len(group.items)
	for _, c := range group.clusters {
		totalItems += len(c.Items)
	}

	if totalItems == 0 {
		return nil
	}

	allItems := make([]db.Item, 0, totalItems)

	for _, c := range group.clusters {
		allItems = append(allItems, c.Items...)
	}

	allItems = append(allItems, group.items...)

	return allItems
}
