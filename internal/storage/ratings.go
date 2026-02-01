package db

import (
	"context"
	"fmt"
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

type WeightedRatingSummary struct {
	ChannelID          string
	Username           string
	ChannelPeerID      int64
	WeightedGood       float64
	WeightedBad        float64
	WeightedIrrelevant float64
	WeightedTotal      float64
	TotalCount         int
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
		return nil, fmt.Errorf("query item rating summary: %w", err)
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
			return nil, fmt.Errorf("scan rating summary row: %w", err)
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
		return nil, fmt.Errorf("iterate rating summary rows: %w", err)
	}

	return res, nil
}

func (db *DB) GetWeightedChannelRatingSummary(ctx context.Context, since time.Time, halfLifeDays float64) ([]WeightedRatingSummary, error) {
	rows, err := db.Pool.Query(ctx, `
		WITH weighted AS (
			SELECT rm.channel_id,
			       c.username,
			       c.tg_peer_id,
			       ir.rating,
			       exp(-EXTRACT(EPOCH FROM (now() - ir.created_at)) / 86400.0 * ln(2) / $2) AS weight
			FROM item_ratings ir
			JOIN items i ON ir.item_id = i.id
			JOIN raw_messages rm ON i.raw_message_id = rm.id
			JOIN channels c ON rm.channel_id = c.id
			WHERE ir.created_at >= $1
		)
		SELECT channel_id,
		       username,
		       tg_peer_id,
		       COUNT(*) AS total_count,
		       SUM(CASE WHEN rating = 'good' THEN weight ELSE 0 END) AS good_weight,
		       SUM(CASE WHEN rating = 'bad' THEN weight ELSE 0 END) AS bad_weight,
		       SUM(CASE WHEN rating = 'irrelevant' THEN weight ELSE 0 END) AS irrelevant_weight,
		       SUM(weight) AS total_weight
		FROM weighted
		GROUP BY channel_id, username, tg_peer_id
	`, since, halfLifeDays)
	if err != nil {
		return nil, fmt.Errorf("query weighted rating summary: %w", err)
	}
	defer rows.Close()

	summaries := make([]WeightedRatingSummary, 0)

	for rows.Next() {
		var (
			channelID        pgtype.UUID
			username         pgtype.Text
			totalCount       int64
			goodWeight       float64
			badWeight        float64
			irrelevantWeight float64
			totalWeight      float64
		)

		var peerID int64

		if err := rows.Scan(&channelID, &username, &peerID, &totalCount, &goodWeight, &badWeight, &irrelevantWeight, &totalWeight); err != nil {
			return nil, fmt.Errorf("scan weighted rating row: %w", err)
		}

		summaries = append(summaries, WeightedRatingSummary{
			ChannelID:          fromUUID(channelID),
			Username:           username.String,
			ChannelPeerID:      peerID,
			WeightedGood:       goodWeight,
			WeightedBad:        badWeight,
			WeightedIrrelevant: irrelevantWeight,
			WeightedTotal:      totalWeight,
			TotalCount:         int(totalCount),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate weighted rating rows: %w", err)
	}

	return summaries, nil
}
