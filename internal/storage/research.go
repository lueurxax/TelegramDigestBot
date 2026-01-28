package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Sentinel errors for research queries.
var (
	ErrResearchClusterNotFound = errors.New("research cluster not found")
	ErrResearchSessionNotFound = errors.New("research session not found")
)

const (
	defaultSearchLimit  = 50
	maxSearchLimit      = 200
	recencyHalfLifeDays = 14.0

	// SQL join constant.
	sqlAndJoin = " AND "

	// SQL format patterns for building dynamic queries.
	fmtScoreExpr       = "(0.5 * i.importance_score + 0.5 * exp(-extract(epoch from (now() - rm.tg_date)) / 86400 / %.1f))"
	fmtTextIlike       = "(i.summary ILIKE $%d OR rm.text ILIKE $%d)"
	fmtEvidenceIlike   = "(es.title ILIKE $%d OR es.description ILIKE $%d)"
	fmtDateFrom        = "rm.tg_date >= $%d"
	fmtDateTo          = "rm.tg_date <= $%d"
	fmtEvidenceDateGte = "es.crawled_at >= $%d"
	fmtEvidenceDateLte = "es.crawled_at <= $%d"

	// Error message format.
	errFmtIterateClaims = "iterate claims: %w"

	// SQL query templates.
	sqlSearchItems = `
			SELECT i.id,
			       i.summary,
			       i.topic,
			       i.status,
			       i.relevance_score,
			       i.importance_score,
			       rm.text,
			       rm.tg_date,
			       rm.tg_message_id,
			       c.username,
			       c.title,
			       c.tg_peer_id,
			       %s AS score
			FROM items i
			JOIN raw_messages rm ON i.raw_message_id = rm.id
			JOIN channels c ON rm.channel_id = c.id
			WHERE %s
			ORDER BY score DESC, rm.tg_date DESC
			LIMIT %d OFFSET %d
		`

	sqlSearchEvidence = `
			SELECT DISTINCT ON (es.id)
			       es.id,
			       es.url,
			       es.title,
			       es.description,
			       es.domain,
			       es.provider,
			       ie.agreement_score,
			       i.id,
			       i.summary,
			       i.topic,
			       rm.tg_date,
			       c.title,
			       c.username
			FROM evidence_sources es
			LEFT JOIN item_evidence ie ON ie.evidence_id = es.id
			LEFT JOIN items i ON i.id = ie.item_id
			LEFT JOIN raw_messages rm ON i.raw_message_id = rm.id
			LEFT JOIN channels c ON rm.channel_id = c.id
			WHERE %s
			ORDER BY es.id, ie.agreement_score DESC NULLS LAST
			LIMIT %d OFFSET %d
		`
)

// ResearchSearchParams defines filters for research search.
type ResearchSearchParams struct {
	Query        string
	From         *time.Time
	To           *time.Time
	Channel      string
	Topic        string
	Lang         string
	Limit        int
	Offset       int
	IncludeCount bool
}

// ResearchItemSearchResult is a lightweight item search result.
type ResearchItemSearchResult struct {
	ID              string
	Summary         string
	Text            string
	Topic           string
	Status          string
	RelevanceScore  float32
	ImportanceScore float32
	TGDate          time.Time
	MessageID       int64
	ChannelUsername string
	ChannelTitle    string
	ChannelPeerID   int64
	Score           float64
}

// ResearchEvidenceSearchResult is an evidence search result with optional item context.
type ResearchEvidenceSearchResult struct {
	EvidenceID      string
	URL             string
	Title           string
	Description     string
	Domain          string
	Provider        string
	AgreementScore  float32
	ItemID          string
	ItemSummary     string
	ItemTopic       string
	ItemTGDate      time.Time
	ChannelTitle    string
	ChannelUsername string
}

// ResearchSearchResultCount holds total count.
type ResearchSearchResultCount struct {
	Total int
}

// normalizeSearchLimit clamps the limit to valid range.
func normalizeSearchLimit(limit int) int {
	if limit <= 0 {
		return defaultSearchLimit
	}

	if limit > maxSearchLimit {
		return maxSearchLimit
	}

	return limit
}

// buildItemSearchQuery builds the SQL query and args for item search.
func buildItemSearchQuery(params ResearchSearchParams, where []string, args []any, limit int) (string, []any) {
	if params.Query != "" && len([]rune(params.Query)) >= 3 {
		args = append(args, params.Query)
		tsQueryIdx := len(args)
		rankExpr := fmt.Sprintf("ts_rank_cd(i.search_vector, plainto_tsquery('simple', $%d))", tsQueryIdx)
		scoreExpr := fmt.Sprintf("(0.5 * %s + 0.3 * i.importance_score + 0.2 * exp(-extract(epoch from (now() - rm.tg_date)) / 86400 / %.1f))", rankExpr, recencyHalfLifeDays)

		where = append(where, fmt.Sprintf("i.search_vector @@ plainto_tsquery('simple', $%d)", tsQueryIdx))

		return fmt.Sprintf(sqlSearchItems, scoreExpr, strings.Join(where, sqlAndJoin), limit, params.Offset), args
	}

	if params.Query != "" {
		pattern := "%" + SanitizeUTF8(params.Query) + "%"
		args = append(args, pattern)
		patternIdx := len(args)
		scoreExpr := fmt.Sprintf(fmtScoreExpr, recencyHalfLifeDays)
		where = append(where, fmt.Sprintf(fmtTextIlike, patternIdx, patternIdx))

		return fmt.Sprintf(sqlSearchItems, scoreExpr, strings.Join(where, sqlAndJoin), limit, params.Offset), args
	}

	scoreExpr := fmt.Sprintf(fmtScoreExpr, recencyHalfLifeDays)

	return fmt.Sprintf(sqlSearchItems, scoreExpr, strings.Join(where, sqlAndJoin), limit, params.Offset), args
}

