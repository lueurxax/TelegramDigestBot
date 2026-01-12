package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

type RatingSummary struct {
	ChannelID       string
	Username        string
	Title           string
	GoodCount       int
	BadCount        int
	IrrelevantCount int
	TotalCount      int
}

func (db *DB) GetItemRatingSummary(ctx context.Context, since time.Time) ([]RatingSummary, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT rm.channel_id,
		       c.username,
		       c.title,
		       COUNT(*) FILTER (WHERE ir.rating = 'good') AS good_count,
		       COUNT(*) FILTER (WHERE ir.rating = 'bad') AS bad_count,
		       COUNT(*) FILTER (WHERE ir.rating = 'irrelevant') AS irrelevant_count,
		       COUNT(*) AS total_count
		FROM item_ratings ir
		JOIN items i ON ir.item_id = i.id
		JOIN raw_messages rm ON i.raw_message_id = rm.id
		JOIN channels c ON rm.channel_id = c.id
		WHERE ir.created_at >= $1
		GROUP BY rm.channel_id, c.username, c.title
		ORDER BY total_count DESC
	`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []RatingSummary

	for rows.Next() {
		var channelID pgtype.UUID

		var username pgtype.Text

		var title pgtype.Text

		var goodCount int64

		var badCount int64

		var irrelevantCount int64

		var totalCount int64

		if err := rows.Scan(&channelID, &username, &title, &goodCount, &badCount, &irrelevantCount, &totalCount); err != nil {
			return nil, err
		}

		res = append(res, RatingSummary{
			ChannelID:       fromUUID(channelID),
			Username:        username.String,
			Title:           title.String,
			GoodCount:       int(goodCount),
			BadCount:        int(badCount),
			IrrelevantCount: int(irrelevantCount),
			TotalCount:      int(totalCount),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return res, nil
}
