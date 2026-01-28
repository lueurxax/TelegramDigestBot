package research

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

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

	// Route path constants.
	routeLogin    = "login"
	routeSearch   = "search"
	routeItem     = "item/"
	routeCluster  = "cluster/"
	routeEvidence = "evidence/"
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
)

// Static errors for err113 compliance.
var (
	errInvalidScope  = errors.New("invalid scope")
	errRangeRequired = errors.New("from and to are required")
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
		return h.writeError(w, r, http.StatusBadRequest, "Invalid Parameters", err.Error()), 0
	}

	items, itemCount, evidence, evCount, errStatus := h.executeSearch(r, params, scope)
	if errStatus != 0 {
		return errStatus, 0
	}

	return h.renderSearchResults(w, r, params, scope, items, itemCount, evidence, evCount)
}

// executeSearch performs the actual search based on scope.
func (h *Handler) executeSearch(r *http.Request, params db.ResearchSearchParams, scope string) (
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
		items, itemCount, err = h.db.SearchResearchItems(r.Context(), params)
		if err != nil {
			h.logger.Error().Err(err).Msg("search research items failed")
			return nil, nil, nil, nil, http.StatusInternalServerError
		}
	}

	if scope == scopeEvidence || scope == scopeAll {
		evidence, evCount, err = h.db.SearchResearchEvidence(r.Context(), params)
		if err != nil {
			h.logger.Error().Err(err).Msg("search research evidence failed")
			return nil, nil, nil, nil, http.StatusInternalServerError
		}
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

	if wantsHTML(r) {
		data := ItemViewData{
			Title:        "Item Details",
			Item:         item,
			Evidence:     evidence,
			Cluster:      cluster,
			ClusterItems: clusterItems,
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

		data := TableViewData{
			Title:   "Channel Overlap",
			Headers: []string{"Channel A", "Channel B", "Shared", "Jaccard"},
			Rows:    rows,
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

	bucket := r.URL.Query().Get("bucket")
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

		data := TableViewData{
			Title:   "Claim Ledger",
			Headers: []string{"ID", "Claim", "First Seen"},
			Rows:    rows,
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

type ErrorViewData struct {
	Title   string
	Message string
	Status  int
}
