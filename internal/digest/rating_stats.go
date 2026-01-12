package digest

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/db"
)

func (s *Scheduler) UpdateRatingStats(ctx context.Context, logger *zerolog.Logger) error {
	now := time.Now()
	since := now.AddDate(0, 0, -autoRelevanceWindowDays)

	ratings, err := s.database.GetItemRatingsSince(ctx, since)
	if err != nil {
		return err
	}

	stats, global := s.aggregateRatingStats(now, ratings)

	periodStart := since.Truncate(HoursPerDay * time.Hour)

	periodEnd := now.Truncate(HoursPerDay * time.Hour)
	if !periodEnd.After(periodStart) {
		periodEnd = periodStart.Add(HoursPerDay * time.Hour)
	}

	for channelID, st := range stats {
		if err := s.database.UpsertChannelRatingStats(ctx, &db.RatingStats{
			ChannelID:          channelID,
			PeriodStart:        periodStart,
			PeriodEnd:          periodEnd,
			WeightedGood:       st.weightedGood,
			WeightedBad:        st.weightedBad,
			WeightedIrrelevant: st.weightedIrrelevant,
			WeightedTotal:      st.weightedTotal,
			RatingCount:        st.count,
		}); err != nil {
			return err
		}
	}

	if err := s.database.UpsertGlobalRatingStats(ctx, &db.RatingStats{
		PeriodStart:        periodStart,
		PeriodEnd:          periodEnd,
		WeightedGood:       global.weightedGood,
		WeightedBad:        global.weightedBad,
		WeightedIrrelevant: global.weightedIrrelevant,
		WeightedTotal:      global.weightedTotal,
		RatingCount:        global.count,
	}); err != nil {
		return err
	}

	logger.Info().
		Int("channels", len(stats)).
		Int(LogFieldRatingCount, global.count).
		Float64("weighted_total", global.weightedTotal).
		Msg("Updated rating stats")

	return nil
}
