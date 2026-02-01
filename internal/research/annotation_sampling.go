package research

import (
	"math"

	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const (
	uncertaintyMargin = 0.10
)

func needsReview(item db.ResearchItemSearchResult, cfg *config.Config) bool {
	if cfg == nil {
		return false
	}

	impDistance := math.Abs(float64(item.ImportanceScore) - float64(cfg.ImportanceThreshold))
	relDistance := math.Abs(float64(item.RelevanceScore) - float64(cfg.RelevanceThreshold))
	distance := math.Min(impDistance, relDistance)

	if uncertaintyMargin <= 0 {
		return false
	}

	u := clamp01(1 - distance/uncertaintyMargin)

	return u > 0
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}

	if value > 1 {
		return 1
	}

	return value
}
