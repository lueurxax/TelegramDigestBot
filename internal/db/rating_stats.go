package db

import (
	"context"
	"time"
)

// RatingStats represents aggregated rating metrics over a period.
type RatingStats struct {
	ChannelID          string
	PeriodStart        time.Time
	PeriodEnd          time.Time
	WeightedGood       float64
	WeightedBad        float64
	WeightedIrrelevant float64
	WeightedTotal      float64
	RatingCount        int
}

func (db *DB) UpsertChannelRatingStats(ctx context.Context, stats *RatingStats) error {
	if stats == nil {
		return nil
	}

	_, err := db.Pool.Exec(ctx, `
		INSERT INTO channel_rating_stats (
			channel_id,
			period_start,
			period_end,
			weighted_good,
			weighted_bad,
			weighted_irrelevant,
			weighted_total,
			rating_count
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (channel_id, period_start, period_end) DO UPDATE SET
			weighted_good = EXCLUDED.weighted_good,
			weighted_bad = EXCLUDED.weighted_bad,
			weighted_irrelevant = EXCLUDED.weighted_irrelevant,
			weighted_total = EXCLUDED.weighted_total,
			rating_count = EXCLUDED.rating_count,
			updated_at = NOW()
	`, toUUID(stats.ChannelID), stats.PeriodStart, stats.PeriodEnd, stats.WeightedGood, stats.WeightedBad, stats.WeightedIrrelevant, stats.WeightedTotal, stats.RatingCount)

	return err
}

func (db *DB) UpsertGlobalRatingStats(ctx context.Context, stats *RatingStats) error {
	if stats == nil {
		return nil
	}

	_, err := db.Pool.Exec(ctx, `
		INSERT INTO global_rating_stats (
			period_start,
			period_end,
			weighted_good,
			weighted_bad,
			weighted_irrelevant,
			weighted_total,
			rating_count
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (period_start, period_end) DO UPDATE SET
			weighted_good = EXCLUDED.weighted_good,
			weighted_bad = EXCLUDED.weighted_bad,
			weighted_irrelevant = EXCLUDED.weighted_irrelevant,
			weighted_total = EXCLUDED.weighted_total,
			rating_count = EXCLUDED.rating_count,
			updated_at = NOW()
	`, stats.PeriodStart, stats.PeriodEnd, stats.WeightedGood, stats.WeightedBad, stats.WeightedIrrelevant, stats.WeightedTotal, stats.RatingCount)

	return err
}
