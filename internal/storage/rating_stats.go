package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/lueurxax/telegram-digest-bot/internal/storage/sqlc"
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

func timeToDate(t time.Time) pgtype.Date {
	return pgtype.Date{Time: t, Valid: true}
}

func dateToTime(d pgtype.Date) time.Time {
	return d.Time
}

func (db *DB) UpsertChannelRatingStats(ctx context.Context, stats *RatingStats) error {
	if stats == nil {
		return nil
	}

	err := db.Queries.UpsertChannelRatingStats(ctx, sqlc.UpsertChannelRatingStatsParams{
		ChannelID:          toUUID(stats.ChannelID),
		PeriodStart:        timeToDate(stats.PeriodStart),
		PeriodEnd:          timeToDate(stats.PeriodEnd),
		WeightedGood:       stats.WeightedGood,
		WeightedBad:        stats.WeightedBad,
		WeightedIrrelevant: stats.WeightedIrrelevant,
		WeightedTotal:      stats.WeightedTotal,
		RatingCount:        safeIntToInt32(stats.RatingCount),
	})
	if err != nil {
		return fmt.Errorf("upsert channel rating stats: %w", err)
	}

	return nil
}

func (db *DB) UpsertGlobalRatingStats(ctx context.Context, stats *RatingStats) error {
	if stats == nil {
		return nil
	}

	err := db.Queries.UpsertGlobalRatingStats(ctx, sqlc.UpsertGlobalRatingStatsParams{
		PeriodStart:        timeToDate(stats.PeriodStart),
		PeriodEnd:          timeToDate(stats.PeriodEnd),
		WeightedGood:       stats.WeightedGood,
		WeightedBad:        stats.WeightedBad,
		WeightedIrrelevant: stats.WeightedIrrelevant,
		WeightedTotal:      stats.WeightedTotal,
		RatingCount:        safeIntToInt32(stats.RatingCount),
	})
	if err != nil {
		return fmt.Errorf("upsert global rating stats: %w", err)
	}

	return nil
}

func (db *DB) GetLatestChannelRatingStats(ctx context.Context, limit int) ([]RatingStatsSummary, error) {
	rows, err := db.Queries.GetLatestChannelRatingStats(ctx, safeIntToInt32(limit))
	if err != nil {
		return nil, fmt.Errorf("query latest channel rating stats: %w", err)
	}

	res := make([]RatingStatsSummary, 0, len(rows))

	for _, row := range rows {
		res = append(res, RatingStatsSummary{
			ChannelID:          fromUUID(row.ChannelID),
			Username:           row.Username.String,
			Title:              row.Title.String,
			PeriodStart:        dateToTime(row.PeriodStart),
			PeriodEnd:          dateToTime(row.PeriodEnd),
			WeightedGood:       row.WeightedGood,
			WeightedBad:        row.WeightedBad,
			WeightedIrrelevant: row.WeightedIrrelevant,
			WeightedTotal:      row.WeightedTotal,
			RatingCount:        int(row.RatingCount),
		})
	}

	return res, nil
}

func (db *DB) GetLatestGlobalRatingStats(ctx context.Context) (*GlobalRatingStats, error) {
	row, err := db.Queries.GetLatestGlobalRatingStats(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil //nolint:nilnil // nil,nil indicates no data exists yet
		}

		return nil, fmt.Errorf("get latest global rating stats: %w", err)
	}

	return &GlobalRatingStats{
		PeriodStart:        dateToTime(row.PeriodStart),
		PeriodEnd:          dateToTime(row.PeriodEnd),
		WeightedGood:       row.WeightedGood,
		WeightedBad:        row.WeightedBad,
		WeightedIrrelevant: row.WeightedIrrelevant,
		WeightedTotal:      row.WeightedTotal,
		RatingCount:        int(row.RatingCount),
	}, nil
}
