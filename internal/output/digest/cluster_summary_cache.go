package digest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const (
	clusterSummaryCacheMaxAgeDays = 7
	clusterSummaryMinOverlap      = 0.8
)

func (rc *digestRenderContext) findCachedClusterSummary(ctx context.Context, items []db.Item) (string, bool) {
	if len(items) == 0 {
		return "", false
	}

	cache := rc.loadClusterSummaryCache(ctx)
	if len(cache) == 0 {
		return "", false
	}

	itemIDs := collectItemIDs(items)
	if len(itemIDs) == 0 {
		return "", false
	}

	fingerprint := clusterFingerprint(itemIDs)

	bestSummary := ""
	bestScore := 0.0

	for _, entry := range cache {
		if entry.ClusterFingerprint == fingerprint {
			return entry.Summary, true
		}

		score := overlapScore(itemIDs, entry.ItemIDs)
		if score >= clusterSummaryMinOverlap && score > bestScore {
			bestScore = score
			bestSummary = entry.Summary
		}
	}

	if bestSummary != "" {
		return bestSummary, true
	}

	return "", false
}

func (rc *digestRenderContext) storeClusterSummaryCache(ctx context.Context, items []db.Item, summary string) {
	if rc == nil || rc.scheduler == nil || rc.scheduler.database == nil {
		return
	}

	itemIDs := collectItemIDs(items)
	if len(itemIDs) == 0 || strings.TrimSpace(summary) == "" {
		return
	}

	entry := &db.ClusterSummaryCacheEntry{
		DigestLanguage:     normalizeLanguage(rc.settings.digestLanguage),
		ClusterFingerprint: clusterFingerprint(itemIDs),
		ItemIDs:            itemIDs,
		Summary:            summary,
	}

	if err := rc.scheduler.database.UpsertClusterSummaryCache(ctx, entry); err != nil {
		rc.logger.Warn().Err(err).Msg("failed to upsert cluster summary cache")
	}
}

func (rc *digestRenderContext) loadClusterSummaryCache(ctx context.Context) []db.ClusterSummaryCacheEntry {
	if rc.clusterSummaryCacheLoaded {
		return rc.clusterSummaryCache
	}

	since := time.Now().Add(-clusterSummaryCacheMaxAgeDays * HoursPerDay * time.Hour)

	cache, err := rc.scheduler.database.GetClusterSummaryCache(ctx, normalizeLanguage(rc.settings.digestLanguage), since)
	if err != nil {
		rc.logger.Warn().Err(err).Msg("failed to load cluster summary cache")

		cache = nil
	}

	rc.clusterSummaryCache = cache
	rc.clusterSummaryCacheLoaded = true

	return cache
}

func collectItemIDs(items []db.Item) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if item.ID != "" {
			ids = append(ids, item.ID)
		}
	}

	sort.Strings(ids)

	return ids
}

func clusterFingerprint(itemIDs []string) string {
	if len(itemIDs) == 0 {
		return ""
	}

	joined := strings.Join(itemIDs, "|")
	hash := sha256.Sum256([]byte(joined))

	return hex.EncodeToString(hash[:])
}

func overlapScore(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	set := make(map[string]struct{}, len(a))
	for _, id := range a {
		set[id] = struct{}{}
	}

	intersection := 0

	for _, id := range b {
		if _, ok := set[id]; ok {
			intersection++
		}
	}

	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}

	return float64(intersection) / float64(union)
}

func normalizeLanguage(lang string) string {
	return strings.ToLower(strings.TrimSpace(lang))
}
