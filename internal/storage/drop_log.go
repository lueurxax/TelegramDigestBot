package db

import (
	"context"
	"fmt"
	"time"
)

type DropReasonStat struct {
	Reason string
	Count  int
}

func (db *DB) SaveRawMessageDropLog(ctx context.Context, rawMsgID, reason, detail string) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO raw_message_drop_log (raw_message_id, reason, detail)
		VALUES ($1, $2, $3)
		ON CONFLICT (raw_message_id) DO UPDATE SET
			reason = EXCLUDED.reason,
			detail = EXCLUDED.detail,
			updated_at = NOW()
	`, toUUID(rawMsgID), reason, toText(detail))
	if err != nil {
		return fmt.Errorf("save raw message drop log: %w", err)
	}

	return nil
}

func (db *DB) GetDropReasonStats(ctx context.Context, since time.Time, limit int) ([]DropReasonStat, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT d.reason, COUNT(*)::int
		FROM raw_message_drop_log d
		JOIN raw_messages rm ON d.raw_message_id = rm.id
		WHERE rm.tg_date >= $1
		GROUP BY d.reason
		ORDER BY COUNT(*) DESC
		LIMIT $2
	`, since, limit)
	if err != nil {
		return nil, fmt.Errorf("query drop reason stats: %w", err)
	}
	defer rows.Close()

	stats := make([]DropReasonStat, 0, limit)

	for rows.Next() {
		var entry DropReasonStat
		if err := rows.Scan(&entry.Reason, &entry.Count); err != nil {
			return nil, fmt.Errorf("scan drop reason stat row: %w", err)
		}

		stats = append(stats, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate drop reason stats rows: %w", err)
	}

	return stats, nil
}