// SearchResearchItems searches items using full-text search with filters.
func (db *DB) SearchResearchItems(ctx context.Context, params ResearchSearchParams) ([]ResearchItemSearchResult, *ResearchSearchResultCount, error) {
	limit := normalizeSearchLimit(params.Limit)
	normalizedChannel := normalizeUsername(strings.TrimSpace(params.Channel))
	where, args := buildResearchItemFilters(params, normalizedChannel)

	query, args := buildItemSearchQuery(params, where, args, limit)

	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("search research items: %w", err)
	}
	defer rows.Close()

	results := make([]ResearchItemSearchResult, 0, limit)

	for rows.Next() {
		var (
			itemID  pgtype.UUID
			summary pgtype.Text
			topic   pgtype.Text
			text    pgtype.Text
			user    pgtype.Text
			title   pgtype.Text
		)

		res := ResearchItemSearchResult{}

		if err := rows.Scan(
			&itemID,
			&summary,
			&topic,
			&res.Status,
			&res.RelevanceScore,
			&res.ImportanceScore,
			&text,
			&res.TGDate,
			&res.MessageID,
			&user,
			&title,
			&res.ChannelPeerID,
			&res.Score,
		); err != nil {
			return nil, nil, fmt.Errorf("scan research item: %w", err)
		}

		res.ID = fromUUID(itemID)
		res.Summary = summary.String
		res.Topic = topic.String
		res.Text = text.String
		res.ChannelUsername = user.String
		res.ChannelTitle = title.String

		results = append(results, res)
	}

	if rows.Err() != nil {
		return nil, nil, fmt.Errorf("iterate research items: %w", rows.Err())
	}

	var count *ResearchSearchResultCount

	if params.IncludeCount {
		total, err := db.countResearchItems(ctx, params, normalizedChannel)
		if err != nil {
			return nil, nil, err
		}

		count = &ResearchSearchResultCount{Total: total}
	}

	return results, count, nil
}

// buildEvidenceSearchQuery builds the SQL query and args for evidence search.
func buildEvidenceSearchQuery(params ResearchSearchParams, where []string, args []any, limit int) (string, []any) {
	if params.Query != "" && len([]rune(params.Query)) >= 3 {
		args = append(args, params.Query)
		tsQueryIdx := len(args)
		where = append(where, fmt.Sprintf("es.search_vector @@ plainto_tsquery('simple', $%d)", tsQueryIdx))

		return fmt.Sprintf(sqlSearchEvidence, strings.Join(where, sqlAndJoin), limit, params.Offset), args
	}

	if params.Query != "" {
		pattern := "%" + SanitizeUTF8(params.Query) + "%"
		args = append(args, pattern)
		patternIdx := len(args)
		where = append(where, fmt.Sprintf(fmtEvidenceIlike, patternIdx, patternIdx))

		return fmt.Sprintf(sqlSearchEvidence, strings.Join(where, sqlAndJoin), limit, params.Offset), args
	}

	return fmt.Sprintf(sqlSearchEvidence, strings.Join(where, sqlAndJoin), limit, params.Offset), args
}

// SearchResearchEvidence searches evidence sources with filters.
func (db *DB) SearchResearchEvidence(ctx context.Context, params ResearchSearchParams) ([]ResearchEvidenceSearchResult, *ResearchSearchResultCount, error) {
	limit := normalizeSearchLimit(params.Limit)
	normalizedChannel := normalizeUsername(strings.TrimSpace(params.Channel))
	where, args := buildResearchEvidenceFilters(params, normalizedChannel)

	query, args := buildEvidenceSearchQuery(params, where, args, limit)

	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("search research evidence: %w", err)
	}
	defer rows.Close()

	results := make([]ResearchEvidenceSearchResult, 0, limit)

	for rows.Next() {
		var (
			evidenceID   pgtype.UUID
			url          pgtype.Text
			title        pgtype.Text
			desc         pgtype.Text
			domain       pgtype.Text
			provider     pgtype.Text
			agreement    pgtype.Float4
			itemID       pgtype.UUID
			itemSummary  pgtype.Text
			itemTopic    pgtype.Text
			tgDate       pgtype.Timestamptz
			channelTitle pgtype.Text
			channelUser  pgtype.Text
		)

		res := ResearchEvidenceSearchResult{}

		if err := rows.Scan(
			&evidenceID,
			&url,
			&title,
			&desc,
			&domain,
			&provider,
			&agreement,
			&itemID,
			&itemSummary,
			&itemTopic,
			&tgDate,
			&channelTitle,
			&channelUser,
		); err != nil {
			return nil, nil, fmt.Errorf("scan research evidence: %w", err)
		}

		res.EvidenceID = fromUUID(evidenceID)
		res.URL = url.String
		res.Title = title.String
		res.Description = desc.String
		res.Domain = domain.String

		res.Provider = provider.String
		if agreement.Valid {
			res.AgreementScore = agreement.Float32
		}

		res.ItemID = fromUUID(itemID)
		res.ItemSummary = itemSummary.String

		res.ItemTopic = itemTopic.String
		if tgDate.Valid {
			res.ItemTGDate = tgDate.Time
		}

		res.ChannelTitle = channelTitle.String
		res.ChannelUsername = channelUser.String

		results = append(results, res)
	}

	if rows.Err() != nil {
		return nil, nil, fmt.Errorf("iterate research evidence: %w", rows.Err())
	}

	var count *ResearchSearchResultCount

	if params.IncludeCount {
		total, err := db.countResearchEvidence(ctx, params, normalizedChannel)
		if err != nil {
			return nil, nil, err
		}

		count = &ResearchSearchResultCount{Total: total}
	}

	return results, count, nil
}

func (db *DB) countResearchItems(ctx context.Context, params ResearchSearchParams, channel string) (int, error) {
	where, args := buildResearchItemFilters(params, channel)
	where, args = applyResearchQueryFilter(params.Query, where, args,
		"i.search_vector", "(i.summary ILIKE $%d OR rm.text ILIKE $%d)")

	return db.executeCountQuery(ctx, `
		SELECT COUNT(*)
		FROM items i
		JOIN raw_messages rm ON i.raw_message_id = rm.id
		JOIN channels c ON rm.channel_id = c.id
		WHERE %s
	`, where, args, "count research items")
}

