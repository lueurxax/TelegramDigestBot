package research

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const (
	researchCookieName     = "research_session"
	researchCookiePath     = "/research"
	researchLoginParam     = "token"
	researchQueryLayout    = "2006-01-02"
	maxBodyBytes           = 1 << 20
	defaultSearchLimit     = 50
	maxSearchLimit         = 200
	defaultWeeklyDiffLimit = 10
	slowQueryThreshold     = 2 * time.Second

	// Route path constants.
	routeLogin    = "login"
	routeSearch   = "search"
	routeItem     = "item/"
	routeCluster  = "cluster/"
	routeEvidence = "evidence/"
	routeSettings = "settings"
	routeChannels = "channels/"
	routeClaims   = "claims"
	routeRebuild  = "rebuild"
	routeTopics   = "topics/"
	routeDiff     = "diff/"

	// Scope constants.
	scopeAll      = "all"
	scopeItems    = "items"
	scopeEvidence = "evidence"

	// Error title constants.
	errTitleNotFound       = "Not Found"
	errTitleError          = "Error"
	errTitleMethodNotAllow = "Method Not Allowed"
	errTitleBadRequest     = "Bad Request"
	errTitleUnauthorized   = "Unauthorized"
	errTitleInvalidRange   = "Invalid Range"

	// Error message constants.
	errMsgCreateSession = "Failed to create session."
	errMsgGetEvidence   = "get evidence failed"
	errMsgRenderTable   = "Failed to render table."
	errMsgWeeklyDiff    = "Failed to load weekly diff."
	errMsgLoginRequired = "Login required."

	// Content type constants.
	contentTypeHeader = "Content-Type"
	contentTypeHTML   = "text/html; charset=utf-8"
	contentTypeJSON   = "application/json; charset=utf-8"

	// Template constants.
	tmplTable = "table.html"

	// Format constants.
	fmtFloat2 = "%.2f"

	// Settings keys.
	settingRelevanceThreshold  = "relevance_threshold"
	settingImportanceThreshold = "importance_threshold"
	settingDigestLanguage      = "digest_language"
	settingDigestSchedule      = "digest_schedule"

	// Log field names.
	logFieldRoute     = "route"
	logFieldQueryHash = "query_hash"
	logFieldScope     = "scope"

	// Time bucket constants.
	bucketWeek  = "week"
	bucketDay   = "day"
	bucketMonth = "month"

	// Source constants.
	sourceEnv = "env"
	sourceDB  = "db"
)

// Static errors for err113 compliance.
var (
	errInvalidScope  = errors.New("invalid scope")
	errRangeRequired = errors.New("from and to are required")
	errInvalidBucket = errors.New("invalid bucket")
)

// Handler serves research API and HTML views.
type Handler struct {
	cfg          *config.Config
	db           *db.DB
	tokenService *AuthTokenService
	renderer     *Renderer
	logger       *zerolog.Logger
}

// NewHandler creates a new research handler.
func NewHandler(cfg *config.Config, dbConn *db.DB, tokenService *AuthTokenService, logger *zerolog.Logger) (*Handler, error) {
	renderer, err := NewRenderer()
	if err != nil {
		return nil, err
	}

	return &Handler{
		cfg:          cfg,
		db:           dbConn,
		tokenService: tokenService,
		renderer:     renderer,
		logger:       logger,
	}, nil
}

// ServeHTTP routes requests to research endpoints.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	route, status, resultSize := h.dispatch(w, r)

	h.recordMetrics(route, status, resultSize, start)
}

// dispatch handles route matching and dispatches to the appropriate handler.
func (h *Handler) dispatch(w http.ResponseWriter, r *http.Request) (route string, status int, resultSize int) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		return "index", h.handleIndex(w, r), 0
	}

	return h.dispatchPath(w, r, path)
}

// dispatchPath matches the path to a handler and executes it.
func (h *Handler) dispatchPath(w http.ResponseWriter, r *http.Request, path string) (route string, status int, resultSize int) {
	switch {
	case strings.HasPrefix(path, routeLogin):
		return "login", h.handleLogin(w, r), 0
	case strings.HasPrefix(path, routeSearch):
		s, rs := h.handleSearch(w, r)
		return "search", s, rs
	case strings.HasPrefix(path, routeItem):
		return "item", h.handleItem(w, r, strings.TrimPrefix(path, "item/")), 0
	case strings.HasPrefix(path, routeCluster):
		return "cluster", h.handleCluster(w, r, strings.TrimPrefix(path, "cluster/")), 0
	case strings.HasPrefix(path, routeEvidence):
		return scopeEvidence, h.handleEvidence(w, r, strings.TrimPrefix(path, routeEvidence)), 0
	case strings.HasPrefix(path, routeSettings):
		return "settings", h.handleSettings(w, r), 0
	default:
		return h.dispatchExtendedPath(w, r, path)
	}
}

// dispatchExtendedPath handles additional route patterns.
func (h *Handler) dispatchExtendedPath(w http.ResponseWriter, r *http.Request, path string) (route string, status int, resultSize int) {
	switch {
	case strings.HasPrefix(path, routeChannels+"overlap"):
		s, rs := h.handleChannelOverlap(w, r)
		return "channels_overlap", s, rs
	case strings.HasPrefix(path, routeTopics+"timeline"):
		s, rs := h.handleTopicTimeline(w, r)
		return "topics_timeline", s, rs
	case strings.HasPrefix(path, routeChannels):
		s, rs := h.handleChannelDetail(w, r, strings.TrimPrefix(path, routeChannels))
		return "channels_detail", s, rs
	case strings.HasPrefix(path, routeClaims):
		s, rs := h.handleClaims(w, r)
		return "claims", s, rs
	case strings.HasPrefix(path, routeDiff+"weekly"):
		s, rs := h.handleWeeklyDiff(w, r)
		return "diff_weekly", s, rs
	case strings.HasPrefix(path, routeRebuild):
		return "rebuild", h.handleRebuild(w, r), 0
	default:
		return "not_found", h.writeError(w, r, http.StatusNotFound, errTitleNotFound, "Unknown research endpoint."), 0
	}
}

