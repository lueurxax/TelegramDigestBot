package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pgvector/pgvector-go"
)

// Sentinel errors for research queries.
var (
	ErrResearchClusterNotFound       = errors.New("research cluster not found")
	ErrResearchChannelNotFound       = errors.New("research channel not found")
	ErrResearchSessionNotFound       = errors.New("research session not found")
	ErrChannelRelevanceNotConfigured = errors.New("channel relevance not configured")
	ErrRelevanceGateNotFound         = errors.New("relevance gate decision not found")
)

const errIterateResearchItems = "iterate research items: %w"

// nullableFloat64 extracts a float64 from a pgtype.Float8, returning 0 if not valid.
func nullableFloat64(v pgtype.Float8) float64 {
	if v.Valid {
		return v.Float64
	}

	return 0
}

// nullableDate extracts a time.Time from a pgtype.Date, returning zero time if not valid.
func nullableDate(v pgtype.Date) time.Time {
	if v.Valid {
		return v.Time
	}

	return time.Time{}
}

const (
	// Time bucket constants for PostgreSQL date_trunc.
	bucketWeek  = "week"
	bucketDay   = "day"
	bucketMonth = "month"

	defaultSearchLimit     = 50
	maxSearchLimit         = 200
	defaultTopicDriftLimit = 100
	recencyHalfLifeDays    = 14.0
	maxOverlapChannels     = 200
	topicDriftMinJaccard   = 0.6
	topicDriftMinEmbedding = 0.6
	langLinkMinSimilarity  = 0.8
	langLinkMaxLagSeconds  = 604800
	originTopicLimit       = 5
	retentionItemsMonths   = 18

	// Log field names.
	logFieldView = "view"

	// Slice preallocation capacity for timeline queries.
	timelineArgsCapacity = 2

	// SQL join constant.
	sqlAndJoin = " AND "

	// SQL format patterns for building dynamic queries.
	fmtScoreExpr       = "(0.5 * i.importance_score + 0.5 * exp(-extract(epoch from ($%d - rm.tg_date)) / 86400 / %.1f))"
	fmtTextIlike       = "(i.summary ILIKE $%d OR rm.text ILIKE $%d OR c.title ILIKE $%d OR c.username ILIKE $%d)"
	fmtSearchTS        = "(i.search_vector @@ plainto_tsquery('simple', $%d) OR c.title ILIKE $%d OR c.username ILIKE $%d)"
	fmtEvidenceIlike   = "(es.title ILIKE $%d OR es.description ILIKE $%d)"
	fmtDateFrom        = "rm.tg_date >= $%d"
	fmtDateTo          = "rm.tg_date <= $%d"
	fmtEvidenceDateGte = "es.crawled_at >= $%d"
	fmtEvidenceDateLte = "es.crawled_at <= $%d"
	fmtCfaDateFrom     = "cfa.first_seen_at >= $%d"
	fmtCfaDateTo       = "cfa.first_seen_at <= $%d"

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
			       cl.cluster_id,
			       ev.evidence_count,
			       ir.rating,
			       ir.feedback,
			       ir.source,
			       ir.created_at,
			       %s AS score
			FROM items i
			JOIN raw_messages rm ON i.raw_message_id = rm.id
			JOIN channels c ON rm.channel_id = c.id
			LEFT JOIN LATERAL (
				SELECT ci.cluster_id
				FROM cluster_items ci
				JOIN clusters c2 ON ci.cluster_id = c2.id AND c2.source = 'research'
				WHERE ci.item_id = i.id
				LIMIT 1
			) cl ON true
			LEFT JOIN LATERAL (
				SELECT COUNT(*) AS evidence_count
				FROM item_evidence ie
				WHERE ie.item_id = i.id
			) ev ON true
			LEFT JOIN LATERAL (
				SELECT rating, feedback, source, created_at
				FROM item_ratings
				WHERE item_id = i.id
				ORDER BY created_at DESC
				LIMIT 1
			) ir ON true
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

// normalizeTimelineBucket converts bucket aliases to valid PostgreSQL date_trunc units.
func normalizeTimelineBucket(bucket string) string {
	switch bucket {
	case bucketDay, "daily":
		return bucketDay
	case bucketMonth, "monthly":
		return bucketMonth
	default:
		return bucketWeek
	}
}

// ResearchSearchParams defines filters for research search.
type ResearchSearchParams struct {
	Query        string
	From         *time.Time
	To           *time.Time
	SearchAt     time.Time
	Channel      string
	Topic        string
	Lang         string
	Provider     string
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
	ClusterID       string
	EvidenceCount   int
	LastRating      string
	LastFeedback    string
	LastSource      string
	LastRatedAt     *time.Time
	Score           float64
	NeedsReview     bool
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

// ResearchChannelRef holds basic channel identity info.
type ResearchChannelRef struct {
	ID       string
	Username string
	Title    string
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

// ResolveChannelRef resolves a channel identifier (UUID or @username) to a channel reference.
func (db *DB) ResolveChannelRef(ctx context.Context, value string) (*ResearchChannelRef, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, ErrResearchChannelNotFound
	}

	trimmed = strings.TrimPrefix(trimmed, "@")

	if id := toUUID(trimmed); id.Valid {
		row := db.Pool.QueryRow(ctx, `
			SELECT id, username, title
			FROM channels
			WHERE id = $1
		`, id)

		return scanChannelRef(row)
	}

	normalized := normalizeUsername(trimmed)
	row := db.Pool.QueryRow(ctx, `
		SELECT id, username, title
		FROM channels
		WHERE username = $1
	`, normalized)

	return scanChannelRef(row)
}

func scanChannelRef(row pgx.Row) (*ResearchChannelRef, error) {
	var (
		id       pgtype.UUID
		username pgtype.Text
		title    pgtype.Text
	)

	if err := row.Scan(&id, &username, &title); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrResearchChannelNotFound
		}

		return nil, fmt.Errorf("get channel: %w", err)
	}

	return &ResearchChannelRef{
		ID:       fromUUID(id),
		Username: username.String,
		Title:    title.String,
	}, nil
}

// buildItemSearchQuery builds the SQL query and args for item search.
func buildItemSearchQuery(params ResearchSearchParams, where []string, args []any, limit int) (string, []any) {
	if params.Query != "" && len([]rune(params.Query)) >= 3 {
		args = append(args, params.Query)
		tsQueryIdx := len(args)
		rankExpr := fmt.Sprintf("ts_rank_cd(i.search_vector, plainto_tsquery('simple', $%d))", tsQueryIdx)

		pattern := "%" + SanitizeUTF8(params.Query) + "%"
		args = append(args, pattern)
		patternIdx := len(args)

		where = append(where, fmt.Sprintf(
			fmtSearchTS,
			tsQueryIdx,
			patternIdx,
			patternIdx,
		))

		args = append(args, toTimestamptz(params.SearchAt))
		scoreIdx := len(args)
		scoreExpr := fmt.Sprintf("(0.5 * %s + 0.3 * i.importance_score + 0.2 * exp(-extract(epoch from ($%d - rm.tg_date)) / 86400 / %.1f))", rankExpr, scoreIdx, recencyHalfLifeDays)

		return fmt.Sprintf(sqlSearchItems, scoreExpr, strings.Join(where, sqlAndJoin), limit, params.Offset), args
	}

	if params.Query != "" {
		pattern := "%" + SanitizeUTF8(params.Query) + "%"
		args = append(args, pattern)
		patternIdx := len(args)
		where = append(where, fmt.Sprintf(fmtTextIlike, patternIdx, patternIdx, patternIdx, patternIdx))

		args = append(args, toTimestamptz(params.SearchAt))
		scoreIdx := len(args)
		scoreExpr := fmt.Sprintf(fmtScoreExpr, scoreIdx, recencyHalfLifeDays)

		return fmt.Sprintf(sqlSearchItems, scoreExpr, strings.Join(where, sqlAndJoin), limit, params.Offset), args
	}

	args = append(args, toTimestamptz(params.SearchAt))
	scoreIdx := len(args)
	scoreExpr := fmt.Sprintf(fmtScoreExpr, scoreIdx, recencyHalfLifeDays)

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
			itemID        pgtype.UUID
			summary       pgtype.Text
			topic         pgtype.Text
			text          pgtype.Text
			user          pgtype.Text
			title         pgtype.Text
			clusterID     pgtype.UUID
			evidenceCount int64
			lastRating    pgtype.Text
			lastFeedback  pgtype.Text
			lastSource    pgtype.Text
			lastRatedAt   pgtype.Timestamptz
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
			&clusterID,
			&evidenceCount,
			&lastRating,
			&lastFeedback,
			&lastSource,
			&lastRatedAt,
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
		res.ClusterID = fromUUID(clusterID)
		res.EvidenceCount = int(evidenceCount)
		res.LastRating = lastRating.String
		res.LastFeedback = lastFeedback.String
		res.LastSource = lastSource.String

		if lastRatedAt.Valid {
			t := lastRatedAt.Time
			res.LastRatedAt = &t
		}

		results = append(results, res)
	}

	if rows.Err() != nil {
		return nil, nil, fmt.Errorf(errIterateResearchItems, rows.Err())
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

	if params.Query != "" && len([]rune(params.Query)) >= 3 {
		args = append(args, params.Query)
		tsQueryIdx := len(args)
		pattern := "%" + SanitizeUTF8(params.Query) + "%"
		args = append(args, pattern)
		patternIdx := len(args)

		where = append(where, fmt.Sprintf(
			fmtSearchTS,
			tsQueryIdx,
			patternIdx,
			patternIdx,
		))
	} else if params.Query != "" {
		pattern := "%" + SanitizeUTF8(params.Query) + "%"
		args = append(args, pattern)
		patternIdx := len(args)
		where = append(where, fmt.Sprintf(fmtTextIlike, patternIdx, patternIdx, patternIdx, patternIdx))
	}

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
	where, args := buildResearchCommonFilters(params, channel)

	if params.Provider != "" {
		args = append(args, params.Provider)
		where = append(where, fmt.Sprintf(`
			EXISTS (
				SELECT 1
				FROM item_evidence ie
				JOIN evidence_sources es ON es.id = ie.evidence_id
				WHERE ie.item_id = i.id AND es.provider = $%d
			)
		`, len(args)))
	}

	return where, args
}

