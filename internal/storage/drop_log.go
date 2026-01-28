package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/lueurxax/telegram-digest-bot/internal/storage/sqlc"
)

type DropReasonStat struct {
	Reason string
	Count  int
}

func (db *DB) SaveRawMessageDropLog(ctx context.Context, rawMsgID, reason, detail string) error {
	err := db.Queries.SaveRawMessageDropLog(ctx, sqlc.SaveRawMessageDropLogParams{
		RawMessageID: toUUID(rawMsgID),
		Reason:       SanitizeUTF8(reason),
		Detail:       toText(detail),
	})
	if err != nil {
		return fmt.Errorf("save raw message drop log: %w", err)
	}

	return nil
}

func (db *DB) GetDropReasonStats(ctx context.Context, since time.Time, limit int) ([]DropReasonStat, error) {
	rows, err := db.Queries.GetDropReasonStats(ctx, sqlc.GetDropReasonStatsParams{
		TgDate: pgtype.Timestamptz{Time: since, Valid: true},
		Limit:  safeIntToInt32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("query drop reason stats: %w", err)
	}

	stats := make([]DropReasonStat, 0, len(rows))
	for _, row := range rows {
		stats = append(stats, DropReasonStat{
			Reason: row.Reason,
			Count:  int(row.Count),
		})
	}

	return stats, nil
}