// recordMetrics records request metrics.
func (h *Handler) recordMetrics(route string, status, resultSize int, start time.Time) {
	latencyHistogram.WithLabelValues(route).Observe(time.Since(start).Seconds())
	requestsTotal.WithLabelValues(route, strconv.Itoa(status)).Inc()

	if resultSize > 0 {
		resultSizeGauge.WithLabelValues(route).Set(float64(resultSize))
	}
}

func (h *Handler) logValidationError(route, rawQuery string, err error) {
	h.logger.Warn().
		Str(logFieldRoute, route).
		Str(logFieldQueryHash, hashString(rawQuery)).
		Err(err).
		Msg("research validation failed")
}

func (h *Handler) logSlowQuery(route, queryHash string, start time.Time, scope string) {
	elapsed := time.Since(start)
	if elapsed < slowQueryThreshold {
		return
	}

	h.logger.Warn().
		Str(logFieldRoute, route).
		Str(logFieldScope, scope).
		Str(logFieldQueryHash, queryHash).
		Dur("duration", elapsed).
		Msg("research query slow")
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) int {
	if _, ok := h.requireSession(w, r); !ok {
		return http.StatusUnauthorized
	}

	data := IndexViewData{
		Title: "Research Dashboard",
		Now:   time.Now(),
	}
	if !wantsHTML(r) {
		return h.writeJSON(w, http.StatusOK, data)
	}

	if err := h.renderHTML(w, "index.html", data); err != nil {
		h.logger.Error().Err(err).Msg("render index failed")
		return h.writeError(w, r, http.StatusInternalServerError, errTitleError, "Failed to render page.")
	}

	return http.StatusOK
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) int {
	if r.Method != http.MethodGet {
		return h.writeError(w, r, http.StatusMethodNotAllowed, errTitleMethodNotAllow, "Use GET to login.")
	}

	token := r.URL.Query().Get(researchLoginParam)
	if token == "" {
		return h.writeError(w, r, http.StatusBadRequest, errTitleBadRequest, "Missing token.")
	}

	payload, err := h.tokenService.Verify(token)
	if err != nil {
		title := "Invalid Token"
		if errors.Is(err, ErrAuthTokenExpired) {
			title = "Expired Token"
		}

		return h.writeError(w, r, http.StatusUnauthorized, title, "Login token is invalid or expired.")
	}

	if !h.isAdmin(payload.UserID) {
		return h.writeError(w, r, http.StatusUnauthorized, errTitleUnauthorized, "You do not have access.")
	}

	sessionToken, err := GenerateSessionToken()
	if err != nil {
		h.logger.Error().Err(err).Msg("generate session token failed")
		return h.writeError(w, r, http.StatusInternalServerError, errTitleError, errMsgCreateSession)
	}

	expiresAt := time.Now().Add(DefaultSessionTTL)
	if err := h.db.CreateResearchSession(r.Context(), sessionToken, payload.UserID, expiresAt); err != nil {
		h.logger.Error().Err(err).Msg("create research session failed")
		return h.writeError(w, r, http.StatusInternalServerError, errTitleError, errMsgCreateSession)
	}

	h.setSessionCookie(w, sessionToken, expiresAt)
	http.Redirect(w, r, "/research/", http.StatusSeeOther)

	return http.StatusSeeOther
}

func (h *Handler) handleSearch(w http.ResponseWriter, r *http.Request) (int, int) {
	if _, ok := h.requireSession(w, r); !ok {
		return http.StatusUnauthorized, 0
	}

	params, scope, err := parseSearchParams(r)
	if err != nil {
		h.logValidationError(routeSearch, r.URL.RawQuery, err)
		return h.writeError(w, r, http.StatusBadRequest, "Invalid Parameters", err.Error()), 0
	}

	queryHash := hashSearchParams(params, scope)

	items, itemCount, evidence, evCount, errStatus := h.executeSearch(r, params, scope, queryHash)
	if errStatus != 0 {
		return errStatus, 0
	}

	return h.renderSearchResults(w, r, params, scope, items, itemCount, evidence, evCount)
}

// executeSearch performs the actual search based on scope.
func (h *Handler) executeSearch(r *http.Request, params db.ResearchSearchParams, scope string, queryHash string) (
	[]db.ResearchItemSearchResult, *db.ResearchSearchResultCount,
	[]db.ResearchEvidenceSearchResult, *db.ResearchSearchResultCount, int,
) {
	var (
		items     []db.ResearchItemSearchResult
		evidence  []db.ResearchEvidenceSearchResult
		itemCount *db.ResearchSearchResultCount
		evCount   *db.ResearchSearchResultCount
		err       error
	)

	if scope == scopeItems || scope == scopeAll {
		start := time.Now()

		items, itemCount, err = h.db.SearchResearchItems(r.Context(), params)
		if err != nil {
			h.logger.Error().Err(err).Msg("search research items failed")
			return nil, nil, nil, nil, http.StatusInternalServerError
		}

		h.logSlowQuery(routeSearch, queryHash, start, scopeItems)
	}

	if scope == scopeEvidence || scope == scopeAll {
		start := time.Now()

		evidence, evCount, err = h.db.SearchResearchEvidence(r.Context(), params)
		if err != nil {
			h.logger.Error().Err(err).Msg("search research evidence failed")
			return nil, nil, nil, nil, http.StatusInternalServerError
		}

		h.logSlowQuery(routeSearch, queryHash, start, scopeEvidence)
	}

	return items, itemCount, evidence, evCount, 0
}

