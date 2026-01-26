package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/lueurxax/telegram-digest-bot/internal/storage/sqlc"
)

// ThresholdTuningLogEntry captures a single threshold adjustment decision.
type ThresholdTuningLogEntry struct {
	TunedAt             time.Time
	NetScore            float64
	Delta               float32
	RelevanceThreshold  float32
	ImportanceThreshold float32
}

func (db *DB) InsertThresholdTuningLog(ctx context.Context, entry *ThresholdTuningLogEntry) error {
	if entry == nil {
		return nil
	}

	if err := db.Queries.InsertThresholdTuningLog(ctx, sqlc.InsertThresholdTuningLogParams{
		TunedAt:             pgtype.Timestamptz{Time: entry.TunedAt, Valid: true},
		NetScore:            entry.NetScore,
		Delta:               entry.Delta,
		RelevanceThreshold:  entry.RelevanceThreshold,
		ImportanceThreshold: entry.ImportanceThreshold,
	}); err != nil {
		return fmt.Errorf("insert threshold tuning log: %w", err)
	}

	return nil
}