func buildResearchEvidenceFilters(params ResearchSearchParams, channel string) ([]string, []any) {
	where, args := buildResearchCommonFilters(params, channel)

	if params.Provider != "" {
		args = append(args, params.Provider)
		where = append(where, fmt.Sprintf("es.provider = $%d", len(args)))
	}

	return where, args
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
	Canonical      string
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
		WHERE c.id = $1 AND c.source = $2
		GROUP BY c.id, c.topic
	`, clusterUUID, ClusterSourceResearch)

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
	if len(items) > 0 {
		detail.Canonical = items[0].Summary
	}

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
		JOIN clusters c ON ci.cluster_id = c.id AND c.source = $2
		JOIN items i ON ci.item_id = i.id
		JOIN raw_messages rm ON i.raw_message_id = rm.id
		JOIN channels ch ON rm.channel_id = ch.id
		WHERE ci.cluster_id = $1
		ORDER BY i.importance_score DESC
	`, clusterUUID, ClusterSourceResearch)
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
		JOIN clusters c ON ci.cluster_id = c.id AND c.source = $2
		JOIN items i ON ci.item_id = i.id
		JOIN raw_messages rm ON i.raw_message_id = rm.id
		WHERE ci.cluster_id = $1
		GROUP BY bucket_date
		ORDER BY bucket_date
	`, clusterUUID, ClusterSourceResearch)
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
		JOIN clusters c ON ci.cluster_id = c.id AND c.source = $2
		JOIN items i ON ci.item_id = i.id
		JOIN raw_messages rm ON i.raw_message_id = rm.id
		JOIN channels ch ON rm.channel_id = ch.id
		WHERE ci.cluster_id = $1
		GROUP BY ch.id, ch.title, ch.username
		ORDER BY item_count DESC
	`, clusterUUID, ClusterSourceResearch)
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
	ChannelA         string
	ChannelB         string
	ChannelATitle    string
	ChannelAUsername string
	ChannelBTitle    string
	ChannelBUsername string
	Shared           int
	TotalA           int
	TotalB           int
	Jaccard          float64
}

type ResearchChannelOverlapSummary struct {
	TotalClusters  int
	SharedClusters int
	TotalChannels  int
}

type ResearchChannelQualitySummary struct {
	ChannelID         string
	ChannelTitle      string
	ChannelUsername   string
	PeriodStart       time.Time
	PeriodEnd         time.Time
	InclusionRate     float64
	NoiseRate         float64
	AvgImportance     float64
	AvgRelevance      float64
	DigestShare       float64
	ItemsDigested     int
	RelevanceStddev   float64
	ImportanceWeight  float64
	AutoWeightEnabled bool
	WeightOverride    bool
	WeightUpdatedAt   time.Time
}

type ResearchChannelWeightEntry struct {
	ImportanceWeight  float64
	AutoWeightEnabled bool
	WeightOverride    bool
	Reason            string
	UpdatedBy         int64
	UpdatedAt         time.Time
}

type ResearchChannelBiasEntry struct {
	Topic        string
	ChannelCount int
	GlobalCount  int
	ChannelShare float64
	GlobalShare  float64
	IndexRatio   float64
}

type ResearchAgendaSimilarityEdge struct {
	ChannelA         string
	ChannelATitle    string
	ChannelAUser     string
	ChannelB         string
	ChannelBTitle    string
	ChannelBUser     string
	SharedTopics     int
	TotalTopicsA     int
	TotalTopicsB     int
	AgendaSimilarity float64
}

type ResearchLanguageCoverageEntry struct {
	Topic        string
	FromLang     string
	ToLang       string
	ClusterCount int
	AvgLagHours  float64
}

type ResearchTopicDriftEntry struct {
	ClusterID      string
	FirstTopic     string
	LastTopic      string
	DistinctTopics int
	FirstSeenAt    time.Time
	LastSeenAt     time.Time
}