// renderSearchResults renders search results as HTML or JSON.
func (h *Handler) renderSearchResults(
	w http.ResponseWriter, r *http.Request,
	params db.ResearchSearchParams, scope string,
	items []db.ResearchItemSearchResult, itemCount *db.ResearchSearchResultCount,
	evidence []db.ResearchEvidenceSearchResult, evCount *db.ResearchSearchResultCount,
) (int, int) {
	resultSize := len(items) + len(evidence)

	if wantsHTML(r) {
		data := SearchViewData{
			Title:         "Search Results",
			Params:        params,
			Scope:         scope,
			Items:         items,
			Evidence:      evidence,
			ItemCount:     itemCount,
			EvidenceCount: evCount,
		}
		if err := h.renderHTML(w, "search.html", data); err != nil {
			h.logger.Error().Err(err).Msg("render search failed")
			return h.writeError(w, r, http.StatusInternalServerError, errTitleError, "Failed to render results."), 0
		}

		return http.StatusOK, resultSize
	}

	resp := SearchResponse{
		Items:         items,
		Evidence:      evidence,
		ItemCount:     itemCount,
		EvidenceCount: evCount,
	}

	return h.writeJSON(w, http.StatusOK, resp), resultSize
}

func (h *Handler) handleItem(w http.ResponseWriter, r *http.Request, itemID string) int {
	if _, ok := h.requireSession(w, r); !ok {
		return http.StatusUnauthorized
	}

	item, err := h.db.GetItemDebugDetail(r.Context(), itemID)
	if err != nil {
		h.logger.Error().Err(err).Msg("get research item failed")
		return h.writeError(w, r, http.StatusInternalServerError, errTitleError, "Failed to load item.")
	}

	if item == nil {
		return h.writeError(w, r, http.StatusNotFound, errTitleNotFound, "Item not found.")
	}

	evidenceMap, err := h.db.GetEvidenceForItems(r.Context(), []string{itemID})
	if err != nil {
		h.logger.Error().Err(err).Msg(errMsgGetEvidence)
	}

	evidence := evidenceMap[itemID]

	cluster, clusterItems, err := h.db.GetClusterForItem(r.Context(), itemID)
	if err != nil {
		h.logger.Error().Err(err).Msg("get cluster for item failed")
	}

	explain, err := h.buildItemExplain(r.Context(), item)
	if err != nil {
		h.logger.Error().Err(err).Msg("build item explain failed")
	}

	if wantsHTML(r) {
		data := ItemViewData{
			Title:        "Item Details",
			Item:         item,
			Evidence:     evidence,
			Cluster:      cluster,
			ClusterItems: clusterItems,
			Explain:      explain,
		}
		if err := h.renderHTML(w, "item.html", data); err != nil {
			h.logger.Error().Err(err).Msg("render item failed")
			return h.writeError(w, r, http.StatusInternalServerError, errTitleError, "Failed to render item.")
		}

		return http.StatusOK
	}

	resp := ItemResponse{
		Item:         item,
		Evidence:     evidence,
		Cluster:      cluster,
		ClusterItems: clusterItems,
	}

	return h.writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleCluster(w http.ResponseWriter, r *http.Request, clusterID string) int {
	if _, ok := h.requireSession(w, r); !ok {
		return http.StatusUnauthorized
	}

	cluster, err := h.db.GetResearchCluster(r.Context(), clusterID)
	if err != nil {
		if errors.Is(err, db.ErrResearchClusterNotFound) {
			return h.writeError(w, r, http.StatusNotFound, errTitleNotFound, "Cluster not found.")
		}

		h.logger.Error().Err(err).Msg("get research cluster failed")

		return h.writeError(w, r, http.StatusInternalServerError, errTitleError, "Failed to load cluster.")
	}

	if wantsHTML(r) {
		data := ClusterViewData{
			Title:   "Cluster Details",
			Cluster: cluster,
		}
		if err := h.renderHTML(w, "cluster.html", data); err != nil {
			h.logger.Error().Err(err).Msg("render cluster failed")
			return h.writeError(w, r, http.StatusInternalServerError, errTitleError, "Failed to render cluster.")
		}

		return http.StatusOK
	}

	return h.writeJSON(w, http.StatusOK, cluster)
}

func (h *Handler) handleEvidence(w http.ResponseWriter, r *http.Request, itemID string) int {
	if _, ok := h.requireSession(w, r); !ok {
		return http.StatusUnauthorized
	}

	evidenceMap, err := h.db.GetEvidenceForItems(r.Context(), []string{itemID})
	if err != nil {
		h.logger.Error().Err(err).Msg(errMsgGetEvidence)
		return h.writeError(w, r, http.StatusInternalServerError, errTitleError, "Failed to load evidence.")
	}

	evidence := evidenceMap[itemID]

	if wantsHTML(r) {
		data := EvidenceViewData{
			Title:    "Evidence Sources",
			ItemID:   itemID,
			Evidence: evidence,
		}
		if err := h.renderHTML(w, "evidence.html", data); err != nil {
			h.logger.Error().Err(err).Msg("render evidence failed")
			return h.writeError(w, r, http.StatusInternalServerError, errTitleError, "Failed to render evidence.")
		}

		return http.StatusOK
	}

	return h.writeJSON(w, http.StatusOK, evidence)
}

func (h *Handler) handleSettings(w http.ResponseWriter, r *http.Request) int {
	if _, ok := h.requireSession(w, r); !ok {
		return http.StatusUnauthorized
	}

	settings, err := h.loadSettingsSnapshot(r.Context())
	if err != nil {
		h.logger.Error().Err(err).Msg("load settings failed")
		return h.writeError(w, r, http.StatusInternalServerError, errTitleError, "Failed to load settings.")
	}

	if wantsHTML(r) {
		rows := make([][]string, 0, len(settings))
		for _, entry := range settings {
			rows = append(rows, []string{entry.Name, entry.Value, entry.Source})
		}

		data := TableViewData{
			Title:       "Settings (Read-only)",
			Headers:     []string{"Setting", "Value", "Source"},
			Rows:        rows,
			Description: "Snapshot of digest configuration used by the pipeline.",
		}
		if err := h.renderHTML(w, tmplTable, data); err != nil {
			return h.writeError(w, r, http.StatusInternalServerError, errTitleError, errMsgRenderTable)
		}

		return http.StatusOK
	}

	return h.writeJSON(w, http.StatusOK, settings)
}

func (h *Handler) getEffectiveRelevanceThreshold(ctx context.Context, channelID string) (float32, error) {
	relThreshold := h.cfg.RelevanceThreshold
	if _, err := h.getSettingValue(ctx, settingRelevanceThreshold, &relThreshold); err != nil {
		return 0, err
	}

	channelSettings, err := h.db.GetChannelRelevanceSettings(ctx, channelID)
	if err != nil && !errors.Is(err, db.ErrChannelRelevanceNotConfigured) {
		return 0, fmt.Errorf("get channel relevance settings: %w", err)
	}

	if channelSettings != nil {
		if channelSettings.RelevanceThreshold > 0 {
			relThreshold = channelSettings.RelevanceThreshold
		}

		if channelSettings.AutoRelevanceEnabled {
			relThreshold += channelSettings.RelevanceThresholdDelta
		}
	}

	return clampFloat32(relThreshold, 0, 1), nil
}

func (h *Handler) getGateInfo(ctx context.Context, rawMessageID string) (*RelevanceGateInfo, error) {
	if rawMessageID == "" {
		return nil, nil //nolint:nilnil // nil,nil indicates no gate info available
	}

	gate, err := h.db.GetRelevanceGateDecision(ctx, rawMessageID)
	if err != nil {
		if errors.Is(err, db.ErrRelevanceGateNotFound) {
			return nil, nil //nolint:nilnil // nil,nil indicates no gate decision found
		}

		return nil, fmt.Errorf("get relevance gate decision: %w", err)
	}

	return &RelevanceGateInfo{
		Decision:    gate.Decision,
		Confidence:  gate.Confidence,
		Reason:      gate.Reason,
		Model:       gate.Model,
		GateVersion: gate.GateVersion,
	}, nil
}

func (h *Handler) buildItemExplain(ctx context.Context, item *db.ItemDebugDetail) (ItemExplainData, error) {
	relThreshold, err := h.getEffectiveRelevanceThreshold(ctx, item.ChannelID)
	if err != nil {
		return ItemExplainData{}, err
	}

	impThreshold := h.cfg.ImportanceThreshold
	if _, err := h.getSettingValue(ctx, settingImportanceThreshold, &impThreshold); err != nil {
		return ItemExplainData{}, err
	}

	gateInfo, err := h.getGateInfo(ctx, item.RawMessageID)
	if err != nil {
		return ItemExplainData{}, err
	}

	return ItemExplainData{
		Status:              item.Status,
		RelevanceScore:      item.RelevanceScore,
		RelevanceThreshold:  relThreshold,
		RelevancePass:       item.RelevanceScore >= relThreshold,
		ImportanceScore:     item.ImportanceScore,
		ImportanceThreshold: impThreshold,
		ImportancePass:      item.ImportanceScore >= impThreshold,
		Gate:                gateInfo,
	}, nil
}

func (h *Handler) getOverlapEmptyDescription(ctx context.Context) string {
	summary, err := h.db.GetChannelOverlapSummary(ctx)
	if err != nil {
		return ""
	}

	switch {
	case summary.TotalClusters == 0:
		return "No clustered items yet. Channel overlap appears after clusters span multiple channels."
	case summary.SharedClusters == 0:
		return fmt.Sprintf(
			"No shared clusters yet. Total clusters: %d across %d channels.",
			summary.TotalClusters,
			summary.TotalChannels,
		)
	default:
		return fmt.Sprintf(
			"No overlap rows returned. Shared clusters: %d out of %d.",
			summary.SharedClusters,
			summary.TotalClusters,
		)
	}
}

func (h *Handler) handleChannelOverlap(w http.ResponseWriter, r *http.Request) (int, int) {
	if _, ok := h.requireSession(w, r); !ok {
		return http.StatusUnauthorized, 0
	}

	from, to, err := parseRange(r)
	if err != nil {
		return h.writeError(w, r, http.StatusBadRequest, errTitleInvalidRange, err.Error()), 0
	}

	limit := parseLimit(r, defaultSearchLimit)

	edges, err := h.db.GetChannelOverlap(r.Context(), from, to, limit)
	if err != nil {
		h.logger.Error().Err(err).Msg("get channel overlap failed")
		return h.writeError(w, r, http.StatusInternalServerError, errTitleError, "Failed to load overlap."), 0
	}

	if wantsHTML(r) {
		rows := make([][]string, 0, len(edges))
		for _, edge := range edges {
			rows = append(rows, []string{edge.ChannelA, edge.ChannelB, strconv.Itoa(edge.Shared), fmt.Sprintf("%.3f", edge.Jaccard)})
		}

		description := ""
		if len(rows) == 0 {
			description = h.getOverlapEmptyDescription(r.Context())
		}

		data := TableViewData{
			Title:       "Channel Overlap",
			Headers:     []string{"Channel A", "Channel B", "Shared", "Jaccard"},
			Rows:        rows,
			Description: description,
		}
		if err := h.renderHTML(w, tmplTable, data); err != nil {
			return h.writeError(w, r, http.StatusInternalServerError, errTitleError, errMsgRenderTable), 0
		}

		return http.StatusOK, len(rows)
	}

	return h.writeJSON(w, http.StatusOK, edges), len(edges)
}

func (h *Handler) handleTopicTimeline(w http.ResponseWriter, r *http.Request) (int, int) {
	if _, ok := h.requireSession(w, r); !ok {
		return http.StatusUnauthorized, 0
	}

	from, to, err := parseRange(r)
	if err != nil {
		return h.writeError(w, r, http.StatusBadRequest, errTitleInvalidRange, err.Error()), 0
	}

	bucket, err := normalizeTimelineBucket(r.URL.Query().Get("bucket"))
	if err != nil {
		return h.writeError(w, r, http.StatusBadRequest, "Invalid Bucket", "Use day, week, or month."), 0
	}

	limit := parseLimit(r, defaultSearchLimit)

	points, err := h.db.GetTopicTimeline(r.Context(), bucket, from, to, limit)
	if err != nil {
		h.logger.Error().Err(err).Msg("get topic timeline failed")
		return h.writeError(w, r, http.StatusInternalServerError, errTitleError, "Failed to load timeline."), 0
	}

	if wantsHTML(r) {
		rows := make([][]string, 0, len(points))
		for _, p := range points {
			rows = append(rows, []string{p.BucketDate.Format(researchQueryLayout), p.Topic, strconv.Itoa(p.ItemCount)})
		}

		data := TableViewData{
			Title:   "Topic Timeline",
			Headers: []string{"Bucket", "Topic", "Count"},
			Rows:    rows,
		}
		if err := h.renderHTML(w, tmplTable, data); err != nil {
			return h.writeError(w, r, http.StatusInternalServerError, errTitleError, errMsgRenderTable), 0
		}

		return http.StatusOK, len(rows)
	}

	return h.writeJSON(w, http.StatusOK, points), len(points)
}

func normalizeTimelineBucket(raw string) (string, error) {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return bucketWeek, nil
	}

	switch value {
	case bucketWeek, "weekly":
		return bucketWeek, nil
	case bucketDay, "daily":
		return bucketDay, nil
	case bucketMonth, "monthly":
		return bucketMonth, nil
	default:
		return "", errInvalidBucket
	}
}

