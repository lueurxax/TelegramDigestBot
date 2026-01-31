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

// ErrDropLogNotFound is returned when no drop log exists for a message.
var ErrDropLogNotFound = errors.New("drop log not found")

type DropReasonStat struct {
	Reason string
	Count  int
}

type RawMessageDropInfo struct {
	Reason    string
	Detail    string
	UpdatedAt time.Time
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

func (db *DB) GetRawMessageDropLog(ctx context.Context, rawMsgID string) (*RawMessageDropInfo, error) {
	row := db.Pool.QueryRow(ctx, `
		SELECT reason, detail, updated_at
		FROM raw_message_drop_log
		WHERE raw_message_id = $1
	`, toUUID(rawMsgID))

	var (
		reason    string
		detail    pgtype.Text
		updatedAt pgtype.Timestamptz
	)

	if err := row.Scan(&reason, &detail, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrDropLogNotFound
		}

		return nil, fmt.Errorf("get raw message drop log: %w", err)
	}

	info := &RawMessageDropInfo{
		Reason: reason,
		Detail: detail.String,
	}
	if updatedAt.Valid {
		info.UpdatedAt = updatedAt.Time
	}

	return info, nil
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