type ResearchClaimsSummary struct {
	ClaimsCount          int
	EvidenceClaimsCount  int
	EvidenceItemsCount   int
	ClusterItemsCount    int
	ClusteredWithEvCount int // items in BOTH cluster_items AND item_evidence
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
			WITH top_channels AS (
				SELECT channel_id
				FROM (
					SELECT channel_a AS channel_id, total_a AS total_clusters
					FROM mv_channel_overlap
					UNION ALL
					SELECT channel_b AS channel_id, total_b AS total_clusters
					FROM mv_channel_overlap
				) ranked
				GROUP BY channel_id, total_clusters
				ORDER BY total_clusters DESC
				LIMIT $1
			)
			SELECT channel_a,
			       channel_b,
			       shared_clusters,
			       total_a,
			       total_b,
			       jaccard,
			       ca.title,
			       ca.username,
			       cb.title,
			       cb.username
			FROM mv_channel_overlap
			JOIN channels ca ON ca.id = channel_a
			JOIN channels cb ON cb.id = channel_b
			WHERE channel_a IN (SELECT channel_id FROM top_channels)
			  AND channel_b IN (SELECT channel_id FROM top_channels)
			ORDER BY jaccard DESC
			LIMIT $2
		`, safeIntToInt32(maxOverlapChannels), safeIntToInt32(limit))
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
				JOIN clusters c ON ci.cluster_id = c.id AND c.source = 'research'
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
			       (s.shared_clusters::double precision / (c1.total_clusters + c2.total_clusters - s.shared_clusters)) AS jaccard,
			       ca.title,
			       ca.username,
			       cb.title,
			       cb.username
			FROM shared s
			JOIN cluster_counts c1 ON s.channel_a = c1.channel_id
			JOIN cluster_counts c2 ON s.channel_b = c2.channel_id
			JOIN channels ca ON ca.id = s.channel_a
			JOIN channels cb ON cb.id = s.channel_b
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
			titleA   pgtype.Text
			userA    pgtype.Text
			titleB   pgtype.Text
			userB    pgtype.Text
		)
		if err := rows.Scan(&channelA, &channelB, &shared, &totalA, &totalB, &jaccard, &titleA, &userA, &titleB, &userB); err != nil {
			return nil, fmt.Errorf("scan channel overlap: %w", err)
		}

		results = append(results, ResearchChannelOverlapEdge{
			ChannelA:         fromUUID(channelA),
			ChannelB:         fromUUID(channelB),
			ChannelATitle:    titleA.String,
			ChannelAUsername: userA.String,
			ChannelBTitle:    titleB.String,
			ChannelBUsername: userB.String,
			Shared:           shared,
			TotalA:           totalA,
			TotalB:           totalB,
			Jaccard:          jaccard,
		})
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate channel overlap: %w", rows.Err())
	}

	return results, nil
}

func (db *DB) GetChannelOverlapSummary(ctx context.Context) (ResearchChannelOverlapSummary, error) {
	row := db.Pool.QueryRow(ctx, `
		WITH channel_clusters AS (
			SELECT ci.cluster_id, rm.channel_id
			FROM cluster_items ci
			JOIN clusters c ON ci.cluster_id = c.id AND c.source = $1
			JOIN items i ON ci.item_id = i.id
			JOIN raw_messages rm ON i.raw_message_id = rm.id
			GROUP BY ci.cluster_id, rm.channel_id
		),
		cluster_counts AS (
			SELECT cluster_id, COUNT(*) AS channel_count
			FROM channel_clusters
			GROUP BY cluster_id
		)
		SELECT
			(SELECT COUNT(*) FROM cluster_counts) AS total_clusters,
			(SELECT COUNT(*) FROM cluster_counts WHERE channel_count > 1) AS shared_clusters,
			(SELECT COUNT(DISTINCT channel_id) FROM channel_clusters) AS total_channels
	`, ClusterSourceResearch)

	var (
		totalClusters  pgtype.Int8
		sharedClusters pgtype.Int8
		totalChannels  pgtype.Int8
	)

	if err := row.Scan(&totalClusters, &sharedClusters, &totalChannels); err != nil {
		return ResearchChannelOverlapSummary{}, fmt.Errorf("get channel overlap summary: %w", err)
	}

	return ResearchChannelOverlapSummary{
		TotalClusters:  int(totalClusters.Int64),
		SharedClusters: int(sharedClusters.Int64),
		TotalChannels:  int(totalChannels.Int64),
	}, nil
}

func (db *DB) GetChannelQualitySummary(ctx context.Context, from, to *time.Time, limit int) ([]ResearchChannelQualitySummary, error) {
	if limit <= 0 {
		limit = 200
	}

	args := []any{}
	qualityWhere := []string{"1=1"}
	statsWhere := []string{"1=1"}
	varianceWhere := []string{"1=1"}

	if from != nil {
		args = append(args, *from)
		argIdx := len(args)
		qualityWhere = append(qualityWhere, fmt.Sprintf("q.period_start >= $%d", argIdx))
		statsWhere = append(statsWhere, fmt.Sprintf("cs.period_start >= $%d", argIdx))
		varianceWhere = append(varianceWhere, fmt.Sprintf(fmtDateFrom, argIdx))
	}

	if to != nil {
		args = append(args, *to)
		argIdx := len(args)
		qualityWhere = append(qualityWhere, fmt.Sprintf("q.period_end <= $%d", argIdx))
		statsWhere = append(statsWhere, fmt.Sprintf("cs.period_end <= $%d", argIdx))
		varianceWhere = append(varianceWhere, fmt.Sprintf(fmtDateTo, argIdx))
	}

	args = append(args, safeIntToInt32(limit))

	query := fmt.Sprintf(`
		WITH ranked AS (
			SELECT q.channel_id,
			       q.period_start,
			       q.period_end,
			       q.inclusion_rate,
			       q.noise_rate,
			       q.avg_importance,
			       q.avg_relevance,
			       c.username,
			       c.title,
			       c.importance_weight,
			       c.auto_weight_enabled,
			       c.weight_override,
			       c.weight_updated_at,
			       ROW_NUMBER() OVER (PARTITION BY q.channel_id ORDER BY q.period_end DESC) AS rn
			FROM channel_quality_history q
			JOIN channels c ON q.channel_id = c.id
			WHERE %s
		),
		stats_window AS (
			SELECT cs.channel_id,
			       SUM(cs.items_digested)::bigint AS items_digested
			FROM channel_stats cs
			WHERE %s
			GROUP BY cs.channel_id
		),
		stats_totals AS (
			SELECT COALESCE(SUM(items_digested), 0)::bigint AS total_digested
			FROM stats_window
		),
		variance_window AS (
			SELECT rm.channel_id,
			       stddev_samp(i.relevance_score) AS relevance_stddev
			FROM items i
			JOIN raw_messages rm ON i.raw_message_id = rm.id
			WHERE i.status IN ('ready', 'digested') AND %s
			GROUP BY rm.channel_id
		)
		SELECT ranked.channel_id,
		       ranked.username,
		       ranked.title,
		       ranked.period_start,
		       ranked.period_end,
		       ranked.inclusion_rate,
		       ranked.noise_rate,
		       ranked.avg_importance,
		       ranked.avg_relevance,
		       COALESCE(stats_window.items_digested, 0)::bigint AS items_digested,
		       CASE
			       WHEN stats_totals.total_digested > 0
			       THEN stats_window.items_digested::double precision / stats_totals.total_digested
			       ELSE 0
		       END AS digest_share,
		       COALESCE(variance_window.relevance_stddev, 0) AS relevance_stddev,
		       COALESCE(ranked.importance_weight, 1) AS importance_weight,
		       ranked.auto_weight_enabled,
		       ranked.weight_override,
		       ranked.weight_updated_at
		FROM ranked
		LEFT JOIN stats_window ON stats_window.channel_id = ranked.channel_id
		CROSS JOIN stats_totals
		LEFT JOIN variance_window ON variance_window.channel_id = ranked.channel_id
		WHERE rn = 1
		ORDER BY noise_rate DESC
		LIMIT $%d
	`, strings.Join(qualityWhere, sqlAndJoin), strings.Join(statsWhere, sqlAndJoin), strings.Join(varianceWhere, sqlAndJoin), len(args))

	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get channel quality summary: %w", err)
	}
	defer rows.Close()

	results := []ResearchChannelQualitySummary{}

	for rows.Next() {
		var (
			channelID     pgtype.UUID
			username      pgtype.Text
			title         pgtype.Text
			start         pgtype.Date
			end           pgtype.Date
			inclusion     pgtype.Float8
			noise         pgtype.Float8
			avgImp        pgtype.Float8
			avgRel        pgtype.Float8
			itemsDigested pgtype.Int8
			digestShare   pgtype.Float8
			relStddev     pgtype.Float8
			impWeight     pgtype.Float8
			autoEnabled   pgtype.Bool
			override      pgtype.Bool
			weightUpdated pgtype.Timestamptz
		)

		if err := rows.Scan(
			&channelID,
			&username,
			&title,
			&start,
			&end,
			&inclusion,
			&noise,
			&avgImp,
			&avgRel,
			&itemsDigested,
			&digestShare,
			&relStddev,
			&impWeight,
			&autoEnabled,
			&override,
			&weightUpdated,
		); err != nil {
			return nil, fmt.Errorf("scan channel quality summary: %w", err)
		}

		entry := ResearchChannelQualitySummary{
			ChannelID:         fromUUID(channelID),
			ChannelTitle:      title.String,
			ChannelUsername:   username.String,
			PeriodStart:       nullableDate(start),
			PeriodEnd:         nullableDate(end),
			InclusionRate:     nullableFloat64(inclusion),
			NoiseRate:         nullableFloat64(noise),
			AvgImportance:     nullableFloat64(avgImp),
			AvgRelevance:      nullableFloat64(avgRel),
			DigestShare:       nullableFloat64(digestShare),
			ItemsDigested:     int(itemsDigested.Int64),
			RelevanceStddev:   nullableFloat64(relStddev),
			ImportanceWeight:  nullableFloat64(impWeight),
			AutoWeightEnabled: autoEnabled.Bool,
			WeightOverride:    override.Bool,
		}

		if weightUpdated.Valid {
			entry.WeightUpdatedAt = weightUpdated.Time
		}

		results = append(results, entry)
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate channel quality summary: %w", rows.Err())
	}

	return results, nil
}

func (db *DB) GetChannelBias(ctx context.Context, channelID string, from, to *time.Time, limit int) ([]ResearchChannelBiasEntry, error) {
	if limit <= 0 {
		limit = 20
	}

	args := []any{toUUID(channelID)}
	where := []string{"rm.channel_id = $1"}

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
		WITH channel_items AS (
			SELECT i.topic, COUNT(*) AS count
			FROM items i
			JOIN raw_messages rm ON i.raw_message_id = rm.id
			WHERE %s AND i.topic IS NOT NULL AND i.topic <> ''
			GROUP BY i.topic
		),
		global_items AS (
			SELECT i.topic, COUNT(*) AS count
			FROM items i
			JOIN raw_messages rm ON i.raw_message_id = rm.id
			WHERE i.topic IS NOT NULL AND i.topic <> '' %s
			GROUP BY i.topic
		),
		totals AS (
			SELECT
				COALESCE((SELECT SUM(count) FROM channel_items), 0) AS channel_total,
				COALESCE((SELECT SUM(count) FROM global_items), 0) AS global_total
		)
		SELECT ci.topic,
		       ci.count,
		       gi.count,
		       (ci.count::float / NULLIF(t.channel_total, 0)) AS channel_share,
		       (gi.count::float / NULLIF(t.global_total, 0)) AS global_share,
		       (ci.count::float / NULLIF(t.channel_total, 0)) / NULLIF((gi.count::float / NULLIF(t.global_total, 0)), 0) AS index_ratio
		FROM channel_items ci
		JOIN global_items gi ON gi.topic = ci.topic
		CROSS JOIN totals t
		ORDER BY index_ratio DESC
		LIMIT $%d
	`, strings.Join(where, sqlAndJoin), buildGlobalTopicFilter(from, to), len(args))

	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get channel bias: %w", err)
	}
	defer rows.Close()

	results := []ResearchChannelBiasEntry{}

	for rows.Next() {
		var (
			topic        pgtype.Text
			channelCnt   int
			globalCnt    int
			channelShare pgtype.Float8
			globalShare  pgtype.Float8
			indexRatio   pgtype.Float8
		)
		if err := rows.Scan(&topic, &channelCnt, &globalCnt, &channelShare, &globalShare, &indexRatio); err != nil {
			return nil, fmt.Errorf("scan channel bias: %w", err)
		}

		results = append(results, ResearchChannelBiasEntry{
			Topic:        topic.String,
			ChannelCount: channelCnt,
			GlobalCount:  globalCnt,
			ChannelShare: nullableFloat64(channelShare),
			GlobalShare:  nullableFloat64(globalShare),
			IndexRatio:   nullableFloat64(indexRatio),
		})
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate channel bias: %w", rows.Err())
	}

	return results, nil
}

func (db *DB) GetChannelAgendaSimilarity(ctx context.Context, from, to *time.Time, limit int) ([]ResearchAgendaSimilarityEdge, error) {
	if limit <= 0 {
		limit = 200
	}

	args, where := buildAgendaSimilarityFilters(from, to)
	args = append(args, safeIntToInt32(maxOverlapChannels), safeIntToInt32(limit))

	query := fmt.Sprintf(`
		WITH channel_topics AS (
			SELECT rm.channel_id, i.topic
			FROM items i
			JOIN raw_messages rm ON i.raw_message_id = rm.id
			WHERE %s AND i.topic IS NOT NULL AND i.topic <> ''
			GROUP BY rm.channel_id, i.topic
		),
		topic_counts AS (
			SELECT channel_id, COUNT(*) AS topic_count
			FROM channel_topics
			GROUP BY channel_id
		),
		top_channels AS (
			SELECT channel_id
			FROM topic_counts
			ORDER BY topic_count DESC
			LIMIT $%d
		),
		shared AS (
			SELECT ct1.channel_id AS channel_a,
			       ct2.channel_id AS channel_b,
			       COUNT(*) AS shared_topics
			FROM channel_topics ct1
			JOIN channel_topics ct2
			  ON ct1.topic = ct2.topic AND ct1.channel_id < ct2.channel_id
			WHERE ct1.channel_id IN (SELECT channel_id FROM top_channels)
			  AND ct2.channel_id IN (SELECT channel_id FROM top_channels)
			GROUP BY ct1.channel_id, ct2.channel_id
		)
		SELECT s.channel_a,
		       s.channel_b,
		       s.shared_topics,
		       tc1.topic_count AS total_a,
		       tc2.topic_count AS total_b,
		       (s.shared_topics::double precision / (tc1.topic_count + tc2.topic_count - s.shared_topics)) AS jaccard,
		       ca.title,
		       ca.username,
		       cb.title,
		       cb.username
		FROM shared s
		JOIN topic_counts tc1 ON s.channel_a = tc1.channel_id
		JOIN topic_counts tc2 ON s.channel_b = tc2.channel_id
		JOIN channels ca ON ca.id = s.channel_a
		JOIN channels cb ON cb.id = s.channel_b
		ORDER BY jaccard DESC
		LIMIT $%d
	`, strings.Join(where, sqlAndJoin), len(args)-1, len(args))

	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get channel agenda similarity: %w", err)
	}
	defer rows.Close()

	results := []ResearchAgendaSimilarityEdge{}

	for rows.Next() {
		var (
			channelA pgtype.UUID
			channelB pgtype.UUID
			titleA   pgtype.Text
			userA    pgtype.Text
			titleB   pgtype.Text
			userB    pgtype.Text
			shared   int
			totalA   int
			totalB   int
			jaccard  float64
		)

		if err := rows.Scan(&channelA, &channelB, &shared, &totalA, &totalB, &jaccard, &titleA, &userA, &titleB, &userB); err != nil {
			return nil, fmt.Errorf("scan agenda similarity: %w", err)
		}

		results = append(results, ResearchAgendaSimilarityEdge{
			ChannelA:         fromUUID(channelA),
			ChannelATitle:    titleA.String,
			ChannelAUser:     userA.String,
			ChannelB:         fromUUID(channelB),
			ChannelBTitle:    titleB.String,
			ChannelBUser:     userB.String,
			SharedTopics:     shared,
			TotalTopicsA:     totalA,
			TotalTopicsB:     totalB,
			AgendaSimilarity: jaccard,
		})
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate agenda similarity: %w", rows.Err())
	}

	return results, nil
}