func (h *Handler) handleChannelDetail(w http.ResponseWriter, r *http.Request, path string) (int, int) {
	if _, ok := h.requireSession(w, r); !ok {
		return http.StatusUnauthorized, 0
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		return h.writeError(w, r, http.StatusBadRequest, "Bad Request", "Missing channel subcommand."), 0
	}

	channelID := parts[0]

	switch parts[1] {
	case "quality":
		return h.handleChannelQuality(w, r, channelID)
	case "origin-stats":
		return h.handleChannelOriginStats(w, r, channelID)
	default:
		return h.writeError(w, r, http.StatusNotFound, errTitleNotFound, "Unknown channel endpoint."), 0
	}
}

func (h *Handler) handleChannelQuality(w http.ResponseWriter, r *http.Request, channelID string) (int, int) {
	from, to, err := parseRange(r)
	if err != nil {
		return h.writeError(w, r, http.StatusBadRequest, errTitleInvalidRange, err.Error()), 0
	}

	entries, err := h.db.GetChannelQualityHistory(r.Context(), channelID, from, to)
	if err != nil {
		h.logger.Error().Err(err).Msg("get channel quality failed")
		return h.writeError(w, r, http.StatusInternalServerError, errTitleError, "Failed to load channel quality."), 0
	}

	if wantsHTML(r) {
		rows := make([][]string, 0, len(entries))
		for _, e := range entries {
			rows = append(rows, []string{
				e.PeriodStart.Format(researchQueryLayout),
				e.PeriodEnd.Format(researchQueryLayout),
				fmt.Sprintf(fmtFloat2, e.InclusionRate),
				fmt.Sprintf(fmtFloat2, e.NoiseRate),
			})
		}

		data := TableViewData{
			Title:   "Channel Quality",
			Headers: []string{"Start", "End", "Inclusion", "Noise"},
			Rows:    rows,
		}
		if err := h.renderHTML(w, tmplTable, data); err != nil {
			return h.writeError(w, r, http.StatusInternalServerError, errTitleError, errMsgRenderTable), 0
		}

		return http.StatusOK, len(rows)
	}

	return h.writeJSON(w, http.StatusOK, entries), len(entries)
}

