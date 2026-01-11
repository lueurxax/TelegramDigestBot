package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type LastDigestInfo struct {
	Start    time.Time
	End      time.Time
	PostedAt time.Time
}

func (db *DB) GetLastPostedDigest(ctx context.Context) (*LastDigestInfo, error) {
	row, err := db.Queries.GetLastPostedDigest(ctx)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &LastDigestInfo{
		Start:    row.WindowStart.Time,
		End:      row.WindowEnd.Time,
		PostedAt: row.PostedAt.Time,
	}, nil
}

type ChannelStats struct {
	ChannelID        string
	ConversionRate   float32
	AvgRelevance     float32
	StddevRelevance  float32
	AvgImportance    float32
	StddevImportance float32
}

func (db *DB) GetChannelStats(ctx context.Context) (map[string]ChannelStats, error) {
	rows, err := db.Queries.GetChannelStats(ctx)
	if err != nil {
		return nil, err
	}
	res := make(map[string]ChannelStats)
	for _, row := range rows {
		cID := fromUUID(row.ChannelID)
		res[cID] = ChannelStats{
			ChannelID:        cID,
			ConversionRate:   row.ConversionRate,
			AvgRelevance:     row.AvgRelevance,
			StddevRelevance:  row.StddevRelevance,
			AvgImportance:    row.AvgImportance,
			StddevImportance: row.StddevImportance,
		}
	}
	return res, nil
}

func (db *DB) CountActiveChannels(ctx context.Context) (int, error) {
	count, err := db.Queries.CountActiveChannels(ctx)
	return int(count), err
}

func (db *DB) CountRecentlyActiveChannels(ctx context.Context) (int, error) {
	count, err := db.Queries.CountRecentlyActiveChannels(ctx)
	return int(count), err
}

func (db *DB) CountReadyItems(ctx context.Context) (int, error) {
	count, err := db.Queries.CountReadyItems(ctx)
	return int(count), err
}