func buildAgendaSimilarityFilters(from, to *time.Time) ([]any, []string) {
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

	return args, where
}

func buildGlobalTopicFilter(from, to *time.Time) string {
	clauses := []string{}
	argIndex := 2

	if from != nil {
		clauses = append(clauses, fmt.Sprintf("rm.tg_date >= $%d", argIndex))
		argIndex++
	}

	if to != nil {
		clauses = append(clauses, fmt.Sprintf("rm.tg_date <= $%d", argIndex))
	}

	if len(clauses) == 0 {
		return ""
	}

	return " AND " + strings.Join(clauses, " AND ")
}

func (db *DB) GetLanguageCoverage(ctx context.Context, from, to *time.Time, limit int) ([]ResearchLanguageCoverageEntry, error) {
	if limit <= 0 {
		limit = 100
	}

	args := []any{}
	where := []string{"cl.language IS NOT NULL", "cl.language <> ''"}

	if from != nil {
		args = append(args, *from)
		where = append(where, fmt.Sprintf("ms.first_seen_at >= $%d", len(args)))
	}

	if to != nil {
		args = append(args, *to)
		where = append(where, fmt.Sprintf("ms.first_seen_at <= $%d", len(args)))
	}

	args = append(args, ClusterSourceResearch)
	sourceIdx := len(args)
	args = append(args, safeIntToInt32(limit))
	limitIdx := len(args)

	query := fmt.Sprintf(`
		WITH cluster_lang AS (
			SELECT cfa.cluster_id, i.language
			FROM cluster_first_appearance cfa
			JOIN items i ON cfa.first_item_id = i.id
			WHERE i.language IS NOT NULL AND i.language <> ''
		),
		links AS (
			SELECT l.cluster_id,
			       l.linked_cluster_id,
			       cl.language AS from_lang,
			       l.language AS to_lang,
			       c.topic AS topic,
			       abs(EXTRACT(epoch FROM (mt.first_seen_at - ms.first_seen_at))) / 3600 AS lag_hours
			FROM cluster_language_links l
			JOIN cluster_lang cl ON l.cluster_id = cl.cluster_id
			JOIN clusters c ON l.cluster_id = c.id AND c.source = $%d
			JOIN mv_cluster_stats ms ON l.cluster_id = ms.cluster_id
			JOIN mv_cluster_stats mt ON l.linked_cluster_id = mt.cluster_id
			WHERE %s
		)
		SELECT topic, from_lang, to_lang, COUNT(DISTINCT cluster_id), AVG(lag_hours)
		FROM links
		GROUP BY topic, from_lang, to_lang
		ORDER BY COUNT(DISTINCT cluster_id) DESC
		LIMIT $%d
	`, sourceIdx, strings.Join(where, sqlAndJoin), limitIdx)

	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get language coverage: %w", err)
	}
	defer rows.Close()

	results := []ResearchLanguageCoverageEntry{}

	for rows.Next() {
		var (
			topic    pgtype.Text
			fromLang pgtype.Text
			toLang   pgtype.Text
			count    int
			avgLag   pgtype.Float8
		)

		if err := rows.Scan(&topic, &fromLang, &toLang, &count, &avgLag); err != nil {
			return nil, fmt.Errorf("scan language coverage: %w", err)
		}

		entry := ResearchLanguageCoverageEntry{
			Topic:        topic.String,
			FromLang:     fromLang.String,
			ToLang:       toLang.String,
			ClusterCount: count,
		}
		if avgLag.Valid {
			entry.AvgLagHours = avgLag.Float64
		}

		results = append(results, entry)
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate language coverage: %w", rows.Err())
	}

	return results, nil
}

func (db *DB) GetTopicDrift(ctx context.Context, from, to *time.Time, limit int) ([]ResearchTopicDriftEntry, error) {
	limit = defaultLimit(limit, defaultTopicDriftLimit)

	args, where := buildTopicDriftFilters(from, to)
	args = append(args, safeIntToInt32(limit))

	query := fmt.Sprintf(`
		WITH topic_summary AS (
			SELECT cluster_id,
			       (ARRAY_AGG(topic ORDER BY window_start ASC))[1] AS first_topic,
			       (ARRAY_AGG(topic ORDER BY window_start DESC))[1] AS last_topic,
			       COUNT(DISTINCT topic) AS distinct_topics,
			       MIN(window_start) AS first_seen,
			       MAX(window_end) AS last_seen
			FROM cluster_topic_history
			WHERE %s
			GROUP BY cluster_id
			HAVING COUNT(DISTINCT topic) > 1
		),
		ranked_items AS (
			SELECT ci.cluster_id,
			       i.id AS item_id,
			       rm.tg_date,
			       ROW_NUMBER() OVER (PARTITION BY ci.cluster_id ORDER BY rm.tg_date ASC) AS rn_first,
			       ROW_NUMBER() OVER (PARTITION BY ci.cluster_id ORDER BY rm.tg_date DESC) AS rn_last
			FROM cluster_items ci
			JOIN clusters c ON ci.cluster_id = c.id AND c.source = 'research'
			JOIN items i ON ci.item_id = i.id
			JOIN raw_messages rm ON i.raw_message_id = rm.id
		),
		first_last AS (
			SELECT r1.cluster_id,
			       r1.item_id AS first_item_id,
			       r2.item_id AS last_item_id
			FROM ranked_items r1
			JOIN ranked_items r2 ON r1.cluster_id = r2.cluster_id
			WHERE r1.rn_first = 1 AND r2.rn_last = 1
		),
		embedding_similarity AS (
			SELECT fl.cluster_id,
			       CASE
			         WHEN e1.embedding IS NOT NULL AND e2.embedding IS NOT NULL THEN 1 - (e1.embedding <=> e2.embedding)
			         ELSE NULL
			       END AS similarity
			FROM first_last fl
			LEFT JOIN embeddings e1 ON e1.item_id = fl.first_item_id
			LEFT JOIN embeddings e2 ON e2.item_id = fl.last_item_id
		)
		SELECT ts.cluster_id,
		       ts.first_topic,
		       ts.last_topic,
		       ts.distinct_topics,
		       ts.first_seen,
		       ts.last_seen,
		       es.similarity
		FROM topic_summary ts
		LEFT JOIN embedding_similarity es ON es.cluster_id = ts.cluster_id
		ORDER BY ts.distinct_topics DESC, ts.last_seen DESC
		LIMIT $%d
	`, strings.Join(where, sqlAndJoin), len(args))

	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get topic drift: %w", err)
	}
	defer rows.Close()

	results := []ResearchTopicDriftEntry{}

	for rows.Next() {
		entry, skip, err := scanTopicDriftRow(rows)
		if err != nil {
			return nil, err
		}

		if !skip {
			results = append(results, entry)
		}
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate topic drift: %w", rows.Err())
	}

	return results, nil
}

func scanTopicDriftRow(rows pgx.Rows) (ResearchTopicDriftEntry, bool, error) {
	var (
		clusterID pgtype.UUID
		first     pgtype.Text
		last      pgtype.Text
		count     int
		firstSeen pgtype.Timestamptz
		lastSeen  pgtype.Timestamptz
		embedSim  pgtype.Float8
	)

	if err := rows.Scan(&clusterID, &first, &last, &count, &firstSeen, &lastSeen, &embedSim); err != nil {
		return ResearchTopicDriftEntry{}, false, fmt.Errorf("scan topic drift: %w", err)
	}

	if shouldSkipDriftEntry(first.String, last.String, embedSim) {
		return ResearchTopicDriftEntry{}, true, nil
	}

	entry := ResearchTopicDriftEntry{
		ClusterID:      fromUUID(clusterID),
		FirstTopic:     first.String,
		LastTopic:      last.String,
		DistinctTopics: count,
	}

	if firstSeen.Valid {
		entry.FirstSeenAt = firstSeen.Time
	}

	if lastSeen.Valid {
		entry.LastSeenAt = lastSeen.Time
	}

	return entry, false, nil
}

func shouldSkipDriftEntry(first, last string, embedSim pgtype.Float8) bool {
	similarity := topicSimilarity(first, last)
	if similarity >= topicDriftMinJaccard {
		return true
	}

	return embedSim.Valid && embedSim.Float64 >= topicDriftMinEmbedding
}

func buildTopicDriftFilters(from, to *time.Time) ([]any, []string) {
	args := []any{}
	where := []string{"topic IS NOT NULL", "topic <> ''"}

	if from != nil {
		args = append(args, *from)
		where = append(where, fmt.Sprintf("window_start >= $%d", len(args)))
	}

	if to != nil {
		args = append(args, *to)
		where = append(where, fmt.Sprintf("window_end <= $%d", len(args)))
	}

	return args, where
}

func defaultLimit(limit, defaultVal int) int {
	if limit <= 0 {
		return defaultVal
	}

	return limit
}

func topicSimilarity(a, b string) float64 {
	tokensA := tokenizeTopic(a)

	tokensB := tokenizeTopic(b)
	if len(tokensA) == 0 || len(tokensB) == 0 {
		return 0
	}

	setA := make(map[string]struct{}, len(tokensA))
	for _, t := range tokensA {
		setA[t] = struct{}{}
	}

	setB := make(map[string]struct{}, len(tokensB))
	for _, t := range tokensB {
		setB[t] = struct{}{}
	}

	var intersection int

	for t := range setA {
		if _, ok := setB[t]; ok {
			intersection++
		}
	}

	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0
	}

	return float64(intersection) / float64(union)
}

func tokenizeTopic(value string) []string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return nil
	}

	var (
		tokens  []string
		current strings.Builder
	)

	for _, r := range normalized {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
			continue
		}

		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}

