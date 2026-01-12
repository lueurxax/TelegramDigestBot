package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/lueurxax/telegram-digest-bot/internal/db/sqlc"
)

// ChannelStatsEntry represents stats for a channel over a period
type ChannelStatsEntry struct {
	ChannelID        string
	PeriodStart      time.Time
	PeriodEnd        time.Time
	MessagesReceived int
	ItemsCreated     int
	ItemsDigested    int
	AvgImportance    float64
	AvgRelevance     float64
}

// RollingStats represents aggregated stats over a rolling window
type RollingStats struct {
	TotalMessages      int
	TotalItemsCreated  int
	TotalItemsDigested int
	AvgImportance      float64
	AvgRelevance       float64
}

// ChannelForAutoWeight represents a channel eligible for auto-weight calculation
type ChannelForAutoWeight struct {
	ID               string
	Username         string
	Title            string
	ImportanceWeight float32
}

func (db *DB) UpsertChannelStats(ctx context.Context, stats *ChannelStatsEntry) error {
	return db.Queries.UpsertChannelStats(ctx, sqlc.UpsertChannelStatsParams{
		ChannelID:        toUUID(stats.ChannelID),
		PeriodStart:      pgtype.Date{Time: stats.PeriodStart, Valid: true},
		PeriodEnd:        pgtype.Date{Time: stats.PeriodEnd, Valid: true},
		MessagesReceived: pgtype.Int4{Int32: int32(stats.MessagesReceived), Valid: true},
		ItemsCreated:     pgtype.Int4{Int32: int32(stats.ItemsCreated), Valid: true},
		ItemsDigested:    pgtype.Int4{Int32: int32(stats.ItemsDigested), Valid: true},
		AvgImportance:    pgtype.Float8{Float64: stats.AvgImportance, Valid: stats.AvgImportance > 0},
		AvgRelevance:     pgtype.Float8{Float64: stats.AvgRelevance, Valid: stats.AvgRelevance > 0},
	})
}

func (db *DB) GetChannelStatsRolling(ctx context.Context, channelID string, since time.Time) (*RollingStats, error) {
	row, err := db.Queries.GetChannelStatsRolling(ctx, sqlc.GetChannelStatsRollingParams{
		ChannelID:   toUUID(channelID),
		PeriodStart: pgtype.Date{Time: since, Valid: true},
	})
	if err != nil {
		return nil, err
	}

	return &RollingStats{
		TotalMessages:      int(row.TotalMessages),
		TotalItemsCreated:  int(row.TotalItemsCreated),
		TotalItemsDigested: int(row.TotalItemsDigested),
		AvgImportance:      row.AvgImportance,
		AvgRelevance:       row.AvgRelevance,
	}, nil
}

func (db *DB) GetChannelsForAutoWeight(ctx context.Context) ([]ChannelForAutoWeight, error) {
	rows, err := db.Queries.GetChannelsForAutoWeight(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]ChannelForAutoWeight, len(rows))

	for i, r := range rows {
		weight := r.ImportanceWeight.Float32
		if !r.ImportanceWeight.Valid || weight == 0 {
			weight = DefaultImportanceWeight
		}

		result[i] = ChannelForAutoWeight{
			ID:               fromUUID(r.ID),
			Username:         r.Username.String,
			Title:            r.Title.String,
			ImportanceWeight: weight,
		}
	}

	return result, nil
}

func (db *DB) UpdateChannelAutoWeight(ctx context.Context, channelID string, weight float32) error {
	return db.Queries.UpdateChannelAutoWeight(ctx, sqlc.UpdateChannelAutoWeightParams{
		ID:               toUUID(channelID),
		ImportanceWeight: pgtype.Float4{Float32: weight, Valid: true},
	})
}

// WindowChannelStats represents stats for a channel over a digest window
type WindowChannelStats struct {
	ChannelID        string
	MessagesReceived int64
	ItemsCreated     int64
	ItemsDigested    int64
	AvgImportance    float64
	AvgRelevance     float64
}

func (db *DB) GetChannelStatsForWindow(ctx context.Context, start, end time.Time) ([]WindowChannelStats, error) {
	rows, err := db.Queries.GetChannelStatsForWindow(ctx, sqlc.GetChannelStatsForWindowParams{
		TgDate:   toTimestamptz(start),
		TgDate_2: toTimestamptz(end),
	})
	if err != nil {
		return nil, err
	}

	result := make([]WindowChannelStats, len(rows))

	for i, r := range rows {
		// Type assert interface{} to float64
		avgImportance, _ := r.AvgImportance.(float64)
		avgRelevance, _ := r.AvgRelevance.(float64)

		result[i] = WindowChannelStats{
			ChannelID:        fromUUID(r.ChannelID),
			MessagesReceived: r.MessagesReceived,
			ItemsCreated:     r.ItemsCreated,
			ItemsDigested:    r.ItemsDigested,
			AvgImportance:    avgImportance,
			AvgRelevance:     avgRelevance,
		}
	}

	return result, nil
}

// CollectAndSaveChannelStats collects stats for a digest window and saves them
func (db *DB) CollectAndSaveChannelStats(ctx context.Context, start, end time.Time) error {
	stats, err := db.GetChannelStatsForWindow(ctx, start, end)
	if err != nil {
		return err
	}

	periodStart := start.Truncate(HoursPerDay)
	periodEnd := end.Truncate(HoursPerDay)

	if periodEnd.Equal(periodStart) {
		periodEnd = periodStart.Add(HoursPerDay)
	}

	for _, s := range stats {
		entry := &ChannelStatsEntry{
			ChannelID:        s.ChannelID,
			PeriodStart:      periodStart,
			PeriodEnd:        periodEnd,
			MessagesReceived: int(s.MessagesReceived),
			ItemsCreated:     int(s.ItemsCreated),
			ItemsDigested:    int(s.ItemsDigested),
			AvgImportance:    s.AvgImportance,
			AvgRelevance:     s.AvgRelevance,
		}
		if err := db.UpsertChannelStats(ctx, entry); err != nil {
			return err
		}
	}

	return nil
}