func (db *DB) countResearchEvidence(ctx context.Context, params ResearchSearchParams, channel string) (int, error) {
	where, args := buildResearchEvidenceFilters(params, channel)
	where, args = applyResearchQueryFilter(params.Query, where, args,
		"es.search_vector", "(es.title ILIKE $%d OR es.description ILIKE $%d)")

	return db.executeCountQuery(ctx, `
		SELECT COUNT(DISTINCT es.id)
		FROM evidence_sources es
		LEFT JOIN item_evidence ie ON ie.evidence_id = es.id
		LEFT JOIN items i ON i.id = ie.item_id
		LEFT JOIN raw_messages rm ON i.raw_message_id = rm.id
		LEFT JOIN channels c ON rm.channel_id = c.id
		WHERE %s
	`, where, args, "count research evidence")
}

// applyResearchQueryFilter adds FTS or ILIKE filter based on query length.
func applyResearchQueryFilter(query string, where []string, args []any, ftsColumn, ilikePattern string) ([]string, []any) {
	if query == "" {
		return where, args
	}

	if len([]rune(query)) >= 3 {
		args = append(args, query)
		where = append(where, fmt.Sprintf("%s @@ plainto_tsquery('simple', $%d)", ftsColumn, len(args)))
	} else {
		pattern := "%" + SanitizeUTF8(query) + "%"
		args = append(args, pattern)
		where = append(where, fmt.Sprintf(ilikePattern, len(args), len(args)))
	}

	return where, args
}

// executeCountQuery runs a COUNT query and returns the result.
func (db *DB) executeCountQuery(ctx context.Context, queryTemplate string, where []string, args []any, errContext string) (int, error) {
	row := db.Pool.QueryRow(ctx, fmt.Sprintf(queryTemplate, strings.Join(where, sqlAndJoin)), args...)

	var total int
	if err := row.Scan(&total); err != nil {
		return 0, fmt.Errorf("%s: %w", errContext, err)
	}

	return total, nil
}

func buildResearchItemFilters(params ResearchSearchParams, channel string) ([]string, []any) {
	return buildResearchCommonFilters(params, channel)
}

func buildResearchEvidenceFilters(params ResearchSearchParams, channel string) ([]string, []any) {
	return buildResearchCommonFilters(params, channel)
}

// buildResearchCommonFilters builds shared filter conditions for research queries.
func buildResearchCommonFilters(params ResearchSearchParams, channel string) ([]string, []any) {
	where := []string{"1=1"}
	args := make([]any, 0)

	if params.From != nil {
		args = append(args, *params.From)
		where = append(where, fmt.Sprintf(fmtDateFrom, len(args)))
	}

	if params.To != nil {
		args = append(args, *params.To)
		where = append(where, fmt.Sprintf(fmtDateTo, len(args)))
	}

	if channel != "" {
		args = append(args, channel)
		where = append(where, fmt.Sprintf("c.username = $%d", len(args)))
	}

	if params.Topic != "" {
		args = append(args, params.Topic)
		where = append(where, fmt.Sprintf("i.topic = $%d", len(args)))
	}

	if params.Lang != "" {
		args = append(args, params.Lang)
		where = append(where, fmt.Sprintf("i.language = $%d", len(args)))
	}

	return where, args
}

// Normalize username by trimming @.
// ResearchClusterDetail contains cluster metadata and items.
type ResearchClusterDetail struct {
	ClusterID      string
	Topic          string
	FirstSeenAt    time.Time
	LastSeenAt     time.Time
	ItemCount      int
	UniqueChannels int
	Items          []ResearchClusterItem
	Timeline       []ResearchClusterTimeline
	Channels       []ResearchClusterChannel
}

// ResearchClusterItem is a cluster item view.
type ResearchClusterItem struct {
	ItemID          string
	Summary         string
	Text            string
	TGDate          time.Time
	Importance      float32
	Relevance       float32
	ChannelUsername string
	ChannelTitle    string
	ChannelPeerID   int64
	MessageID       int64
}

// ResearchClusterTimeline is a time bucket for cluster items.
type ResearchClusterTimeline struct {
	BucketDate time.Time
	ItemCount  int
}

// ResearchClusterChannel is a channel contribution summary.
type ResearchClusterChannel struct {
	ChannelID       string
	ChannelTitle    string
	ChannelUsername string
	ItemCount       int
}

// GetResearchCluster returns cluster detail by cluster id.
func (db *DB) GetResearchCluster(ctx context.Context, clusterID string) (*ResearchClusterDetail, error) {
	clusterUUID := toUUID(clusterID)

	var detail ResearchClusterDetail

	row := db.Pool.QueryRow(ctx, `
		SELECT c.id,
		       c.topic,
		       MIN(rm.tg_date) AS first_seen,
		       MAX(rm.tg_date) AS last_seen,
		       COUNT(*) AS item_count,
		       COUNT(DISTINCT ch.id) AS unique_channels
		FROM clusters c
		JOIN cluster_items ci ON c.id = ci.cluster_id
		JOIN items i ON ci.item_id = i.id
		JOIN raw_messages rm ON i.raw_message_id = rm.id
		JOIN channels ch ON rm.channel_id = ch.id
		WHERE c.id = $1
		GROUP BY c.id, c.topic
	`, clusterUUID)

	var (
		clusterIDRaw pgtype.UUID
		topic        pgtype.Text
		firstSeen    pgtype.Timestamptz
		lastSeen     pgtype.Timestamptz
		itemCount    int
		unique       int
	)

	if err := row.Scan(&clusterIDRaw, &topic, &firstSeen, &lastSeen, &itemCount, &unique); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrResearchClusterNotFound
		}

		return nil, fmt.Errorf("get research cluster: %w", err)
	}

	detail.ClusterID = fromUUID(clusterIDRaw)
	detail.Topic = topic.String
	detail.FirstSeenAt = firstSeen.Time
	detail.LastSeenAt = lastSeen.Time
	detail.ItemCount = itemCount
	detail.UniqueChannels = unique

	items, err := db.getResearchClusterItems(ctx, clusterUUID)
	if err != nil {
		return nil, err
	}

	detail.Items = items

	timeline, err := db.getResearchClusterTimeline(ctx, clusterUUID)
	if err != nil {
		return nil, err
	}

	detail.Timeline = timeline

	channels, err := db.getResearchClusterChannels(ctx, clusterUUID)
	if err != nil {
		return nil, err
	}

	detail.Channels = channels

	return &detail, nil
}