func (h *Handler) handleChannelOriginStats(w http.ResponseWriter, r *http.Request, channelID string) (int, int) {
	from, to, err := parseRange(r)
	if err != nil {
		return h.writeError(w, r, http.StatusBadRequest, errTitleInvalidRange, err.Error()), 0
	}

	stats, err := h.db.GetOriginStats(r.Context(), channelID, from, to)
	if err != nil {
		h.logger.Error().Err(err).Msg("get origin stats failed")
		return h.writeError(w, r, http.StatusInternalServerError, errTitleError, "Failed to load origin stats."), 0
	}

	if wantsHTML(r) {
		rows := [][]string{{stats.ChannelID, strconv.Itoa(stats.OriginCount), strconv.Itoa(stats.TotalCount), fmt.Sprintf(fmtFloat2, stats.OriginRate)}}

		data := TableViewData{
			Title:   "Origin vs Amplifier",
			Headers: []string{"Channel", "Origin Count", "Total", "Origin Rate"},
			Rows:    rows,
		}
		if err := h.renderHTML(w, tmplTable, data); err != nil {
			return h.writeError(w, r, http.StatusInternalServerError, errTitleError, errMsgRenderTable), 0
		}

		return http.StatusOK, len(rows)
	}

	return h.writeJSON(w, http.StatusOK, stats), 1
}

