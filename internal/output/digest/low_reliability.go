package digest

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

type lowReliabilityIndex struct {
	byUsername map[string]bool
	byPeerID   map[int64]bool
}

func (s *Scheduler) loadLowReliabilityIndex(ctx context.Context, logger *zerolog.Logger) lowReliabilityIndex {
	if s.database == nil {
		return lowReliabilityIndex{
			byUsername: map[string]bool{},
			byPeerID:   map[int64]bool{},
		}
	}

	since := time.Now().AddDate(0, 0, -LowReliabilityLookbackDays)

	summaries, err := s.database.GetWeightedChannelRatingSummary(ctx, since, LowReliabilityHalfLifeDays)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to load low reliability channels")

		return lowReliabilityIndex{
			byUsername: map[string]bool{},
			byPeerID:   map[int64]bool{},
		}
	}

	index := lowReliabilityIndex{
		byUsername: make(map[string]bool),
		byPeerID:   make(map[int64]bool),
	}

	for _, s := range summaries {
		if s.TotalCount < LowReliabilityMinRatings || s.WeightedTotal <= 0 {
			continue
		}

		score := (s.WeightedGood - (s.WeightedBad + s.WeightedIrrelevant)) / s.WeightedTotal
		if score > LowReliabilityScoreThreshold {
			continue
		}

		if s.Username != "" {
			index.byUsername[s.Username] = true
		}

		if s.ChannelPeerID != 0 {
			index.byPeerID[s.ChannelPeerID] = true
		}
	}

	return index
}

func (rc *digestRenderContext) isLowReliabilityItem(item db.Item) bool {
	if rc.lowReliability.byUsername[item.SourceChannel] {
		return true
	}

	if rc.lowReliability.byPeerID[item.SourceChannelID] {
		return true
	}

	return false
}

func (rc *digestRenderContext) isLowReliabilityGroup(items []db.Item) bool {
	for _, item := range items {
		if rc.isLowReliabilityItem(item) {
			return true
		}
	}

	return false
}