func (db *DB) getResearchClusterItems(ctx context.Context, clusterUUID pgtype.UUID) ([]ResearchClusterItem, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT i.id,
		       i.summary,
		       rm.text,
		       rm.tg_date,
		       i.importance_score,
		       i.relevance_score,
		       ch.username,
		       ch.title,
		       ch.tg_peer_id,
		       rm.tg_message_id
		FROM cluster_items ci
		JOIN items i ON ci.item_id = i.id
		JOIN raw_messages rm ON i.raw_message_id = rm.id
		JOIN channels ch ON rm.channel_id = ch.id
		WHERE ci.cluster_id = $1
		ORDER BY i.importance_score DESC
	`, clusterUUID)
	if err != nil {
		return nil, fmt.Errorf("get research cluster items: %w", err)
	}
	defer rows.Close()

	items := []ResearchClusterItem{}

	for rows.Next() {
		var (
			itemID   pgtype.UUID
			summary  pgtype.Text
			text     pgtype.Text
			tgDate   pgtype.Timestamptz
			username pgtype.Text
			title    pgtype.Text
			peerID   pgtype.Int8
			msgID    pgtype.Int8
		)

		item := ResearchClusterItem{}
		if err := rows.Scan(&itemID, &summary, &text, &tgDate, &item.Importance, &item.Relevance, &username, &title, &peerID, &msgID); err != nil {
			return nil, fmt.Errorf("scan research cluster item: %w", err)
		}

		item.ItemID = fromUUID(itemID)
		item.Summary = summary.String

		item.Text = text.String
		if tgDate.Valid {
			item.TGDate = tgDate.Time
		}

		item.ChannelUsername = username.String

		item.ChannelTitle = title.String
		if peerID.Valid {
			item.ChannelPeerID = peerID.Int64
		}

		if msgID.Valid {
			item.MessageID = msgID.Int64
		}

		items = append(items, item)
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate research cluster items: %w", rows.Err())
	}

	return items, nil
}

func (db *DB) getResearchClusterTimeline(ctx context.Context, clusterUUID pgtype.UUID) ([]ResearchClusterTimeline, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT date_trunc('day', rm.tg_date) AS bucket_date,
		       COUNT(*) AS item_count
		FROM cluster_items ci
		JOIN items i ON ci.item_id = i.id
		JOIN raw_messages rm ON i.raw_message_id = rm.id
		WHERE ci.cluster_id = $1
		GROUP BY bucket_date
		ORDER BY bucket_date
	`, clusterUUID)
	if err != nil {
		return nil, fmt.Errorf("get research cluster timeline: %w", err)
	}
	defer rows.Close()

	timeline := []ResearchClusterTimeline{}

	for rows.Next() {
		var (
			bucket pgtype.Timestamptz
			count  int
		)
		if err := rows.Scan(&bucket, &count); err != nil {
			return nil, fmt.Errorf("scan research cluster timeline: %w", err)
		}

		entry := ResearchClusterTimeline{ItemCount: count}
		if bucket.Valid {
			entry.BucketDate = bucket.Time
		}

		timeline = append(timeline, entry)
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate research cluster timeline: %w", rows.Err())
	}

	return timeline, nil
}

