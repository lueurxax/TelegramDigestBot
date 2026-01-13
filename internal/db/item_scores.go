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

// ItemStatusStats represents item counts by status in a time window.
type ItemStatusStats struct {
	Total         int
	ReadyPending  int
	ReadyDigested int
	Rejected      int
	Error         int
}

// ScoreDebugStats represents high-level pipeline counts for diagnostics.
type ScoreDebugStats struct {
	RawTotal       int
	RawProcessed   int
	ItemsTotal     int
	GateRelevant   int
	GateIrrelevant int
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

func (db *DB) GetItemStatusStats(ctx context.Context, since time.Time) (ItemStatusStats, error) {
	var (
		stats         ItemStatusStats
		total         int64
		readyPending  int64
		readyDigested int64
		rejected      int64
		errCount      int64
	)

	err := db.Pool.QueryRow(ctx, `
		SELECT
			COUNT(*)::bigint AS total_count,
			COUNT(*) FILTER (WHERE i.status = 'ready' AND i.digested_at IS NULL)::bigint AS ready_pending,
			COUNT(*) FILTER (WHERE i.status = 'ready' AND i.digested_at IS NOT NULL)::bigint AS ready_digested,
			COUNT(*) FILTER (WHERE i.status = 'rejected')::bigint AS rejected,
			COUNT(*) FILTER (WHERE i.status = 'error')::bigint AS error_count
		FROM items i
		JOIN raw_messages rm ON i.raw_message_id = rm.id
		WHERE rm.tg_date >= $1
	`, since).Scan(&total, &readyPending, &readyDigested, &rejected, &errCount)
	if err != nil {
		return stats, err
	}

	stats.Total = int(total)
	stats.ReadyPending = int(readyPending)
	stats.ReadyDigested = int(readyDigested)
	stats.Rejected = int(rejected)
	stats.Error = int(errCount)

	return stats, nil
}

func (db *DB) GetScoreDebugStats(ctx context.Context, since time.Time) (ScoreDebugStats, error) {
	var (
		stats          ScoreDebugStats
		rawTotal       int64
		rawProcessed   int64
		itemsTotal     int64
		gateRelevant   int64
		gateIrrelevant int64
	)

	err := db.Pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM raw_messages rm WHERE rm.tg_date >= $1)::bigint AS raw_total,
			(SELECT COUNT(*) FROM raw_messages rm WHERE rm.tg_date >= $1 AND rm.processed_at IS NOT NULL)::bigint AS raw_processed,
			(SELECT COUNT(*) FROM items i JOIN raw_messages rm ON i.raw_message_id = rm.id WHERE rm.tg_date >= $1)::bigint AS items_total,
			(SELECT COUNT(*) FROM relevance_gate_log rgl JOIN raw_messages rm ON rgl.raw_message_id = rm.id
				WHERE rm.tg_date >= $1 AND rgl.decision = 'relevant')::bigint AS gate_relevant,
			(SELECT COUNT(*) FROM relevance_gate_log rgl JOIN raw_messages rm ON rgl.raw_message_id = rm.id
				WHERE rm.tg_date >= $1 AND rgl.decision = 'irrelevant')::bigint AS gate_irrelevant
	`, since).Scan(&rawTotal, &rawProcessed, &itemsTotal, &gateRelevant, &gateIrrelevant)
	if err != nil {
		return stats, err
	}

	stats.RawTotal = int(rawTotal)
	stats.RawProcessed = int(rawProcessed)
	stats.ItemsTotal = int(itemsTotal)
	stats.GateRelevant = int(gateRelevant)
	stats.GateIrrelevant = int(gateIrrelevant)

	return stats, nil
}
