package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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

// RatingStatsSummary represents rating stats with channel metadata for reporting.
type RatingStatsSummary struct {
	ChannelID          string
	Username           string
	Title              string
	PeriodStart        time.Time
	PeriodEnd          time.Time
	WeightedGood       float64
	WeightedBad        float64
	WeightedIrrelevant float64
	WeightedTotal      float64
	RatingCount        int
}

// GlobalRatingStats represents aggregated rating stats across all channels.
type GlobalRatingStats struct {
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
	if err != nil {
		return fmt.Errorf("upsert channel rating stats: %w", err)
	}

	return nil
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
	if err != nil {
		return fmt.Errorf("upsert global rating stats: %w", err)
	}

	return nil
}

func (db *DB) GetLatestChannelRatingStats(ctx context.Context, limit int) ([]RatingStatsSummary, error) {
	rows, err := db.Pool.Query(ctx, `
		WITH latest AS (
			SELECT MAX(period_end) AS period_end FROM channel_rating_stats
		)
		SELECT crs.channel_id,
		       c.username,
		       c.title,
		       crs.period_start,
		       crs.period_end,
		       crs.weighted_good,
		       crs.weighted_bad,
		       crs.weighted_irrelevant,
		       crs.weighted_total,
		       crs.rating_count
		FROM channel_rating_stats crs
		JOIN latest l ON crs.period_end = l.period_end
		JOIN channels c ON c.id = crs.channel_id
		ORDER BY crs.weighted_total DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query latest channel rating stats: %w", err)
	}
	defer rows.Close()

	var res []RatingStatsSummary

	for rows.Next() {
		var (
			channelID pgtype.UUID
			username  pgtype.Text
			title     pgtype.Text
		)

		entry := RatingStatsSummary{}
		if err := rows.Scan(
			&channelID,
			&username,
			&title,
			&entry.PeriodStart,
			&entry.PeriodEnd,
			&entry.WeightedGood,
			&entry.WeightedBad,
			&entry.WeightedIrrelevant,
			&entry.WeightedTotal,
			&entry.RatingCount,
		); err != nil {
			return nil, fmt.Errorf("scan channel rating stat row: %w", err)
		}

		entry.ChannelID = fromUUID(channelID)
		entry.Username = username.String
		entry.Title = title.String

		res = append(res, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate channel rating stats rows: %w", err)
	}

	return res, nil
}

func (db *DB) GetLatestGlobalRatingStats(ctx context.Context) (*GlobalRatingStats, error) {
	var stats GlobalRatingStats

	err := db.Pool.QueryRow(ctx, `
		SELECT period_start,
		       period_end,
		       weighted_good,
		       weighted_bad,
		       weighted_irrelevant,
		       weighted_total,
		       rating_count
		FROM global_rating_stats
		ORDER BY period_end DESC
		LIMIT 1
	`).Scan(
		&stats.PeriodStart,
		&stats.PeriodEnd,
		&stats.WeightedGood,
		&stats.WeightedBad,
		&stats.WeightedIrrelevant,
		&stats.WeightedTotal,
		&stats.RatingCount,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil //nolint:nilnil // nil,nil indicates no data exists yet
		}

		return nil, fmt.Errorf("get latest global rating stats: %w", err)
	}

	return &stats, nil
}