func (db *DB) getResearchClusterChannels(ctx context.Context, clusterUUID pgtype.UUID) ([]ResearchClusterChannel, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT ch.id, ch.title, ch.username, COUNT(*) AS item_count
		FROM cluster_items ci
		JOIN items i ON ci.item_id = i.id
		JOIN raw_messages rm ON i.raw_message_id = rm.id
		JOIN channels ch ON rm.channel_id = ch.id
		WHERE ci.cluster_id = $1
		GROUP BY ch.id, ch.title, ch.username
		ORDER BY item_count DESC
	`, clusterUUID)
	if err != nil {
		return nil, fmt.Errorf("get research cluster channels: %w", err)
	}
	defer rows.Close()

	channels := []ResearchClusterChannel{}

	for rows.Next() {
		var (
			channelID pgtype.UUID
			title     pgtype.Text
			username  pgtype.Text
			count     int
		)

		entry := ResearchClusterChannel{ItemCount: count}
		if err := rows.Scan(&channelID, &title, &username, &count); err != nil {
			return nil, fmt.Errorf("scan research cluster channel: %w", err)
		}

		entry.ChannelID = fromUUID(channelID)
		entry.ChannelTitle = title.String
		entry.ChannelUsername = username.String
		entry.ItemCount = count
		channels = append(channels, entry)
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate research cluster channels: %w", rows.Err())
	}

	return channels, nil
}

// ResearchChannelOverlapEdge represents overlap metrics between channels.
type ResearchChannelOverlapEdge struct {
	ChannelA string
	ChannelB string
	Shared   int
	TotalA   int
	TotalB   int
	Jaccard  float64
}

// GetChannelOverlap returns overlap edges, optionally filtered by time range.
func (db *DB) GetChannelOverlap(ctx context.Context, from, to *time.Time, limit int) ([]ResearchChannelOverlapEdge, error) {
	if limit <= 0 {
		limit = 200
	}

	var (
		rows pgx.Rows
		err  error
	)

	if from == nil && to == nil {
		rows, err = db.Pool.Query(ctx, `
			SELECT channel_a, channel_b, shared_clusters, total_a, total_b, jaccard
			FROM mv_channel_overlap
			ORDER BY jaccard DESC
			LIMIT $1
		`, safeIntToInt32(limit))
	} else {
		args := []any{}
		where := []string{"1=1"}

		if from != nil {
			args = append(args, *from)
			where = append(where, fmt.Sprintf(fmtDateFrom, len(args)))
		}

		if to != nil {
			args = append(args, *to)
			where = append(where, fmt.Sprintf(fmtDateTo, len(args)))
		}

		args = append(args, safeIntToInt32(limit))
		query := fmt.Sprintf(`
			WITH channel_clusters AS (
				SELECT ch.id AS channel_id, ci.cluster_id
				FROM cluster_items ci
				JOIN items i ON ci.item_id = i.id
				JOIN raw_messages rm ON i.raw_message_id = rm.id
				JOIN channels ch ON rm.channel_id = ch.id
				WHERE %s
				GROUP BY ch.id, ci.cluster_id
			),
			cluster_counts AS (
				SELECT channel_id, COUNT(*) AS total_clusters
				FROM channel_clusters
				GROUP BY channel_id
			),
			shared AS (
				SELECT c1.channel_id AS channel_a, c2.channel_id AS channel_b, COUNT(*) AS shared_clusters
				FROM channel_clusters c1
				JOIN channel_clusters c2
				  ON c1.cluster_id = c2.cluster_id AND c1.channel_id < c2.channel_id
				GROUP BY c1.channel_id, c2.channel_id
			)
			SELECT s.channel_a,
			       s.channel_b,
			       s.shared_clusters,
			       c1.total_clusters AS total_a,
			       c2.total_clusters AS total_b,
			       (s.shared_clusters::double precision / (c1.total_clusters + c2.total_clusters - s.shared_clusters)) AS jaccard
			FROM shared s
			JOIN cluster_counts c1 ON s.channel_a = c1.channel_id
			JOIN cluster_counts c2 ON s.channel_b = c2.channel_id
			ORDER BY jaccard DESC
			LIMIT $%d
		`, strings.Join(where, sqlAndJoin), len(args))
		rows, err = db.Pool.Query(ctx, query, args...)
	}

	if err != nil {
		return nil, fmt.Errorf("get channel overlap: %w", err)
	}

	defer rows.Close()

	results := []ResearchChannelOverlapEdge{}

	for rows.Next() {
		var (
			channelA pgtype.UUID
			channelB pgtype.UUID
			shared   int
			totalA   int
			totalB   int
			jaccard  float64
		)
		if err := rows.Scan(&channelA, &channelB, &shared, &totalA, &totalB, &jaccard); err != nil {
			return nil, fmt.Errorf("scan channel overlap: %w", err)
		}

		results = append(results, ResearchChannelOverlapEdge{
			ChannelA: fromUUID(channelA),
			ChannelB: fromUUID(channelB),
			Shared:   shared,
			TotalA:   totalA,
			TotalB:   totalB,
			Jaccard:  jaccard,
		})
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate channel overlap: %w", rows.Err())
	}

	return results, nil
}

// ResearchTopicTimelinePoint represents topic timeline buckets.
type ResearchTopicTimelinePoint struct {
	BucketDate    time.Time
	Topic         string
	ItemCount     int
	AvgImportance float64
	AvgRelevance  float64
}

// buildTimelineFilters builds the where clause and args for timeline queries.
func buildTimelineFilters(from, to *time.Time, args []any) ([]string, []any) {
	where := []string{"1=1"}

	if from != nil {
		args = append(args, *from)
		where = append(where, fmt.Sprintf(fmtDateFrom, len(args)))
	}

	if to != nil {
		args = append(args, *to)
		where = append(where, fmt.Sprintf(fmtDateTo, len(args)))
	}

	return where, args
}

// GetTopicTimeline returns topic timeline data.
func (db *DB) GetTopicTimeline(ctx context.Context, bucket string, from, to *time.Time, limit int) ([]ResearchTopicTimelinePoint, error) {
	if bucket == "" {
		bucket = "week"
	}

	if limit <= 0 {
		limit = 200
	}

	args := []any{bucket}
	where, args := buildTimelineFilters(from, to, args)

	query := fmt.Sprintf(`
		SELECT date_trunc($1, rm.tg_date) AS bucket_date,
		       i.topic,
		       COUNT(*) AS item_count,
		       AVG(i.importance_score) AS avg_importance,
		       AVG(i.relevance_score) AS avg_relevance
		FROM items i
		JOIN raw_messages rm ON i.raw_message_id = rm.id
		WHERE %s
		GROUP BY bucket_date, i.topic
		ORDER BY bucket_date DESC
		LIMIT %d
	`, strings.Join(where, sqlAndJoin), limit)

	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get topic timeline: %w", err)
	}
	defer rows.Close()

	points := []ResearchTopicTimelinePoint{}

	for rows.Next() {
		var (
			bucketDate pgtype.Timestamptz
			topic      pgtype.Text
			count      int
			avgImp     pgtype.Float8
			avgRel     pgtype.Float8
		)
		if err := rows.Scan(&bucketDate, &topic, &count, &avgImp, &avgRel); err != nil {
			return nil, fmt.Errorf("scan topic timeline: %w", err)
		}

		entry := ResearchTopicTimelinePoint{
			Topic:     topic.String,
			ItemCount: count,
		}
		if bucketDate.Valid {
			entry.BucketDate = bucketDate.Time
		}

		if avgImp.Valid {
			entry.AvgImportance = avgImp.Float64
		}

		if avgRel.Valid {
			entry.AvgRelevance = avgRel.Float64
		}

		points = append(points, entry)
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate topic timeline: %w", rows.Err())
	}

	return points, nil
}

// ResearchChannelQualityEntry represents channel quality history.
type ResearchChannelQualityEntry struct {
	PeriodStart   time.Time
	PeriodEnd     time.Time
	InclusionRate float64
	NoiseRate     float64
	AvgImportance float64
	AvgRelevance  float64
}

// scanChannelQualityRow scans a single channel quality row.
func scanChannelQualityRow(rows pgx.Rows) (ResearchChannelQualityEntry, error) {
	var (
		start     pgtype.Date
		end       pgtype.Date
		inclusion pgtype.Float8
		noise     pgtype.Float8
		avgImp    pgtype.Float8
		avgRel    pgtype.Float8
	)

	if err := rows.Scan(&start, &end, &inclusion, &noise, &avgImp, &avgRel); err != nil {
		return ResearchChannelQualityEntry{}, fmt.Errorf("scan channel quality history: %w", err)
	}

	return buildChannelQualityEntry(start, end, inclusion, noise, avgImp, avgRel), nil
}

// buildChannelQualityEntry builds a ResearchChannelQualityEntry from nullable values.
func buildChannelQualityEntry(
	start, end pgtype.Date,
	inclusion, noise, avgImp, avgRel pgtype.Float8,
) ResearchChannelQualityEntry {
	entry := ResearchChannelQualityEntry{}

	if start.Valid {
		entry.PeriodStart = start.Time
	}

	if end.Valid {
		entry.PeriodEnd = end.Time
	}

	if inclusion.Valid {
		entry.InclusionRate = inclusion.Float64
	}

	if noise.Valid {
		entry.NoiseRate = noise.Float64
	}

	if avgImp.Valid {
		entry.AvgImportance = avgImp.Float64
	}

	if avgRel.Valid {
		entry.AvgRelevance = avgRel.Float64
	}

	return entry
}

// buildQualityHistoryFilters builds the where clause for quality history queries.
func buildQualityHistoryFilters(channelID string, from, to *time.Time) ([]string, []any) {
	args := []any{toUUID(channelID)}
	where := []string{"channel_id = $1"}

	if from != nil {
		args = append(args, *from)
		where = append(where, fmt.Sprintf("period_start >= $%d", len(args)))
	}

	if to != nil {
		args = append(args, *to)
		where = append(where, fmt.Sprintf("period_end <= $%d", len(args)))
	}

	return where, args
}

// GetChannelQualityHistory returns quality history entries for a channel.
func (db *DB) GetChannelQualityHistory(ctx context.Context, channelID string, from, to *time.Time) ([]ResearchChannelQualityEntry, error) {
	where, args := buildQualityHistoryFilters(channelID, from, to)

	rows, err := db.Pool.Query(ctx, fmt.Sprintf(`
		SELECT period_start, period_end, inclusion_rate, noise_rate, avg_importance, avg_relevance
		FROM channel_quality_history
		WHERE %s
		ORDER BY period_start DESC
	`, strings.Join(where, sqlAndJoin)), args...)
	if err != nil {
		return nil, fmt.Errorf("get channel quality history: %w", err)
	}
	defer rows.Close()

	entries := []ResearchChannelQualityEntry{}

	for rows.Next() {
		entry, err := scanChannelQualityRow(rows)
		if err != nil {
			return nil, err
		}

		entries = append(entries, entry)
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate channel quality history: %w", rows.Err())
	}

	return entries, nil
}

// ResearchClaimEntry represents a claim ledger row.
type ResearchClaimEntry struct {
	ID              string
	ClaimText       string
	FirstSeenAt     time.Time
	OriginClusterID string
	ClusterIDs      []string
	ContradictedBy  []string
}

// GetClaims returns claim ledger entries.
func (db *DB) GetClaims(ctx context.Context, from, to *time.Time, limit int) ([]ResearchClaimEntry, error) {
	if limit <= 0 {
		limit = 200
	}

	args := []any{}
	where := []string{"1=1"}

	if from != nil {
		args = append(args, *from)
		where = append(where, fmt.Sprintf("first_seen_at >= $%d", len(args)))
	}

	if to != nil {
		args = append(args, *to)
		where = append(where, fmt.Sprintf("first_seen_at <= $%d", len(args)))
	}

	rows, err := db.Pool.Query(ctx, fmt.Sprintf(`
		SELECT id, claim_text, first_seen_at, origin_cluster_id, cluster_ids, contradicted_by
		FROM claims
		WHERE %s
		ORDER BY first_seen_at DESC
		LIMIT %d
	`, strings.Join(where, sqlAndJoin), limit), args...)
	if err != nil {
		return nil, fmt.Errorf("get claims: %w", err)
	}
	defer rows.Close()

	results := []ResearchClaimEntry{}

	for rows.Next() {
		var (
			id           pgtype.UUID
			text         pgtype.Text
			first        pgtype.Timestamptz
			origin       pgtype.UUID
			clusterIDs   pgtype.Array[pgtype.UUID]
			contradicted pgtype.Array[pgtype.UUID]
		)
		if err := rows.Scan(&id, &text, &first, &origin, &clusterIDs, &contradicted); err != nil {
			return nil, fmt.Errorf("scan claims: %w", err)
		}

		entry := ResearchClaimEntry{
			ID:        fromUUID(id),
			ClaimText: text.String,
		}
		if first.Valid {
			entry.FirstSeenAt = first.Time
		}

		entry.OriginClusterID = fromUUID(origin)
		entry.ClusterIDs = uuidArrayToStrings(clusterIDs)
		entry.ContradictedBy = uuidArrayToStrings(contradicted)
		results = append(results, entry)
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf(errFmtIterateClaims, rows.Err())
	}

	return results, nil
}

// ResearchOriginStats represents origin vs amplifier stats.
type ResearchOriginStats struct {
	ChannelID     string
	OriginCount   int
	TotalCount    int
	OriginRate    float64
	AmplifierRate float64
}

// GetOriginStats returns origin vs amplifier stats for a channel.
func (db *DB) GetOriginStats(ctx context.Context, channelID string, from, to *time.Time) (*ResearchOriginStats, error) {
	channelUUID := toUUID(channelID)
	args := []any{channelUUID}
	where := []string{"cfa.channel_id = $1"}

	if from != nil {
		args = append(args, *from)
		where = append(where, fmt.Sprintf("cfa.first_seen_at >= $%d", len(args)))
	}

	if to != nil {
		args = append(args, *to)
		where = append(where, fmt.Sprintf("cfa.first_seen_at <= $%d", len(args)))
	}

	row := db.Pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT COUNT(*)
		FROM cluster_first_appearance cfa
		WHERE %s
	`, strings.Join(where, sqlAndJoin)), args...)

	var originCount int
	if err := row.Scan(&originCount); err != nil {
		return nil, fmt.Errorf("count origin clusters: %w", err)
	}

	// Total clusters where channel appears
	args = []any{channelUUID}
	where = []string{"ch.id = $1"}

	if from != nil {
		args = append(args, *from)
		where = append(where, fmt.Sprintf(fmtDateFrom, len(args)))
	}

	if to != nil {
		args = append(args, *to)
		where = append(where, fmt.Sprintf(fmtDateTo, len(args)))
	}

	row = db.Pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT COUNT(DISTINCT ci.cluster_id)
		FROM cluster_items ci
		JOIN items i ON ci.item_id = i.id
		JOIN raw_messages rm ON i.raw_message_id = rm.id
		JOIN channels ch ON rm.channel_id = ch.id
		WHERE %s
	`, strings.Join(where, sqlAndJoin)), args...)

	var totalCount int
	if err := row.Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("count total clusters: %w", err)
	}

	stats := &ResearchOriginStats{
		ChannelID:   channelID,
		OriginCount: originCount,
		TotalCount:  totalCount,
	}
	if totalCount > 0 {
		stats.OriginRate = float64(originCount) / float64(totalCount)
		stats.AmplifierRate = 1 - stats.OriginRate
	}

	return stats, nil
}

// ResearchWeeklyDiff represents weekly topic diff summary.
type ResearchWeeklyDiff struct {
	Topic string
	Delta int
}

// ResearchWeeklyChannelDiff represents weekly channel diff summary.
type ResearchWeeklyChannelDiff struct {
	ChannelID    string
	ChannelTitle string
	Delta        int
}

type ChannelRelevanceSettings struct {
	RelevanceThreshold      float32
	AutoRelevanceEnabled    bool
	RelevanceThresholdDelta float32
}

type RelevanceGateDecision struct {
	Decision    string
	Confidence  float32
	Reason      string
	Model       string
	GateVersion string
}

// GetWeeklyDiff returns top topic deltas between two ranges.
func (db *DB) GetWeeklyDiff(ctx context.Context, from, to time.Time, limit int) ([]ResearchWeeklyDiff, error) {
	if limit <= 0 {
		limit = 10
	}

	duration := to.Sub(from)
	prevFrom := from.Add(-duration)
	prevTo := from

	rows, err := db.Pool.Query(ctx, `
		WITH current AS (
			SELECT i.topic, COUNT(*) AS cnt
			FROM items i
			JOIN raw_messages rm ON i.raw_message_id = rm.id
			WHERE rm.tg_date >= $1 AND rm.tg_date < $2
			GROUP BY i.topic
		),
		prev AS (
			SELECT i.topic, COUNT(*) AS cnt
			FROM items i
			JOIN raw_messages rm ON i.raw_message_id = rm.id
			WHERE rm.tg_date >= $3 AND rm.tg_date < $4
			GROUP BY i.topic
		)
		SELECT COALESCE(c.topic, p.topic) AS topic,
		       COALESCE(c.cnt, 0) - COALESCE(p.cnt, 0) AS delta
		FROM current c
		FULL OUTER JOIN prev p ON c.topic = p.topic
		ORDER BY delta DESC
		LIMIT $5
	`, from, to, prevFrom, prevTo, safeIntToInt32(limit))
	if err != nil {
		return nil, fmt.Errorf("get weekly diff: %w", err)
	}
	defer rows.Close()

	results := []ResearchWeeklyDiff{}

	for rows.Next() {
		var (
			topic pgtype.Text
			delta int
		)

		if err := rows.Scan(&topic, &delta); err != nil {
			return nil, fmt.Errorf("scan weekly diff: %w", err)
		}

		results = append(results, ResearchWeeklyDiff{
			Topic: topic.String,
			Delta: delta,
		})
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate weekly diff: %w", rows.Err())
	}

	return results, nil
}

// GetWeeklyChannelDiff returns top channel deltas between two ranges.
func (db *DB) GetWeeklyChannelDiff(ctx context.Context, from, to time.Time, limit int) ([]ResearchWeeklyChannelDiff, error) {
	if limit <= 0 {
		limit = 10
	}

	duration := to.Sub(from)
	prevFrom := from.Add(-duration)
	prevTo := from

	rows, err := db.Pool.Query(ctx, `
		WITH current AS (
			SELECT ch.id AS channel_id, ch.title AS channel_title, COUNT(*) AS cnt
			FROM items i
			JOIN raw_messages rm ON i.raw_message_id = rm.id
			JOIN channels ch ON rm.channel_id = ch.id
			WHERE rm.tg_date >= $1 AND rm.tg_date < $2
			GROUP BY ch.id, ch.title
		),
		prev AS (
			SELECT ch.id AS channel_id, ch.title AS channel_title, COUNT(*) AS cnt
			FROM items i
			JOIN raw_messages rm ON i.raw_message_id = rm.id
			JOIN channels ch ON rm.channel_id = ch.id
			WHERE rm.tg_date >= $3 AND rm.tg_date < $4
			GROUP BY ch.id, ch.title
		)
		SELECT COALESCE(c.channel_id, p.channel_id) AS channel_id,
		       COALESCE(c.channel_title, p.channel_title) AS channel_title,
		       COALESCE(c.cnt, 0) - COALESCE(p.cnt, 0) AS delta
		FROM current c
		FULL OUTER JOIN prev p ON c.channel_id = p.channel_id
		ORDER BY delta DESC
		LIMIT $5
	`, from, to, prevFrom, prevTo, safeIntToInt32(limit))
	if err != nil {
		return nil, fmt.Errorf("get weekly channel diff: %w", err)
	}
	defer rows.Close()

	results := []ResearchWeeklyChannelDiff{}

	for rows.Next() {
		var (
			channelID    pgtype.UUID
			channelTitle pgtype.Text
			delta        int
		)
		if err := rows.Scan(&channelID, &channelTitle, &delta); err != nil {
			return nil, fmt.Errorf("scan weekly channel diff: %w", err)
		}

		results = append(results, ResearchWeeklyChannelDiff{
			ChannelID:    fromUUID(channelID),
			ChannelTitle: channelTitle.String,
			Delta:        delta,
		})
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate weekly channel diff: %w", rows.Err())
	}

	return results, nil
}

// ResearchSession represents a session row.
type ResearchSession struct {
	Token     string
	UserID    int64
	ExpiresAt time.Time
	CreatedAt time.Time
}

// CreateResearchSession stores a new session.
func (db *DB) CreateResearchSession(ctx context.Context, token string, userID int64, expiresAt time.Time) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO research_sessions (token, user_id, expires_at)
		VALUES ($1, $2, $3)
	`, token, userID, expiresAt)
	if err != nil {
		return fmt.Errorf("create research session: %w", err)
	}

	return nil
}