func (h *Handler) getClaimsEmptyDescription(ctx context.Context) string {
	summary, err := h.db.GetClaimsSummary(ctx)
	if err != nil {
		return ""
	}

	switch {
	case summary.EvidenceClaimsCount == 0:
		return "No claims extracted yet. Claim ledger is built from LLM-extracted evidence claims."
	case summary.ClaimsCount == 0:
		return fmt.Sprintf(
			"Claims table empty but evidence claims exist (%d). Run /research/rebuild to populate claims.",
			summary.EvidenceClaimsCount,
		)
	default:
		return fmt.Sprintf("No claims returned. Evidence claims: %d.", summary.EvidenceClaimsCount)
	}
}

func (h *Handler) handleClaims(w http.ResponseWriter, r *http.Request) (int, int) {
	if _, ok := h.requireSession(w, r); !ok {
		return http.StatusUnauthorized, 0
	}

	from, to, err := parseRange(r)
	if err != nil {
		return h.writeError(w, r, http.StatusBadRequest, errTitleInvalidRange, err.Error()), 0
	}

	limit := parseLimit(r, defaultSearchLimit)

	claims, err := h.db.GetClaims(r.Context(), from, to, limit)
	if err != nil {
		h.logger.Error().Err(err).Msg("get claims failed")
		return h.writeError(w, r, http.StatusInternalServerError, errTitleError, "Failed to load claims."), 0
	}

	if wantsHTML(r) {
		rows := make([][]string, 0, len(claims))
		for _, c := range claims {
			rows = append(rows, []string{c.ID, c.ClaimText, c.FirstSeenAt.Format(time.RFC3339)})
		}

		description := ""
		if len(rows) == 0 {
			description = h.getClaimsEmptyDescription(r.Context())
		}

		data := TableViewData{
			Title:       "Claim Ledger",
			Headers:     []string{"ID", "Claim", "First Seen"},
			Rows:        rows,
			Description: description,
		}
		if err := h.renderHTML(w, tmplTable, data); err != nil {
			return h.writeError(w, r, http.StatusInternalServerError, errTitleError, errMsgRenderTable), 0
		}

		return http.StatusOK, len(rows)
	}

	return h.writeJSON(w, http.StatusOK, claims), len(claims)
}

func (h *Handler) handleWeeklyDiff(w http.ResponseWriter, r *http.Request) (int, int) {
	if _, ok := h.requireSession(w, r); !ok {
		return http.StatusUnauthorized, 0
	}

	from, to, err := parseRangeRequired(r)
	if err != nil {
		return h.writeError(w, r, http.StatusBadRequest, errTitleInvalidRange, err.Error()), 0
	}

	limit := parseLimit(r, defaultWeeklyDiffLimit)

	topics, err := h.db.GetWeeklyDiff(r.Context(), from, to, limit)
	if err != nil {
		h.logger.Error().Err(err).Msg("get weekly diff failed")
		return h.writeError(w, r, http.StatusInternalServerError, errTitleError, errMsgWeeklyDiff), 0
	}

	channels, err := h.db.GetWeeklyChannelDiff(r.Context(), from, to, limit)
	if err != nil {
		h.logger.Error().Err(err).Msg("get weekly channel diff failed")
		return h.writeError(w, r, http.StatusInternalServerError, errTitleError, errMsgWeeklyDiff), 0
	}

	if wantsHTML(r) {
		rows := make([][]string, 0, len(topics))
		for _, t := range topics {
			rows = append(rows, []string{t.Topic, strconv.Itoa(t.Delta)})
		}

		data := TableViewData{
			Title:            "Weekly Diff (Topics)",
			Headers:          []string{"Topic", "Delta"},
			Rows:             rows,
			SecondaryTitle:   "Weekly Diff (Channels)",
			SecondaryHeaders: []string{"Channel", "Delta"},
			SecondaryRows:    buildChannelDiffRows(channels),
		}
		if err := h.renderHTML(w, tmplTable, data); err != nil {
			return h.writeError(w, r, http.StatusInternalServerError, errTitleError, errMsgRenderTable), 0
		}

		return http.StatusOK, len(rows)
	}

	resp := WeeklyDiffResponse{
		Topics:   topics,
		Channels: channels,
	}

	return h.writeJSON(w, http.StatusOK, resp), len(topics) + len(channels)
}

