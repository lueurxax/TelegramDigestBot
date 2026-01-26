package expandedview

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/time/rate"

	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

// Rate limiting constants.
const (
	rateLimitRequests = 10
	rateLimitBurst    = 20
	rateLimitWindow   = time.Minute
)

// Log field constants.
const logFieldItemID = "item_id"

// Handler serves expanded item views.
type Handler struct {
	cfg          *config.Config
	tokenService *TokenService
	database     *db.DB
	renderer     *Renderer
	logger       *zerolog.Logger

	// IP-based rate limiting
	limiters   map[string]*rate.Limiter
	limitersMu sync.Mutex
}

// NewHandler creates a new expanded view handler.
func NewHandler(cfg *config.Config, tokenService *TokenService, database *db.DB, logger *zerolog.Logger) (*Handler, error) {
	renderer, err := NewRenderer()
	if err != nil {
		return nil, err
	}

	return &Handler{
		cfg:          cfg,
		tokenService: tokenService,
		database:     database,
		renderer:     renderer,
		logger:       logger,
		limiters:     make(map[string]*rate.Limiter),
	}, nil
}

// ServeHTTP handles requests to /i/{token}.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	defer func() {
		LatencyHistogram.Observe(time.Since(start).Seconds())
	}()

	// Set security headers
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Rate limiting
	clientIP := getClientIP(r)

	if !h.allowRequest(clientIP) {
		h.renderError(w, http.StatusTooManyRequests, "Too Many Requests", "Please wait before trying again.")
		HitsTotal.WithLabelValues(StatusLimited).Inc()
		DeniedTotal.WithLabelValues(ReasonRateLimited).Inc()

		return
	}

	// Extract token from path (path is already stripped of /i/ prefix by StripPrefix)
	token := strings.TrimPrefix(r.URL.Path, "/")

	if token == "" {
		h.renderError(w, http.StatusBadRequest, "Bad Request", "Missing token in URL.")
		HitsTotal.WithLabelValues(StatusDenied).Inc()

		return
	}

	// Verify token
	payload, err := h.tokenService.Verify(token)
	if err != nil {
		h.handleTokenError(w, err)

		return
	}

	// Check admin status if required
	// UserID = 0 indicates a system-generated token (e.g., from digests) - allow for any viewer
	if h.cfg.ExpandedViewRequireAdmin && payload.UserID != 0 && !h.isAdmin(payload.UserID) {
		h.renderError(w, http.StatusUnauthorized, "Unauthorized", "You don't have permission to view this page.")
		HitsTotal.WithLabelValues(StatusDenied).Inc()
		DeniedTotal.WithLabelValues(ReasonNotAdmin).Inc()
		h.logger.Warn().Int64("user_id", payload.UserID).Msg("Non-admin attempted expanded view access")

		return
	}

	// Fetch data and render
	h.serveExpandedView(r.Context(), w, payload.ItemID)
}

func (h *Handler) handleTokenError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrTokenExpired):
		h.renderError(w, http.StatusGone, "Link Expired", "This link has expired. Please request a new one from the digest.")
		HitsTotal.WithLabelValues(StatusExpired).Inc()
		DeniedTotal.WithLabelValues(ReasonExpired).Inc()
	default:
		h.renderError(w, http.StatusUnauthorized, "Invalid Link", "This link is invalid or has been tampered with.")
		HitsTotal.WithLabelValues(StatusDenied).Inc()
		DeniedTotal.WithLabelValues(ReasonInvalidToken).Inc()
	}
}

func (h *Handler) serveExpandedView(ctx context.Context, w http.ResponseWriter, itemID string) {
	// Fetch item details
	item, err := h.database.GetItemDebugDetail(ctx, itemID)
	if err != nil {
		h.logger.Error().Err(err).Str(logFieldItemID, itemID).Msg("Failed to fetch item")
		h.renderError(w, http.StatusInternalServerError, "Error", "Failed to load item data.")
		HitsTotal.WithLabelValues(StatusError).Inc()
		ErrorsTotal.WithLabelValues(ErrorTypeDB).Inc()

		return
	}

	if item == nil {
		h.renderError(w, http.StatusNotFound, "Not Found", "This item no longer exists.")
		HitsTotal.WithLabelValues(StatusNotFound).Inc()

		return
	}

	// Fetch evidence
	evidenceMap, err := h.database.GetEvidenceForItems(ctx, []string{itemID})
	if err != nil {
		h.logger.Error().Err(err).Str(logFieldItemID, itemID).Msg("Failed to fetch evidence")
		// Continue without evidence - it's not critical
	}

	evidence := evidenceMap[itemID]

	// Fetch cluster context
	var clusterItems []ClusterItemView

	_, clusterInfo, err := h.database.GetClusterForItem(ctx, itemID)
	if err != nil {
		h.logger.Debug().Err(err).Str(logFieldItemID, itemID).Msg("Failed to fetch cluster context")
		// Continue without cluster - it's not critical
	}

	for _, ci := range clusterInfo {
		clusterItems = append(clusterItems, ClusterItemView{
			ID:              ci.ID,
			Summary:         ci.Summary,
			ChannelUsername: ci.ChannelUsername,
		})
	}

	// Build ChatGPT link
	chatGPTLink := BuildChatGPTLink(item, evidence)

	// Render
	data := &ExpandedViewData{
		Item:         item,
		Evidence:     evidence,
		ClusterItems: clusterItems,
		ChatGPTLink:  chatGPTLink,
		GeneratedAt:  time.Now(),
	}

	if err := h.renderer.RenderExpanded(w, data); err != nil {
		h.logger.Error().Err(err).Str(logFieldItemID, itemID).Msg("Failed to render expanded view")
		// Can't render error page since we already started writing
		ErrorsTotal.WithLabelValues(ErrorTypeRender).Inc()

		return
	}

	HitsTotal.WithLabelValues(StatusOK).Inc()
}

func (h *Handler) renderError(w http.ResponseWriter, code int, title, message string) {
	w.WriteHeader(code)

	if err := h.renderer.RenderError(w, &ErrorData{
		Code:    code,
		Title:   title,
		Message: message,
	}); err != nil {
		h.logger.Error().Err(err).Msg("Failed to render error page")
	}
}

func (h *Handler) isAdmin(userID int64) bool {
	for _, adminID := range h.cfg.AdminIDs {
		if adminID == userID {
			return true
		}
	}

	return false
}

func (h *Handler) allowRequest(ip string) bool {
	h.limitersMu.Lock()

	limiter, ok := h.limiters[ip]
	if !ok {
		limiter = rate.NewLimiter(rate.Every(rateLimitWindow/rateLimitRequests), rateLimitBurst)
		h.limiters[ip] = limiter
	}

	h.limitersMu.Unlock()

	return limiter.Allow()
}

func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (common with reverse proxies)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	return r.RemoteAddr
}