// GetResearchSession retrieves a session by token.
func (db *DB) GetResearchSession(ctx context.Context, token string) (*ResearchSession, error) {
	row := db.Pool.QueryRow(ctx, `
		SELECT token, user_id, expires_at, created_at
		FROM research_sessions
		WHERE token = $1
	`, token)

	var (
		userID  pgtype.Int8
		expires pgtype.Timestamptz
		created pgtype.Timestamptz
	)

	session := ResearchSession{Token: token}
	if err := row.Scan(&session.Token, &userID, &expires, &created); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrResearchSessionNotFound
		}

		return nil, fmt.Errorf("get research session: %w", err)
	}

	if userID.Valid {
		session.UserID = userID.Int64
	}

	if expires.Valid {
		session.ExpiresAt = expires.Time
	}

	if created.Valid {
		session.CreatedAt = created.Time
	}

	return &session, nil
}

// DeleteExpiredResearchSessions removes expired sessions.
func (db *DB) DeleteExpiredResearchSessions(ctx context.Context) error {
	_, err := db.Pool.Exec(ctx, `
		DELETE FROM research_sessions
		WHERE expires_at <= now()
	`)
	if err != nil {
		return fmt.Errorf("delete expired research sessions: %w", err)
	}

	return nil
}

// RefreshResearchMaterializedViews refreshes research materialized views and derived caches.
func (db *DB) RefreshResearchMaterializedViews(ctx context.Context) error {
	views := []string{
		"mv_topic_timeline",
		"mv_channel_overlap",
		"mv_cluster_stats",
	}

	if err := db.rebuildResearchDerivedTables(ctx); err != nil {
		return err
	}

	for _, view := range views {
		if _, err := db.Pool.Exec(ctx, fmt.Sprintf("REFRESH MATERIALIZED VIEW CONCURRENTLY %s", view)); err != nil {
			return fmt.Errorf("refresh materialized view %s: %w", view, err)
		}
	}

	return nil
}