func (h *Handler) handleRebuild(w http.ResponseWriter, r *http.Request) int {
	if _, ok := h.requireSession(w, r); !ok {
		return http.StatusUnauthorized
	}

	if r.Method != http.MethodPost {
		return h.writeError(w, r, http.StatusMethodNotAllowed, "Method Not Allowed", "Use POST to rebuild.")
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := h.db.RefreshResearchMaterializedViews(r.Context()); err != nil {
		h.logger.Error().Err(err).Msg("refresh materialized views failed")
		return h.writeError(w, r, http.StatusInternalServerError, errTitleError, "Failed to refresh views.")
	}

	return h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) requireSession(w http.ResponseWriter, r *http.Request) (int64, bool) {
	cookie, err := r.Cookie(researchCookieName)
	if err != nil || cookie.Value == "" {
		h.writeError(w, r, http.StatusUnauthorized, errTitleUnauthorized, errMsgLoginRequired)
		return 0, false
	}

	session, err := h.db.GetResearchSession(r.Context(), cookie.Value)
	if err != nil {
		if errors.Is(err, db.ErrResearchSessionNotFound) {
			h.writeError(w, r, http.StatusUnauthorized, errTitleUnauthorized, errMsgLoginRequired)
			return 0, false
		}

		h.logger.Error().Err(err).Msg("get research session failed")
		h.writeError(w, r, http.StatusUnauthorized, errTitleUnauthorized, errMsgLoginRequired)

		return 0, false
	}

	if time.Now().After(session.ExpiresAt) {
		h.writeError(w, r, http.StatusUnauthorized, errTitleUnauthorized, "Session expired.")
		return 0, false
	}

	if !h.isAdmin(session.UserID) {
		h.writeError(w, r, http.StatusUnauthorized, errTitleUnauthorized, "Access denied.")
		return 0, false
	}

	return session.UserID, true
}

func (h *Handler) isAdmin(userID int64) bool {
	for _, adminID := range h.cfg.AdminIDs {
		if adminID == userID {
			return true
		}
	}

	return false
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     researchCookieName,
		Value:    token,
		Path:     researchCookiePath,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   true,
		Expires:  expires,
	})
}

func parseSearchParams(r *http.Request) (db.ResearchSearchParams, string, error) {
	q := r.URL.Query()
	params := db.ResearchSearchParams{
		Query:        strings.TrimSpace(q.Get("q")),
		Channel:      strings.TrimSpace(q.Get("channel")),
		Topic:        strings.TrimSpace(q.Get("topic")),
		Lang:         strings.TrimSpace(q.Get("lang")),
		Limit:        parseLimit(r, defaultSearchLimit),
		Offset:       parseOffset(r),
		IncludeCount: parseBool(q.Get("include_count")),
	}

	from, to, err := parseRange(r)
	if err != nil {
		return params, "", err
	}

	params.From = from
	params.To = to

	scope := strings.ToLower(strings.TrimSpace(q.Get("scope")))
	if scope == "" {
		scope = scopeItems
	}

	switch scope {
	case "items", "evidence", "all":
	default:
		return params, "", fmt.Errorf("%w: %s", errInvalidScope, scope)
	}

	return params, scope, nil
}

func (h *Handler) loadSettingsSnapshot(ctx context.Context) ([]SettingEntry, error) {
	entries := []SettingEntry{}

	relThreshold := h.cfg.RelevanceThreshold
	source := sourceEnv

	if ok, err := h.getSettingValue(ctx, settingRelevanceThreshold, &relThreshold); err != nil {
		return nil, err
	} else if ok {
		source = sourceDB
	}

	entries = append(entries, SettingEntry{
		Name:   settingRelevanceThreshold,
		Value:  fmt.Sprintf(fmtFloat2, relThreshold),
		Source: source,
	})

	impThreshold := h.cfg.ImportanceThreshold
	source = sourceEnv

	if ok, err := h.getSettingValue(ctx, settingImportanceThreshold, &impThreshold); err != nil {
		return nil, err
	} else if ok {
		source = sourceDB
	}

	entries = append(entries, SettingEntry{
		Name:   settingImportanceThreshold,
		Value:  fmt.Sprintf(fmtFloat2, impThreshold),
		Source: source,
	})

	digestLang := ""
	source = sourceDB

	if ok, err := h.getSettingValue(ctx, settingDigestLanguage, &digestLang); err != nil {
		return nil, err
	} else if !ok {
		source = "unset"
	}

	entries = append(entries, SettingEntry{
		Name:   settingDigestLanguage,
		Value:  digestLang,
		Source: source,
	})

	var scheduleRaw json.RawMessage

	source = sourceDB

	if ok, err := h.getSettingValue(ctx, settingDigestSchedule, &scheduleRaw); err != nil {
		return nil, err
	} else if !ok {
		source = sourceEnv
	}

	scheduleValue := strings.TrimSpace(string(scheduleRaw))
	if scheduleValue == "" {
		scheduleValue = fmt.Sprintf("digest_window=%s", h.cfg.DigestWindow)
	}

	entries = append(entries, SettingEntry{
		Name:   settingDigestSchedule,
		Value:  scheduleValue,
		Source: source,
	})

	return entries, nil
}

func (h *Handler) getSettingValue(ctx context.Context, key string, target any) (bool, error) {
	raw, err := h.db.Queries.GetSetting(ctx, key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}

		return false, fmt.Errorf("get setting %s: %w", key, err)
	}

	if err := json.Unmarshal(raw, target); err != nil {
		return false, fmt.Errorf("decode setting %s: %w", key, err)
	}

	return true, nil
}

func parseRange(r *http.Request) (*time.Time, *time.Time, error) {
	q := r.URL.Query()
	fromStr := strings.TrimSpace(q.Get("from"))
	toStr := strings.TrimSpace(q.Get("to"))

	var (
		from *time.Time
		to   *time.Time
		err  error
	)

	if fromStr != "" {
		var t time.Time

		t, err = parseTime(fromStr)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid from: %w", err)
		}

		from = &t
	}

	if toStr != "" {
		var t time.Time

		t, err = parseTime(toStr)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid to: %w", err)
		}

		to = &t
	}

	return from, to, nil
}