func (db *DB) GetClaimsSummary(ctx context.Context) (ResearchClaimsSummary, error) {
	row := db.Pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM claims) AS claims_count,
			(SELECT COUNT(*) FROM evidence_claims) AS evidence_claims_count,
			(SELECT COUNT(DISTINCT item_id) FROM item_evidence) AS evidence_items_count,
			(SELECT COUNT(DISTINCT ci.item_id)
			 FROM cluster_items ci
			 JOIN clusters c ON c.id = ci.cluster_id
			 WHERE c.source = $1) AS cluster_items_count,
			(SELECT COUNT(DISTINCT ie.item_id)
			 FROM item_evidence ie
			 JOIN cluster_items ci ON ci.item_id = ie.item_id
			 JOIN clusters c ON c.id = ci.cluster_id
			 WHERE c.source = $1) AS clustered_with_evidence
	`, ClusterSourceResearch)

	var (
		claimsCount          pgtype.Int8
		evidenceClaimsCount  pgtype.Int8
		evidenceItemsCount   pgtype.Int8
		clusterItemsCount    pgtype.Int8
		clusteredWithEvCount pgtype.Int8
	)

	if err := row.Scan(&claimsCount, &evidenceClaimsCount, &evidenceItemsCount, &clusterItemsCount, &clusteredWithEvCount); err != nil {
		return ResearchClaimsSummary{}, fmt.Errorf("get claims summary: %w", err)
	}

	return ResearchClaimsSummary{
		ClaimsCount:          int(claimsCount.Int64),
		EvidenceClaimsCount:  int(evidenceClaimsCount.Int64),
		EvidenceItemsCount:   int(evidenceItemsCount.Int64),
		ClusterItemsCount:    int(clusterItemsCount.Int64),
		ClusteredWithEvCount: int(clusteredWithEvCount.Int64),
	}, nil
}

// GetReadyItemsForResearch returns ready items with embeddings for research clustering.
func (db *DB) GetReadyItemsForResearch(ctx context.Context, start, end time.Time, limit int) ([]Item, error) {
	if limit <= 0 {
		limit = 2000
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT i.id, i.raw_message_id, i.relevance_score, i.importance_score, i.topic, i.summary, i.language,
		       i.status, i.first_seen_at, rm.tg_date, c.username as source_channel,
		       c.title as source_channel_title, c.tg_peer_id as source_channel_id, rm.tg_message_id as source_msg_id,
		       e.embedding
		FROM items i
		JOIN raw_messages rm ON i.raw_message_id = rm.id
		JOIN channels c ON rm.channel_id = c.id
		JOIN embeddings e ON i.id = e.item_id
		WHERE rm.tg_date >= $1 AND rm.tg_date < $2
		  AND i.status = 'ready'
		ORDER BY rm.tg_date DESC
		LIMIT $3
	`, toTimestamptz(start), toTimestamptz(end), safeIntToInt32(limit))
	if err != nil {
		return nil, fmt.Errorf("get research items: %w", err)
	}
	defer rows.Close()

	var items []Item

	for rows.Next() {
		var (
			id                 pgtype.UUID
			rawMessageID       pgtype.UUID
			relevanceScore     float32
			importanceScore    float32
			topic              pgtype.Text
			summary            pgtype.Text
			language           pgtype.Text
			status             string
			firstSeenAt        pgtype.Timestamptz
			tgDate             pgtype.Timestamptz
			sourceChannel      pgtype.Text
			sourceChannelTitle pgtype.Text
			sourceChannelID    int64
			sourceMsgID        int64
			embedding          pgvector.Vector
		)

		if err := rows.Scan(
			&id,
			&rawMessageID,
			&relevanceScore,
			&importanceScore,
			&topic,
			&summary,
			&language,
			&status,
			&firstSeenAt,
			&tgDate,
			&sourceChannel,
			&sourceChannelTitle,
			&sourceChannelID,
			&sourceMsgID,
			&embedding,
		); err != nil {
			return nil, fmt.Errorf("scan research items: %w", err)
		}

		items = append(items, Item{
			ID:                 fromUUID(id),
			RawMessageID:       fromUUID(rawMessageID),
			RelevanceScore:     relevanceScore,
			ImportanceScore:    importanceScore,
			Topic:              topic.String,
			Summary:            summary.String,
			Language:           language.String,
			Status:             status,
			FirstSeenAt:        firstSeenAt.Time,
			TGDate:             tgDate.Time,
			SourceChannel:      sourceChannel.String,
			SourceChannelTitle: sourceChannelTitle.String,
			SourceChannelID:    sourceChannelID,
			SourceMsgID:        sourceMsgID,
			Embedding:          embedding.Slice(),
		})
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf(errIterateResearchItems, rows.Err())
	}

	return items, nil
}

// ResearchTopicTimelinePoint represents topic timeline buckets.
type ResearchTopicTimelinePoint struct {
	BucketDate    time.Time
	Topic         string
	ItemCount     int
	AvgImportance float64
	AvgRelevance  float64
}

// ResearchTopicVolatilityEntry represents topic churn metrics per bucket.
type ResearchTopicVolatilityEntry struct {
	BucketDate     time.Time
	DistinctTopics int
	NewTopics      int
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
	bucket = normalizeTimelineBucket(bucket)
	limit = defaultLimit(limit, maxSearchLimit)

	query, args := buildTopicTimelineQuery(bucket, from, to, limit)

	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get topic timeline: %w", err)
	}
	defer rows.Close()

	return scanTopicTimelineRows(rows)
}

func buildTopicTimelineQuery(bucket string, from, to *time.Time, limit int) (string, []any) {
	if bucket == bucketWeek {
		return buildWeekTimelineQuery(from, to, limit)
	}

	return buildDynamicTimelineQuery(bucket, from, to, limit)
}

func buildWeekTimelineQuery(from, to *time.Time, limit int) (string, []any) {
	var args []any

	where := []string{"1=1"}

	if from != nil {
		args = append(args, *from)
		where = append(where, fmt.Sprintf("bucket_date >= $%d", len(args)))
	}

	if to != nil {
		args = append(args, *to)
		where = append(where, fmt.Sprintf("bucket_date <= $%d", len(args)))
	}

	args = append(args, safeIntToInt32(limit))

	query := fmt.Sprintf(`
		WITH ranked AS (
			SELECT bucket_date,
			       topic,
			       item_count,
			       avg_importance,
			       avg_relevance,
			       ROW_NUMBER() OVER (PARTITION BY bucket_date ORDER BY item_count DESC) AS rn
			FROM mv_topic_timeline
			WHERE %s
		)
		SELECT bucket_date, topic, item_count, avg_importance, avg_relevance
		FROM ranked
		WHERE rn <= $%d
		ORDER BY bucket_date DESC, item_count DESC
	`, strings.Join(where, sqlAndJoin), len(args))

	return query, args
}

func buildDynamicTimelineQuery(bucket string, from, to *time.Time, limit int) (string, []any) {
	args := make([]any, 0, timelineArgsCapacity)
	args = append(args, bucket)

	where, args := buildTimelineFilters(from, to, args)
	args = append(args, safeIntToInt32(limit))

	query := fmt.Sprintf(`
		WITH ranked AS (
			SELECT date_trunc($1, rm.tg_date) AS bucket_date,
			       i.topic,
			       COUNT(*) AS item_count,
			       AVG(i.importance_score) AS avg_importance,
			       AVG(i.relevance_score) AS avg_relevance,
			       ROW_NUMBER() OVER (PARTITION BY date_trunc($1, rm.tg_date) ORDER BY COUNT(*) DESC) AS rn
			FROM items i
			JOIN raw_messages rm ON i.raw_message_id = rm.id
			WHERE %s
			GROUP BY bucket_date, i.topic
		)
		SELECT bucket_date, topic, item_count, avg_importance, avg_relevance
		FROM ranked
		WHERE rn <= $%d
		ORDER BY bucket_date DESC, item_count DESC
	`, strings.Join(where, sqlAndJoin), len(args))

	return query, args
}

func scanTopicTimelineRows(rows pgx.Rows) ([]ResearchTopicTimelinePoint, error) {
	points := []ResearchTopicTimelinePoint{}

	for rows.Next() {
		entry, err := scanTopicTimelineRow(rows)
		if err != nil {
			return nil, err
		}

		points = append(points, entry)
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate topic timeline: %w", rows.Err())
	}

	return points, nil
}