func (db *DB) rebuildResearchDerivedTables(ctx context.Context) error {
	if _, err := db.Pool.Exec(ctx, "TRUNCATE cluster_first_appearance"); err != nil {
		return fmt.Errorf("truncate cluster_first_appearance: %w", err)
	}
	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO cluster_first_appearance (cluster_id, channel_id, first_item_id, first_seen_at)
		WITH ranked AS (
			SELECT ci.cluster_id,
			       rm.channel_id,
			       i.id AS item_id,
			       rm.tg_date,
			       ROW_NUMBER() OVER (PARTITION BY ci.cluster_id ORDER BY rm.tg_date ASC) AS rn
			FROM cluster_items ci
			JOIN items i ON ci.item_id = i.id
			JOIN raw_messages rm ON i.raw_message_id = rm.id
		)
		SELECT cluster_id, channel_id, item_id, tg_date
		FROM ranked
		WHERE rn = 1
	`); err != nil {
		return fmt.Errorf("populate cluster_first_appearance: %w", err)
	}

	if _, err := db.Pool.Exec(ctx, "TRUNCATE cluster_topic_history"); err != nil {
		return fmt.Errorf("truncate cluster_topic_history: %w", err)
	}
	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO cluster_topic_history (cluster_id, topic, window_start, window_end)
		SELECT c.id,
		       c.topic,
		       MIN(rm.tg_date),
		       MAX(rm.tg_date)
		FROM clusters c
		JOIN cluster_items ci ON c.id = ci.cluster_id
		JOIN items i ON ci.item_id = i.id
		JOIN raw_messages rm ON i.raw_message_id = rm.id
		WHERE c.topic IS NOT NULL
		GROUP BY c.id, c.topic
	`); err != nil {
		return fmt.Errorf("populate cluster_topic_history: %w", err)
	}

	if _, err := db.Pool.Exec(ctx, "TRUNCATE claims"); err != nil {
		return fmt.Errorf("truncate claims: %w", err)
	}
	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO claims (claim_text, first_seen_at, origin_cluster_id, cluster_ids, contradicted_by)
		SELECT claim_text,
		       first_seen_at,
		       origin_cluster_id,
		       cluster_ids,
		       '{}'::uuid[]
		FROM (
			SELECT ec.claim_text AS claim_text,
			       MIN(rm.tg_date) AS first_seen_at,
			       (ARRAY_AGG(DISTINCT ci.cluster_id ORDER BY rm.tg_date ASC))[1] AS origin_cluster_id,
			       ARRAY_AGG(DISTINCT ci.cluster_id) AS cluster_ids
			FROM evidence_claims ec
			JOIN item_evidence ie ON ie.evidence_id = ec.evidence_id
			JOIN items i ON i.id = ie.item_id
			JOIN raw_messages rm ON i.raw_message_id = rm.id
			JOIN cluster_items ci ON ci.item_id = i.id
			GROUP BY ec.claim_text
		) AS grouped
	`); err != nil {
		return fmt.Errorf("populate claims: %w", err)
	}

	return nil
}

func (db *DB) GetChannelRelevanceSettings(ctx context.Context, channelID string) (*ChannelRelevanceSettings, error) {
	row := db.Pool.QueryRow(ctx, `
		SELECT relevance_threshold,
		       auto_relevance_enabled,
		       relevance_threshold_delta
		FROM channels
		WHERE id = $1
	`, toUUID(channelID))

	var (
		threshold pgtype.Float4
		auto      pgtype.Bool
		delta     pgtype.Float4
	)

	if err := row.Scan(&threshold, &auto, &delta); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get channel relevance settings: %w", err)
	}

	return &ChannelRelevanceSettings{
		RelevanceThreshold:      threshold.Float32,
		AutoRelevanceEnabled:    auto.Bool,
		RelevanceThresholdDelta: delta.Float32,
	}, nil
}

func (db *DB) GetRelevanceGateDecision(ctx context.Context, rawMessageID string) (*RelevanceGateDecision, error) {
	row := db.Pool.QueryRow(ctx, `
		SELECT decision, confidence, reason, model, gate_version
		FROM relevance_gate_log
		WHERE raw_message_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, toUUID(rawMessageID))

	var (
		decision   pgtype.Text
		confidence pgtype.Float4
		reason     pgtype.Text
		model      pgtype.Text
		version    pgtype.Text
	)

	if err := row.Scan(&decision, &confidence, &reason, &model, &version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get relevance gate decision: %w", err)
	}

	return &RelevanceGateDecision{
		Decision:    decision.String,
		Confidence:  confidence.Float32,
		Reason:      reason.String,
		Model:       model.String,
		GateVersion: version.String,
	}, nil
}

func uuidArrayToStrings(arr pgtype.Array[pgtype.UUID]) []string {
	if !arr.Valid {
		return nil
	}

	results := make([]string, 0, len(arr.Elements))
	for _, el := range arr.Elements {
		if !el.Valid {
			continue
		}

		results = append(results, fromUUID(el))
	}

	return results
}