func parseRangeRequired(r *http.Request) (time.Time, time.Time, error) {
	from, to, err := parseRange(r)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	if from == nil || to == nil {
		return time.Time{}, time.Time{}, errRangeRequired
	}

	return *from, *to, nil
}

func parseTime(value string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}

	t, err := time.Parse(researchQueryLayout, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse time %q: %w", value, err)
	}

	return t, nil
}

func parseLimit(r *http.Request, fallback int) int {
	val := strings.TrimSpace(r.URL.Query().Get("limit"))
	if val == "" {
		return fallback
	}

	num, err := strconv.Atoi(val)
	if err != nil || num <= 0 {
		return fallback
	}

	if num > maxSearchLimit {
		return maxSearchLimit
	}

	return num
}

func parseOffset(r *http.Request) int {
	val := strings.TrimSpace(r.URL.Query().Get("offset"))
	if val == "" {
		return 0
	}

	num, err := strconv.Atoi(val)
	if err != nil || num < 0 {
		return 0
	}

	return num
}

func parseBool(val string) bool {
	val = strings.ToLower(strings.TrimSpace(val))
	return val == "true" || val == "1" || val == "yes"
}

func wantsHTML(r *http.Request) bool {
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		return true
	}

	return strings.ToLower(r.URL.Query().Get("format")) == "html"
}

func hashSearchParams(params db.ResearchSearchParams, scope string) string {
	payload := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%d|%d|%t",
		scope,
		params.Query,
		params.Channel,
		params.Topic,
		params.Lang,
		formatTimePtr(params.From),
		formatTimePtr(params.To),
		params.Limit,
		params.Offset,
		params.IncludeCount,
	)

	return hashString(payload)
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:6])
}

func formatTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}

	return t.UTC().Format(time.RFC3339)
}

func clampFloat32(value, min, max float32) float32 {
	if value < min {
		return min
	}

	if value > max {
		return max
	}

	return value
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, payload any) int {
	w.Header().Set(contentTypeHeader, contentTypeJSON)
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		h.logger.Error().Err(err).Msg("write json failed")
	}

	return status
}

func (h *Handler) renderHTML(w http.ResponseWriter, name string, data any) error {
	w.Header().Set(contentTypeHeader, contentTypeHTML)
	return h.renderer.Render(w, name, data)
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, status int, title, message string) int {
	if wantsHTML(r) {
		w.Header().Set(contentTypeHeader, contentTypeHTML)
		w.WriteHeader(status)

		if err := h.renderer.Render(w, "error.html", ErrorViewData{
			Title:   title,
			Message: message,
			Status:  status,
		}); err != nil {
			h.logger.Error().Err(err).Msg("failed to render error page")
		}

		return status
	}

	return h.writeJSON(w, status, map[string]string{"error": message})
}

func buildChannelDiffRows(entries []db.ResearchWeeklyChannelDiff) [][]string {
	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, []string{entry.ChannelTitle, strconv.Itoa(entry.Delta)})
	}

	return rows
}

// SearchResponse is the JSON payload for search results.
type SearchResponse struct {
	Items         []db.ResearchItemSearchResult     `json:"items,omitempty"`
	Evidence      []db.ResearchEvidenceSearchResult `json:"evidence,omitempty"`
	ItemCount     *db.ResearchSearchResultCount     `json:"item_count,omitempty"`
	EvidenceCount *db.ResearchSearchResultCount     `json:"evidence_count,omitempty"`
}

// ItemResponse is the JSON payload for item detail.
type ItemResponse struct {
	Item         *db.ItemDebugDetail         `json:"item"`
	Evidence     []db.ItemEvidenceWithSource `json:"evidence"`
	Cluster      *db.ClusterWithItems        `json:"cluster,omitempty"`
	ClusterItems []db.ClusterItemInfo        `json:"cluster_items,omitempty"`
}

// WeeklyDiffResponse is the JSON payload for weekly diff.
type WeeklyDiffResponse struct {
	Topics   []db.ResearchWeeklyDiff        `json:"topics"`
	Channels []db.ResearchWeeklyChannelDiff `json:"channels"`
}

// Template view data structs.
type IndexViewData struct {
	Title string
	Now   time.Time
}

type SearchViewData struct {
	Title         string
	Params        db.ResearchSearchParams
	Scope         string
	Items         []db.ResearchItemSearchResult
	Evidence      []db.ResearchEvidenceSearchResult
	ItemCount     *db.ResearchSearchResultCount
	EvidenceCount *db.ResearchSearchResultCount
}

type ItemViewData struct {
	Title        string
	Item         *db.ItemDebugDetail
	Evidence     []db.ItemEvidenceWithSource
	Cluster      *db.ClusterWithItems
	ClusterItems []db.ClusterItemInfo
	Explain      ItemExplainData
}

type ClusterViewData struct {
	Title   string
	Cluster *db.ResearchClusterDetail
}

type EvidenceViewData struct {
	Title    string
	ItemID   string
	Evidence []db.ItemEvidenceWithSource
}

type TableViewData struct {
	Title            string
	Headers          []string
	Rows             [][]string
	SecondaryTitle   string
	SecondaryHeaders []string
	SecondaryRows    [][]string
	Description      string
}

type SettingEntry struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Source string `json:"source"`
}

type ItemExplainData struct {
	Status              string
	RelevanceScore      float32
	RelevanceThreshold  float32
	RelevancePass       bool
	ImportanceScore     float32
	ImportanceThreshold float32
	ImportancePass      bool
	Gate                *RelevanceGateInfo
}

type RelevanceGateInfo struct {
	Decision    string
	Confidence  float32
	Reason      string
	Model       string
	GateVersion string
}

type ErrorViewData struct {
	Title   string
	Message string
	Status  int
}