func scanTopicTimelineRow(rows pgx.Rows) (ResearchTopicTimelinePoint, error) {
	var (
		bucketDate pgtype.Timestamptz
		topic      pgtype.Text
		count      int
		avgImp     pgtype.Float8
		avgRel     pgtype.Float8
	)

	if err := rows.Scan(&bucketDate, &topic, &count, &avgImp, &avgRel); err != nil {
		return ResearchTopicTimelinePoint{}, fmt.Errorf("scan topic timeline: %w", err)
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

	return entry, nil
}

func bucketInterval(bucket string) string {
	switch bucket {
	case bucketDay:
		return "1 day"
	case bucketMonth:
		return "1 month"
	default:
		return "1 week"
	}
}

// GetTopicVolatility returns topic churn metrics per bucket.
func (db *DB) GetTopicVolatility(ctx context.Context, bucket string, from, to *time.Time, limit int) ([]ResearchTopicVolatilityEntry, error) {
	bucket = normalizeTimelineBucket(bucket)

	if limit <= 0 {
		limit = 52
	}

	args := make([]any, 0, timelineArgsCapacity)
	args = append(args, bucket)
	where, args := buildTimelineFilters(from, to, args)
	where = append(where, "i.topic IS NOT NULL", "i.topic <> ''")

	interval := bucketInterval(bucket)

	args = append(args, safeIntToInt32(limit))

	query := fmt.Sprintf(`
		WITH bucket_topics AS (
			SELECT date_trunc($1, rm.tg_date) AS bucket_date,
			       i.topic
			FROM items i
			JOIN raw_messages rm ON i.raw_message_id = rm.id
			WHERE %s
			GROUP BY bucket_date, i.topic
		),
		summary AS (
			SELECT bucket_date,
			       COUNT(*) AS topic_count
			FROM bucket_topics
			GROUP BY bucket_date
		),
		new_topics AS (
			SELECT cur.bucket_date,
			       COUNT(*) AS new_topics
			FROM bucket_topics cur
			LEFT JOIN bucket_topics prev
			  ON prev.topic = cur.topic
			 AND prev.bucket_date = cur.bucket_date - INTERVAL '%s'
			WHERE prev.topic IS NULL
			GROUP BY cur.bucket_date
		)
		SELECT s.bucket_date,
		       s.topic_count,
		       COALESCE(n.new_topics, 0) AS new_topics
		FROM summary s
		LEFT JOIN new_topics n ON n.bucket_date = s.bucket_date
		ORDER BY s.bucket_date DESC
		LIMIT $%d
	`, strings.Join(where, sqlAndJoin), interval, len(args))

	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get topic volatility: %w", err)
	}
	defer rows.Close()

	entries := []ResearchTopicVolatilityEntry{}

	for rows.Next() {
		var (
			bucketDate pgtype.Timestamptz
			count      int
			newTopics  int
		)

		if err := rows.Scan(&bucketDate, &count, &newTopics); err != nil {
			return nil, fmt.Errorf("scan topic volatility: %w", err)
		}

		entry := ResearchTopicVolatilityEntry{
			DistinctTopics: count,
			NewTopics:      newTopics,
		}
		if bucketDate.Valid {
			entry.BucketDate = bucketDate.Time
		}

		entries = append(entries, entry)
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate topic volatility: %w", rows.Err())
	}

	return entries, nil
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

// GetChannelWeightHistory returns weight history entries for a channel.
func (db *DB) GetChannelWeightHistory(ctx context.Context, channelID string, from, to *time.Time, limit int) ([]ResearchChannelWeightEntry, error) {
	if limit <= 0 {
		limit = 50
	}

	args := []any{toUUID(channelID)}
	where := []string{"channel_id = $1"}

	if from != nil {
		args = append(args, *from)
		where = append(where, fmt.Sprintf("updated_at >= $%d", len(args)))
	}

	if to != nil {
		args = append(args, *to)
		where = append(where, fmt.Sprintf("updated_at <= $%d", len(args)))
	}

	args = append(args, safeIntToInt32(limit))

	rows, err := db.Pool.Query(ctx, fmt.Sprintf(`
		SELECT importance_weight,
		       auto_weight_enabled,
		       weight_override,
		       reason,
		       updated_by,
		       updated_at
		FROM channel_weight_history
		WHERE %s
		ORDER BY updated_at DESC
		LIMIT $%d
	`, strings.Join(where, sqlAndJoin), len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("get channel weight history: %w", err)
	}
	defer rows.Close()

	entries := []ResearchChannelWeightEntry{}

	for rows.Next() {
		var (
			weight    pgtype.Float4
			auto      pgtype.Bool
			override  pgtype.Bool
			reason    pgtype.Text
			updatedBy pgtype.Int8
			updatedAt pgtype.Timestamptz
		)

		if err := rows.Scan(&weight, &auto, &override, &reason, &updatedBy, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan channel weight history: %w", err)
		}

		entry := ResearchChannelWeightEntry{
			ImportanceWeight:  float64(weight.Float32),
			AutoWeightEnabled: auto.Bool,
			WeightOverride:    override.Bool,
			Reason:            reason.String,
		}

		if updatedBy.Valid {
			entry.UpdatedBy = updatedBy.Int64
		}

		if updatedAt.Valid {
			entry.UpdatedAt = updatedAt.Time
		}

		entries = append(entries, entry)
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate channel weight history: %w", rows.Err())
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
	ChannelID       string
	OriginCount     int
	TotalCount      int
	OriginRate      float64
	AmplifierRate   float64
	OriginTopics    []ResearchOriginTopicEntry
	AmplifierTopics []ResearchOriginTopicEntry
}

// ResearchOriginTopicEntry represents top topics for origin/amplifier stats.
type ResearchOriginTopicEntry struct {
	Topic string
	Count int
}

// GetOriginStats returns origin vs amplifier stats for a channel.
func (db *DB) GetOriginStats(ctx context.Context, channelID string, from, to *time.Time) (*ResearchOriginStats, error) {
	channelUUID := toUUID(channelID)
	args := []any{channelUUID}
	where := []string{"cfa.channel_id = $1"}

	if from != nil {
		args = append(args, *from)
		where = append(where, fmt.Sprintf(fmtCfaDateFrom, len(args)))
	}

	if to != nil {
		args = append(args, *to)
		where = append(where, fmt.Sprintf(fmtCfaDateTo, len(args)))
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
		JOIN clusters c ON ci.cluster_id = c.id AND c.source = '%s'
		JOIN items i ON ci.item_id = i.id
		JOIN raw_messages rm ON i.raw_message_id = rm.id
		JOIN channels ch ON rm.channel_id = ch.id
		WHERE %s
	`, ClusterSourceResearch, strings.Join(where, sqlAndJoin)), args...)

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

	originTopics, err := db.getOriginTopicBreakdown(ctx, channelUUID, from, to, true)
	if err != nil {
		return nil, err
	}

	amplifierTopics, err := db.getOriginTopicBreakdown(ctx, channelUUID, from, to, false)
	if err != nil {
		return nil, err
	}

	stats.OriginTopics = originTopics
	stats.AmplifierTopics = amplifierTopics

	return stats, nil
}

func (db *DB) getOriginTopicBreakdown(ctx context.Context, channelUUID pgtype.UUID, from, to *time.Time, origin bool) ([]ResearchOriginTopicEntry, error) {
	args, where := buildOriginFilters(channelUUID, from, to, origin)
	args = append(args, safeIntToInt32(originTopicLimit))

	query := buildOriginTopicQuery(where, len(args), origin)

	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, formatOriginError("get", origin, err)
	}
	defer rows.Close()

	return scanOriginTopicRows(rows)
}

func buildOriginFilters(channelUUID pgtype.UUID, from, to *time.Time, origin bool) ([]any, []string) {
	args := []any{channelUUID}
	where := []string{}

	if origin {
		where = append(where, "cfa.channel_id = $1")
		args, where = appendOriginTimeFilters(args, where, from, to, fmtCfaDateFrom, fmtCfaDateTo)
	} else {
		where = append(where, "ch.id = $1")
		args, where = appendOriginTimeFilters(args, where, from, to, fmtDateFrom, fmtDateTo)
	}

	return args, where
}

func appendOriginTimeFilters(args []any, where []string, from, to *time.Time, fromFmt, toFmt string) ([]any, []string) {
	if from != nil {
		args = append(args, *from)
		where = append(where, fmt.Sprintf(fromFmt, len(args)))
	}

	if to != nil {
		args = append(args, *to)
		where = append(where, fmt.Sprintf(toFmt, len(args)))
	}

	return args, where
}

func buildOriginTopicQuery(where []string, limitIdx int, origin bool) string {
	if origin {
		return fmt.Sprintf(`
			SELECT c.topic, COUNT(*) AS cnt
			FROM cluster_first_appearance cfa
			JOIN clusters c ON c.id = cfa.cluster_id
			WHERE %s AND c.topic IS NOT NULL AND c.topic <> ''
			GROUP BY c.topic
			ORDER BY cnt DESC
			LIMIT $%d
		`, strings.Join(where, sqlAndJoin), limitIdx)
	}

	return fmt.Sprintf(`
		WITH channel_clusters AS (
			SELECT DISTINCT ci.cluster_id
			FROM cluster_items ci
			JOIN clusters c ON ci.cluster_id = c.id AND c.source = 'research'
			JOIN items i ON ci.item_id = i.id
			JOIN raw_messages rm ON i.raw_message_id = rm.id
			JOIN channels ch ON rm.channel_id = ch.id
			WHERE %s
		),
		amplifier_clusters AS (
			SELECT cc.cluster_id
			FROM channel_clusters cc
			LEFT JOIN cluster_first_appearance cfa ON cfa.cluster_id = cc.cluster_id
			WHERE cfa.channel_id IS DISTINCT FROM $1
		)
		SELECT c.topic, COUNT(*) AS cnt
		FROM amplifier_clusters ac
		JOIN clusters c ON c.id = ac.cluster_id
		WHERE c.topic IS NOT NULL AND c.topic <> ''
		GROUP BY c.topic
		ORDER BY cnt DESC
		LIMIT $%d
	`, strings.Join(where, sqlAndJoin), limitIdx)
}

func formatOriginError(action string, origin bool, err error) error {
	if origin {
		return fmt.Errorf("%s origin topics: %w", action, err)
	}

	return fmt.Errorf("%s amplifier topics: %w", action, err)
}

func scanOriginTopicRows(rows pgx.Rows) ([]ResearchOriginTopicEntry, error) {
	entries := []ResearchOriginTopicEntry{}

	for rows.Next() {
		var (
			topic pgtype.Text
			count int
		)

		if err := rows.Scan(&topic, &count); err != nil {
			return nil, fmt.Errorf("scan origin topics: %w", err)
		}

		entries = append(entries, ResearchOriginTopicEntry{
			Topic: topic.String,
			Count: count,
		})
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate origin topics: %w", rows.Err())
	}

	return entries, nil
}

// ResearchWeeklyDiff represents weekly topic diff summary.
type ResearchWeeklyDiff struct {
	Topic string
	Delta int
}

// ResearchWeeklyChannelDiff represents weekly channel diff summary.
type ResearchWeeklyChannelDiff struct {
	ChannelID       string
	ChannelTitle    string
	Delta           int
	ImportanceDelta float64
	RelevanceDelta  float64
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
			SELECT ch.id AS channel_id,
			       ch.title AS channel_title,
			       COUNT(*) AS cnt,
			       AVG(i.importance_score) AS avg_importance,
			       AVG(i.relevance_score) AS avg_relevance
			FROM items i
			JOIN raw_messages rm ON i.raw_message_id = rm.id
			JOIN channels ch ON rm.channel_id = ch.id
			WHERE rm.tg_date >= $1 AND rm.tg_date < $2
			GROUP BY ch.id, ch.title
		),
		prev AS (
			SELECT ch.id AS channel_id,
			       ch.title AS channel_title,
			       COUNT(*) AS cnt,
			       AVG(i.importance_score) AS avg_importance,
			       AVG(i.relevance_score) AS avg_relevance
			FROM items i
			JOIN raw_messages rm ON i.raw_message_id = rm.id
			JOIN channels ch ON rm.channel_id = ch.id
			WHERE rm.tg_date >= $3 AND rm.tg_date < $4
			GROUP BY ch.id, ch.title
		)
		SELECT COALESCE(c.channel_id, p.channel_id) AS channel_id,
		       COALESCE(c.channel_title, p.channel_title) AS channel_title,
		       COALESCE(c.cnt, 0) - COALESCE(p.cnt, 0) AS delta,
		       COALESCE(c.avg_importance, 0) - COALESCE(p.avg_importance, 0) AS importance_delta,
		       COALESCE(c.avg_relevance, 0) - COALESCE(p.avg_relevance, 0) AS relevance_delta
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
			impDelta     pgtype.Float8
			relDelta     pgtype.Float8
		)
		if err := rows.Scan(&channelID, &channelTitle, &delta, &impDelta, &relDelta); err != nil {
			return nil, fmt.Errorf("scan weekly channel diff: %w", err)
		}

		results = append(results, ResearchWeeklyChannelDiff{
			ChannelID:       fromUUID(channelID),
			ChannelTitle:    channelTitle.String,
			Delta:           delta,
			ImportanceDelta: nullableFloat64(impDelta),
			RelevanceDelta:  nullableFloat64(relDelta),
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

// ResearchRetentionCounts tracks cleanup counts for retention.
type ResearchRetentionCounts struct {
	ItemsDeleted        int64
	EvidenceDeleted     int64
	TranslationsDeleted int64
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

// InsertResearchAuditLog stores a lightweight audit log entry.
func (db *DB) InsertResearchAuditLog(ctx context.Context, userID int64, route string, status int, ip, queryHash string) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO research_audit_log (user_id, route, status_code, ip_address, query_hash)
		VALUES ($1, $2, $3, $4, $5)
	`, userID, route, status, ip, queryHash)
	if err != nil {
		return fmt.Errorf("insert research audit log: %w", err)
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

// DeleteOldItems removes items older than the retention cutoff.
func (db *DB) DeleteOldItems(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := db.Pool.Exec(ctx, `
		DELETE FROM items i
		USING raw_messages rm
		WHERE i.raw_message_id = rm.id
		  AND rm.tg_date < $1
	`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete old items: %w", err)
	}

	return tag.RowsAffected(), nil
}

// CleanupResearchRetention removes expired research data.
func (db *DB) CleanupResearchRetention(ctx context.Context) (ResearchRetentionCounts, error) {
	var counts ResearchRetentionCounts

	cutoff := time.Now().AddDate(0, -retentionItemsMonths, 0)

	itemsDeleted, err := db.DeleteOldItems(ctx, cutoff)
	if err != nil {
		return counts, err
	}

	counts.ItemsDeleted = itemsDeleted

	evidenceDeleted, err := db.DeleteExpiredEvidenceSources(ctx)
	if err != nil {
		return counts, err
	}

	counts.EvidenceDeleted = evidenceDeleted

	translationsDeleted, err := db.CleanupExpiredTranslations(ctx)
	if err != nil {
		return counts, err
	}

	counts.TranslationsDeleted = translationsDeleted

	return counts, nil
}

// RefreshResearchMaterializedViews refreshes research materialized views and derived caches.
func (db *DB) RefreshResearchMaterializedViews(ctx context.Context) error {
	views := []string{
		"mv_topic_timeline",
		"mv_channel_overlap",
		"mv_cluster_stats",
	}

	if err := db.rebuildResearchDerivedTables(ctx); err != nil {
		// Log the error but continue to refresh views if possible,
		// as views don't depend on the derived tables.
		db.Logger.Error().Err(err).Msg("research derived tables rebuild failed")
	}

	for _, view := range views {
		db.Logger.Info().Str(logFieldView, view).Msg("refreshing materialized view")

		if _, err := db.Pool.Exec(ctx, fmt.Sprintf("REFRESH MATERIALIZED VIEW CONCURRENTLY %s", view)); err != nil {
			db.Logger.Warn().Err(err).Str(logFieldView, view).Msg("failed to refresh materialized view concurrently (trying non-concurrently)")
			// Fallback to non-concurrent refresh if concurrent fails (e.g. if it was never populated)
			if _, err := db.Pool.Exec(ctx, fmt.Sprintf("REFRESH MATERIALIZED VIEW %s", view)); err != nil {
				db.Logger.Error().Err(err).Str(logFieldView, view).Msg("failed to refresh materialized view")
			}
		}
	}

	return nil
}

func (db *DB) rebuildResearchDerivedTables(ctx context.Context) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback(ctx) //nolint:errcheck
	}()

	if err := db.rebuildClusterFirstAppearance(ctx, tx); err != nil {
		return err
	}

	if err := db.rebuildClusterTopicHistory(ctx, tx); err != nil {
		return err
	}

	if err := db.rebuildEvidenceClaims(ctx, tx); err != nil {
		return err
	}

	if err := db.rebuildClusterLanguageLinks(ctx, tx); err != nil {
		return err
	}

	db.Logger.Info().Msg("committing research derived tables transaction")

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit research derived tables: %w", err)
	}

	return nil
}

func (db *DB) rebuildClusterFirstAppearance(ctx context.Context, tx pgx.Tx) error {
	db.Logger.Info().Msg("rebuilding cluster_first_appearance")

	if _, err := tx.Exec(ctx, "TRUNCATE cluster_first_appearance"); err != nil {
		return fmt.Errorf("truncate cluster_first_appearance: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO cluster_first_appearance (cluster_id, channel_id, first_item_id, first_seen_at)
		WITH ranked AS (
			SELECT ci.cluster_id,
			       rm.channel_id,
			       i.id AS item_id,
			       rm.tg_date,
			       ROW_NUMBER() OVER (PARTITION BY ci.cluster_id ORDER BY rm.tg_date ASC) AS rn
			FROM cluster_items ci
			JOIN clusters c ON ci.cluster_id = c.id AND c.source = $1
			JOIN items i ON ci.item_id = i.id
			JOIN raw_messages rm ON i.raw_message_id = rm.id
		)
		SELECT cluster_id, channel_id, item_id, tg_date
		FROM ranked
		WHERE rn = 1
	`, ClusterSourceResearch); err != nil {
		return fmt.Errorf("populate cluster_first_appearance: %w", err)
	}

	return nil
}

func (db *DB) rebuildClusterTopicHistory(ctx context.Context, tx pgx.Tx) error {
	db.Logger.Info().Msg("rebuilding cluster_topic_history")

	if _, err := tx.Exec(ctx, "TRUNCATE cluster_topic_history"); err != nil {
		return fmt.Errorf("truncate cluster_topic_history: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO cluster_topic_history (cluster_id, topic, window_start, window_end)
		SELECT ci.cluster_id,
		       i.topic,
		       date_trunc('week', rm.tg_date),
		       date_trunc('week', rm.tg_date) + INTERVAL '7 days'
		FROM cluster_items ci
		JOIN clusters c ON ci.cluster_id = c.id AND c.source = $1
		JOIN items i ON ci.item_id = i.id
		JOIN raw_messages rm ON i.raw_message_id = rm.id
		WHERE i.topic IS NOT NULL AND i.topic <> ''
		GROUP BY ci.cluster_id, i.topic, date_trunc('week', rm.tg_date)
	`, ClusterSourceResearch); err != nil {
		return fmt.Errorf("populate cluster_topic_history: %w", err)
	}

	return nil
}

func (db *DB) rebuildEvidenceClaims(ctx context.Context, tx pgx.Tx) error {
	db.Logger.Info().Msg("rebuilding claims (evidence-based)")

	if _, err := tx.Exec(ctx, "DELETE FROM claims WHERE normalized_hash IS NULL"); err != nil {
		return fmt.Errorf("delete old evidence claims: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO claims (claim_text, first_seen_at, origin_cluster_id, cluster_ids, contradicted_by)
		WITH claim_data AS (
			-- Gather all raw claim occurrences from evidence
			SELECT ec.claim_text,
			       rm.tg_date,
			       c.id AS cluster_id,
			       ie.evidence_id,
			       ie.is_contradiction
			FROM evidence_claims ec
			JOIN item_evidence ie ON ie.evidence_id = ec.evidence_id
			JOIN items i ON i.id = ie.item_id
			JOIN raw_messages rm ON i.raw_message_id = rm.id
			LEFT JOIN cluster_items ci ON ci.item_id = i.id
			LEFT JOIN clusters c ON c.id = ci.cluster_id AND c.source = $1
		),
		claim_origin AS (
			-- Identify the first occurrence (with or without cluster)
			SELECT DISTINCT ON (claim_text)
			       claim_text,
			       cluster_id
			FROM claim_data
			ORDER BY claim_text, tg_date ASC
		)
		SELECT d.claim_text,
		       MIN(d.tg_date) AS first_seen_at,
		       o.cluster_id AS origin_cluster_id,
		       array_remove(ARRAY_AGG(DISTINCT d.cluster_id), NULL) AS cluster_ids,
		       COALESCE(ARRAY_AGG(DISTINCT d.evidence_id) FILTER (WHERE d.is_contradiction), '{}'::uuid[]) AS contradicted_by
		FROM claim_data d
		JOIN claim_origin o ON d.claim_text = o.claim_text
		GROUP BY d.claim_text, o.cluster_id
	`, ClusterSourceResearch); err != nil {
		return fmt.Errorf("populate evidence claims: %w", err)
	}

	return nil
}

func (db *DB) rebuildClusterLanguageLinks(ctx context.Context, tx pgx.Tx) error {
	db.Logger.Info().Msg("rebuilding cluster_language_links")

	if _, err := tx.Exec(ctx, "TRUNCATE cluster_language_links"); err != nil {
		return fmt.Errorf("truncate cluster_language_links: %w", err)
	}

	if _, err := tx.Exec(ctx, fmt.Sprintf(`
		WITH cluster_rep AS (
			SELECT ci.cluster_id,
			       i.language,
			       e.embedding,
			       rm.tg_date,
			       ROW_NUMBER() OVER (
						PARTITION BY ci.cluster_id
						ORDER BY i.importance_score DESC NULLS LAST, rm.tg_date DESC
			       ) AS rn
			FROM cluster_items ci
			JOIN clusters c ON ci.cluster_id = c.id AND c.source = '%s'
			JOIN items i ON ci.item_id = i.id
			JOIN raw_messages rm ON i.raw_message_id = rm.id
			JOIN embeddings e ON e.item_id = i.id
			WHERE i.language IS NOT NULL AND i.language <> ''
		),
		rep AS (
			SELECT cluster_id, language, embedding, tg_date
			FROM cluster_rep
			WHERE rn = 1
		),
		pairs AS (
			SELECT a.cluster_id AS cluster_id,
			       b.cluster_id AS linked_cluster_id,
			       a.language AS source_lang,
			       b.language AS target_lang,
			       1 - (a.embedding <=> b.embedding) AS similarity,
			       a.tg_date AS source_date,
			       b.tg_date AS target_date
			FROM rep a
			JOIN rep b ON a.cluster_id < b.cluster_id AND a.language <> b.language
			WHERE abs(EXTRACT(epoch FROM (a.tg_date - b.tg_date))) <= %d
		),
		filtered AS (
			SELECT cluster_id, linked_cluster_id, source_lang, target_lang, similarity
			FROM pairs
			WHERE similarity >= %0.2f
		)
		INSERT INTO cluster_language_links (cluster_id, language, linked_cluster_id, confidence)
		SELECT cluster_id, target_lang, linked_cluster_id, similarity
		FROM filtered
		UNION ALL
		SELECT linked_cluster_id, source_lang, cluster_id, similarity
		FROM filtered
	`, ClusterSourceResearch, langLinkMaxLagSeconds, langLinkMinSimilarity)); err != nil {
		return fmt.Errorf("populate cluster_language_links: %w", err)
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
			return nil, ErrChannelRelevanceNotConfigured
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
			return nil, ErrRelevanceGateNotFound
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

// ItemForHeuristicClaim represents an item that needs heuristic claim extraction.
type ItemForHeuristicClaim struct {
	ItemID    string
	Summary   string
	ClusterID string
	TgDate    time.Time
}

// GetItemsWithoutEvidenceClaims returns items that are in clusters but have no evidence claims.
// These items need heuristic claim extraction.
func (db *DB) GetItemsWithoutEvidenceClaims(ctx context.Context, limit int) ([]ItemForHeuristicClaim, error) {
	if limit <= 0 {
		limit = 1000
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT DISTINCT ON (ci.cluster_id)
		       i.id AS item_id,
		       i.summary,
		       ci.cluster_id,
		       rm.tg_date
		FROM cluster_items ci
		JOIN clusters c ON ci.cluster_id = c.id AND c.source = $2
		JOIN items i ON ci.item_id = i.id
		JOIN raw_messages rm ON i.raw_message_id = rm.id
		LEFT JOIN item_evidence ie ON ie.item_id = i.id
		WHERE ie.id IS NULL
		  AND i.summary IS NOT NULL
		  AND LENGTH(i.summary) > 30
		ORDER BY ci.cluster_id, rm.tg_date ASC
		LIMIT $1
	`, limit, ClusterSourceResearch)
	if err != nil {
		return nil, fmt.Errorf("query items without evidence: %w", err)
	}
	defer rows.Close()

	var items []ItemForHeuristicClaim

	for rows.Next() {
		var item ItemForHeuristicClaim

		var itemID, clusterID pgtype.UUID

		if err := rows.Scan(&itemID, &item.Summary, &clusterID, &item.TgDate); err != nil {
			return nil, fmt.Errorf("scan item: %w", err)
		}

		item.ItemID = fromUUID(itemID)
		item.ClusterID = fromUUID(clusterID)
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate items: %w", err)
	}

	return items, nil
}

// HeuristicClaimInput represents a heuristic claim to be inserted.
type HeuristicClaimInput struct {
	ClaimText       string
	NormalizedHash  string
	FirstSeenAt     time.Time
	OriginClusterID string
	ClusterIDs      []string
	Embedding       []float32 // Optional embedding for semantic similarity
}

// SimilarClaim represents a claim found by embedding similarity search.
type SimilarClaim struct {
	ID         string
	ClaimText  string
	Similarity float64
}

// InsertHeuristicClaims inserts claims extracted using heuristic methods.
// It deduplicates by normalized_hash and merges cluster_ids for existing claims.
func (db *DB) InsertHeuristicClaims(ctx context.Context, claims []HeuristicClaimInput) (int64, error) {
	if len(claims) == 0 {
		return 0, nil
	}

	var inserted int64

	for _, claim := range claims {
		clusterIDs := make([]pgtype.UUID, len(claim.ClusterIDs))
		for i, id := range claim.ClusterIDs {
			clusterIDs[i] = toUUID(id)
		}

		var (
			result pgconn.CommandTag
			err    error
		)

		if len(claim.Embedding) > 0 {
			result, err = db.Pool.Exec(ctx, `
				INSERT INTO claims (claim_text, first_seen_at, origin_cluster_id, cluster_ids, contradicted_by, normalized_hash, embedding)
				VALUES ($1, $2, $3, $4, '{}', $5, $6)
				ON CONFLICT (normalized_hash) DO UPDATE SET
					cluster_ids = (
						SELECT ARRAY(SELECT DISTINCT unnest(claims.cluster_ids || EXCLUDED.cluster_ids))
					),
					first_seen_at = LEAST(claims.first_seen_at, EXCLUDED.first_seen_at),
					embedding = COALESCE(EXCLUDED.embedding, claims.embedding),
					updated_at = NOW()
			`, claim.ClaimText, claim.FirstSeenAt, toUUID(claim.OriginClusterID), clusterIDs, claim.NormalizedHash, pgvector.NewVector(claim.Embedding))
		} else {
			result, err = db.Pool.Exec(ctx, `
				INSERT INTO claims (claim_text, first_seen_at, origin_cluster_id, cluster_ids, contradicted_by, normalized_hash)
				VALUES ($1, $2, $3, $4, '{}', $5)
				ON CONFLICT (normalized_hash) DO UPDATE SET
					cluster_ids = (
						SELECT ARRAY(SELECT DISTINCT unnest(claims.cluster_ids || EXCLUDED.cluster_ids))
					),
					first_seen_at = LEAST(claims.first_seen_at, EXCLUDED.first_seen_at),
					updated_at = NOW()
			`, claim.ClaimText, claim.FirstSeenAt, toUUID(claim.OriginClusterID), clusterIDs, claim.NormalizedHash)
		}

		if err != nil {
			return inserted, fmt.Errorf("insert heuristic claim: %w", err)
		}

		inserted += result.RowsAffected()
	}

	return inserted, nil
}

// CountEvidenceBasedClaims returns the number of claims populated from evidence.
func (db *DB) CountEvidenceBasedClaims(ctx context.Context) (int64, error) {
	var count int64

	err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM claims WHERE normalized_hash IS NULL`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count evidence claims: %w", err)
	}

	return count, nil
}

// CountHeuristicClaims returns the number of claims populated from heuristic extraction.
func (db *DB) CountHeuristicClaims(ctx context.Context) (int64, error) {
	var count int64

	err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM claims WHERE normalized_hash IS NOT NULL`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count heuristic claims: %w", err)
	}

	return count, nil
}

// FindSimilarClaimsByEmbedding finds claims with embedding similarity above the threshold.
// Uses pgvector cosine distance operator (<=>). Returns claims ordered by similarity (descending).
// The threshold parameter should be 0-1 where 1 is identical; we convert to distance.
func (db *DB) FindSimilarClaimsByEmbedding(ctx context.Context, embedding []float32, limit int, threshold float64) ([]SimilarClaim, error) {
	if len(embedding) == 0 {
		return nil, nil
	}

	// Convert similarity threshold to distance (cosine distance = 1 - similarity)
	distanceThreshold := 1.0 - threshold

	rows, err := db.Pool.Query(ctx, `
		SELECT id, claim_text, 1.0 - (embedding <=> $1::vector) as similarity
		FROM claims
		WHERE embedding IS NOT NULL
		  AND (embedding <=> $1::vector) < $2
		ORDER BY embedding <=> $1::vector
		LIMIT $3
	`, pgvector.NewVector(embedding), distanceThreshold, limit)
	if err != nil {
		return nil, fmt.Errorf("find similar claims: %w", err)
	}
	defer rows.Close()

	var claims []SimilarClaim

	for rows.Next() {
		var claim SimilarClaim

		var claimID pgtype.UUID

		if err := rows.Scan(&claimID, &claim.ClaimText, &claim.Similarity); err != nil {
			return nil, fmt.Errorf("scan similar claim: %w", err)
		}

		claim.ID = fromUUID(claimID)
		claims = append(claims, claim)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate similar claims: %w", err)
	}

	return claims, nil
}

// UpdateClaimClusters adds cluster IDs to an existing claim.
func (db *DB) UpdateClaimClusters(ctx context.Context, claimID string, clusterIDs []string) error {
	clusterUUIDs := make([]pgtype.UUID, len(clusterIDs))
	for i, id := range clusterIDs {
		clusterUUIDs[i] = toUUID(id)
	}

	_, err := db.Pool.Exec(ctx, `
		UPDATE claims
		SET cluster_ids = (
			SELECT ARRAY(SELECT DISTINCT unnest(cluster_ids || $2::uuid[]))
		),
		updated_at = NOW()
		WHERE id = $1
	`, toUUID(claimID), clusterUUIDs)
	if err != nil {
		return fmt.Errorf("update claim clusters: %w", err)
	}

	return nil
}
