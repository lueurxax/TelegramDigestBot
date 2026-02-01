package expandedview

import (
	"context"
	"time"
)

const (
	lowReliabilityLookbackDays   = 14
	lowReliabilityHalfLifeDays   = 7
	lowReliabilityMinRatings     = 10
	lowReliabilityScoreThreshold = -0.25
)

func (h *Handler) isLowReliabilityChannel(ctx context.Context, channelID string) bool {
	since := time.Now().AddDate(0, 0, -lowReliabilityLookbackDays)

	summaries, err := h.database.GetWeightedChannelRatingSummary(ctx, since, lowReliabilityHalfLifeDays)
	if err != nil {
		h.logger.Warn().Err(err).Msg("failed to load channel reliability")

		return false
	}

	for _, s := range summaries {
		if s.ChannelID != channelID {
			continue
		}

		if s.TotalCount < lowReliabilityMinRatings || s.WeightedTotal <= 0 {
			return false
		}

		score := (s.WeightedGood - (s.WeightedBad + s.WeightedIrrelevant)) / s.WeightedTotal

		return score <= lowReliabilityScoreThreshold
	}

	return false
}
