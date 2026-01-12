package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// ImportanceStats represents aggregate importance score stats for a window.
type ImportanceStats struct {
	Total          int
	AboveThreshold int
	P50            float64
	P75            float64
	P90            float64
	P95            float64
	Min            float64
	Max            float64
}

// ItemScore represents an item with its importance metrics for inspection.
type ItemScore struct {
	TGDate     time.Time
	Username   string
	Title      string
	Topic      string
	Summary    string
	Importance float64
	Relevance  float64
}

func (db *DB) GetImportanceStats(ctx context.Context, since time.Time, threshold float32) (ImportanceStats, error) {
	var (
		stats      ImportanceStats
		totalCount int64
		aboveCount int64
		p50        float64
		p75        float64
		p90        float64
		p95        float64
		minScore   float64
		maxScore   float64
	)

	err := db.Pool.QueryRow(ctx, `
		SELECT
			COUNT(*)::bigint AS total_count,
			COUNT(*) FILTER (WHERE i.importance_score >= $2)::bigint AS above_count,
			COALESCE(percentile_cont(0.50) WITHIN GROUP (ORDER BY i.importance_score), 0) AS p50,
			COALESCE(percentile_cont(0.75) WITHIN GROUP (ORDER BY i.importance_score), 0) AS p75,
			COALESCE(percentile_cont(0.90) WITHIN GROUP (ORDER BY i.importance_score), 0) AS p90,
			COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY i.importance_score), 0) AS p95,
			COALESCE(MIN(i.importance_score), 0) AS min_score,
			COALESCE(MAX(i.importance_score), 0) AS max_score
		FROM items i
		JOIN raw_messages rm ON i.raw_message_id = rm.id
		WHERE rm.tg_date >= $1
		  AND i.status = 'ready'
		  AND i.digested_at IS NULL
	`, since, threshold).Scan(&totalCount, &aboveCount, &p50, &p75, &p90, &p95, &minScore, &maxScore)
	if err != nil {
		return stats, err
	}

	stats.Total = int(totalCount)
	stats.AboveThreshold = int(aboveCount)
	stats.P50 = p50
	stats.P75 = p75
	stats.P90 = p90
	stats.P95 = p95
	stats.Min = minScore
	stats.Max = maxScore

	return stats, nil
}

func (db *DB) GetTopItemScores(ctx context.Context, since time.Time, limit int) ([]ItemScore, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT rm.tg_date,
		       c.username,
		       c.title,
		       i.importance_score,
		       i.relevance_score,
		       i.topic,
		       i.summary
		FROM items i
		JOIN raw_messages rm ON i.raw_message_id = rm.id
		JOIN channels c ON rm.channel_id = c.id
		WHERE rm.tg_date >= $1
		  AND i.status = 'ready'
		  AND i.digested_at IS NULL
		ORDER BY i.importance_score DESC, i.relevance_score DESC
		LIMIT $2
	`, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []ItemScore

	for rows.Next() {
		var (
			username pgtype.Text
			title    pgtype.Text
			topic    pgtype.Text
			summary  pgtype.Text
		)

		item := ItemScore{}
		if err := rows.Scan(&item.TGDate, &username, &title, &item.Importance, &item.Relevance, &topic, &summary); err != nil {
			return nil, err
		}

		item.Username = username.String
		item.Title = title.String
		item.Topic = topic.String
		item.Summary = summary.String

		res = append(res, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return res, nil
}
