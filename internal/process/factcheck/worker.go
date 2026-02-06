// Package factcheck implements fact-checking via Google Fact Check API.
//
// The Worker processes items from the fact-check queue, querying the
// Google Fact Check Tools API to find existing fact-checks for claims.
// Results are cached to avoid repeated API calls for similar claims.
package factcheck

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	linkscore "github.com/lueurxax/telegram-digest-bot/internal/core/links"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/observability"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const (
	maxFactCheckAttempts            = 3
	defaultRetryDelay               = 10 * time.Minute
	defaultFactCheckCacheTTL        = 48 * time.Hour
	defaultFactCheckPollInterval    = 10 * time.Second
	defaultFactCheckCleanupInterval = 6 * time.Hour
	// defaultFactCheckItemTimeout is the maximum time to process a single item.
	defaultFactCheckItemTimeout = 60 * time.Second
	// stuckFactCheckThreshold is when a "processing" item is considered stuck.
	// Set to 2x item timeout to allow for retries before considering stuck.
	stuckFactCheckThreshold = 2 * defaultFactCheckItemTimeout
	// recoveryInterval is how often to check for and recover stuck items.
	recoveryInterval = 5 * time.Minute
)

type Repository interface {
	ClaimNextFactCheck(ctx context.Context) (*db.FactCheckQueueItem, error)
	UpdateFactCheckStatus(ctx context.Context, queueID, status, errMsg string, retryAt *time.Time) error
	GetFactCheckCache(ctx context.Context, normalizedClaim string) (*db.FactCheckCacheEntry, error)
	SaveFactCheckCache(ctx context.Context, normalizedClaim string, payload []byte, cachedAt time.Time) error
	SaveItemFactChecks(ctx context.Context, itemID string, matches []db.FactCheckMatch) error
	DeleteFactCheckCacheBefore(ctx context.Context, cutoff time.Time) (int64, error)
	RecoverStuckFactCheckItems(ctx context.Context, stuckThreshold time.Duration) (int64, error)
	GetLinksForMessage(ctx context.Context, msgID string) ([]domain.ResolvedLink, error)
}

type Worker struct {
	cfg    *config.Config
	db     Repository
	client *GoogleClient
	logger *zerolog.Logger
}

func NewWorker(cfg *config.Config, database Repository, logger *zerolog.Logger) *Worker {
	client := NewGoogleClient(cfg.FactCheckGoogleAPIKey, cfg.FactCheckGoogleRPM, cfg.FactCheckGoogleMaxResults)

	return &Worker{
		cfg:    cfg,
		db:     database,
		client: client,
		logger: logger,
	}
}

func (w *Worker) Run(ctx context.Context) error {
	if !w.cfg.FactCheckGoogleEnabled || w.cfg.FactCheckGoogleAPIKey == "" {
		w.logger.Info().Msg("fact check worker disabled")
		return nil
	}

	return w.runLoop(ctx, w.parsePollInterval())
}

func (w *Worker) parsePollInterval() time.Duration {
	pollInterval, err := time.ParseDuration(w.cfg.WorkerPollInterval)
	if err != nil {
		w.logger.Error().Err(err).Str("interval", w.cfg.WorkerPollInterval).Msg("invalid worker poll interval, using 10s")

		return defaultFactCheckPollInterval
	}

	return pollInterval
}

func (w *Worker) runLoop(ctx context.Context, pollInterval time.Duration) error {
	lastCleanup := time.Now()
	lastRecovery := time.Now()

	for {
		w.runPeriodicTasks(ctx, &lastRecovery, &lastCleanup)
		w.processNextItem(ctx)

		select {
		case <-ctx.Done():
			return ctx.Err() //nolint:wrapcheck
		case <-time.After(pollInterval):
		}
	}
}

func (w *Worker) runPeriodicTasks(ctx context.Context, lastRecovery, lastCleanup *time.Time) {
	if time.Since(*lastRecovery) >= recoveryInterval {
		w.recoverStuckItems(ctx)

		*lastRecovery = time.Now()
	}

	if time.Since(*lastCleanup) >= defaultFactCheckCleanupInterval {
		w.cleanupCache(ctx)

		*lastCleanup = time.Now()
	}
}

func (w *Worker) processNextItem(ctx context.Context) {
	item, err := w.db.ClaimNextFactCheck(ctx)
	if err != nil {
		w.logger.Error().Err(err).Msg("failed to claim fact check item")
		return
	}

	if item != nil {
		w.processItemWithTimeout(ctx, item)
	}
}

// recoverStuckItems recovers items that were claimed but never completed.
func (w *Worker) recoverStuckItems(ctx context.Context) {
	recovered, err := w.db.RecoverStuckFactCheckItems(ctx, stuckFactCheckThreshold)
	if err != nil {
		w.logger.Error().Err(err).Msg("failed to recover stuck fact check items")
		return
	}

	if recovered > 0 {
		w.logger.Info().Int64("recovered", recovered).Msg("recovered stuck fact check items")
	}
}

// processItemWithTimeout wraps processItem with a per-item timeout.
func (w *Worker) processItemWithTimeout(ctx context.Context, item *db.FactCheckQueueItem) {
	itemCtx, cancel := context.WithTimeout(ctx, defaultFactCheckItemTimeout)
	defer cancel()

	w.processItem(itemCtx, item)
}

func (w *Worker) processItem(ctx context.Context, item *db.FactCheckQueueItem) {
	w.enrichClaimFromLinks(ctx, item)

	if w.shouldSkipClaim(ctx, item) {
		return
	}

	cacheTTL := w.factCheckCacheTTL()
	if w.processFromCache(ctx, item, cacheTTL) {
		return
	}

	if err := w.processWithProvider(ctx, item); err != nil {
		w.handleError(ctx, item, err)
		return
	}

	w.updateStatus(ctx, item.ID, db.FactCheckStatusDone, "", nil)
}

func (w *Worker) enrichClaimFromLinks(ctx context.Context, item *db.FactCheckQueueItem) {
	// If claim is short, try to extract from link context
	if !w.shouldEnrichClaim(item) {
		return
	}

	links, err := w.db.GetLinksForMessage(ctx, item.RawMessageID)
	if err != nil || len(links) == 0 {
		return
	}

	links = w.filterLinksForFactCheck(item, links)
	if len(links) == 0 {
		return
	}

	extracted := w.extractClaimFromLink(links[0])
	if len(extracted) >= w.factCheckMinLength() {
		item.Claim = extracted
		// Re-normalizing if needed (NormalizeClaim is in factcheck package)
		item.NormalizedClaim = NormalizeClaim(extracted)
	}
}

func (w *Worker) filterLinksForFactCheck(item *db.FactCheckQueueItem, links []domain.ResolvedLink) []domain.ResolvedLink {
	if len(links) == 0 {
		return links
	}

	msgLang := linkscore.DetectLanguage(item.Claim)
	filtered := make([]domain.ResolvedLink, 0, len(links))

	for _, link := range links {
		if len(strings.Fields(link.Content)) < w.cfg.LinkMinWords {
			continue
		}

		if msgLang != "" && link.Language != "" && msgLang != link.Language {
			continue
		}

		filtered = append(filtered, link)
	}

	return filtered
}

func (w *Worker) shouldEnrichClaim(item *db.FactCheckQueueItem) bool {
	if len(item.Claim) >= w.factCheckMinLength() || item.RawMessageID == "" {
		return false
	}

	return strings.Contains(w.cfg.LinkEnrichmentScope, domain.ScopeFactCheck)
}

func (w *Worker) extractClaimFromLink(link domain.ResolvedLink) string {
	extracted := link.Title
	if link.Content == "" {
		return extracted
	}

	sentences := strings.Split(link.Content, ".")
	if len(sentences) == 0 {
		return extracted
	}

	sentencePart := strings.TrimSpace(sentences[0])
	if len(sentences) > 1 && len(sentencePart) < 100 { // Add second sentence if first is short
		sentencePart += ". " + strings.TrimSpace(sentences[1])
	}

	if extracted != "" {
		return extracted + ": " + sentencePart
	}

	return sentencePart
}

func (w *Worker) shouldSkipClaim(ctx context.Context, item *db.FactCheckQueueItem) bool {
	if len(item.Claim) >= w.factCheckMinLength() {
		return false
	}

	w.updateStatus(ctx, item.ID, db.FactCheckStatusDone, "", nil)

	return true
}

func (w *Worker) factCheckMinLength() int {
	minLen := w.cfg.FactCheckMinClaimLength
	if minLen <= 0 {
		minLen = DefaultMinClaimLength
	}

	return minLen
}

func (w *Worker) factCheckCacheTTL() time.Duration {
	cacheTTL := time.Duration(w.cfg.FactCheckCacheTTLHours) * time.Hour
	if cacheTTL <= 0 {
		cacheTTL = defaultFactCheckCacheTTL
	}

	return cacheTTL
}

func (w *Worker) processFromCache(ctx context.Context, item *db.FactCheckQueueItem, cacheTTL time.Duration) bool {
	entry, err := w.db.GetFactCheckCache(ctx, item.NormalizedClaim)
	if err != nil {
		w.logger.Warn().Err(err).Msg("fact check cache lookup failed")
		return false
	}

	if entry == nil {
		observability.FactCheckCacheMisses.Inc()
		return false
	}

	if time.Since(entry.CachedAt) > cacheTTL {
		observability.FactCheckCacheMisses.Inc()
		return false
	}

	observability.FactCheckCacheHits.Inc()

	if err := w.saveMatchesFromCache(ctx, item, entry); err != nil {
		w.logger.Warn().Err(err).Msg("failed to save cached fact check matches")
		return false
	}

	w.updateStatus(ctx, item.ID, db.FactCheckStatusDone, "", nil)

	return true
}

func (w *Worker) processWithProvider(ctx context.Context, item *db.FactCheckQueueItem) error {
	observability.FactCheckCacheMisses.Inc()

	start := time.Now()
	results, payload, err := w.client.Search(ctx, item.Claim)

	observability.FactCheckRequestDuration.Observe(time.Since(start).Seconds())

	if err != nil {
		observability.FactCheckRequests.WithLabelValues("error").Inc()

		return err
	}

	observability.FactCheckRequests.WithLabelValues("success").Inc()

	if len(payload) > 0 {
		if err := w.db.SaveFactCheckCache(ctx, item.NormalizedClaim, payload, time.Now()); err != nil {
			w.logger.Warn().Err(err).Msg("failed to save fact check cache")
		}
	}

	if err := w.saveMatches(ctx, item.ItemID, results); err != nil {
		return err
	}

	return nil
}

func (w *Worker) saveMatchesFromCache(ctx context.Context, item *db.FactCheckQueueItem, entry *db.FactCheckCacheEntry) error {
	results, err := ParseGoogleResults(entry.ResultJSON, item.Claim, w.cfg.FactCheckGoogleMaxResults)
	if err != nil {
		w.logger.Warn().Err(err).Msg("failed to parse cached fact check payload")
		return err
	}

	return w.saveMatches(ctx, item.ItemID, results)
}

func (w *Worker) saveMatches(ctx context.Context, itemID string, results []Result) error {
	if len(results) == 0 {
		return nil
	}

	matches := make([]db.FactCheckMatch, 0, len(results))
	now := time.Now()

	for _, res := range results {
		matches = append(matches, db.FactCheckMatch{
			ItemID:    itemID,
			Claim:     res.Claim,
			URL:       res.URL,
			Publisher: res.Publisher,
			Rating:    res.Rating,
			MatchedAt: now,
		})
	}

	if err := w.db.SaveItemFactChecks(ctx, itemID, matches); err != nil {
		return fmt.Errorf("save item fact checks: %w", err)
	}

	observability.FactCheckMatches.Inc()

	return nil
}

func (w *Worker) handleError(ctx context.Context, item *db.FactCheckQueueItem, err error) {
	if item.AttemptCount >= maxFactCheckAttempts {
		w.updateStatus(ctx, item.ID, db.FactCheckStatusError, err.Error(), nil)
		return
	}

	retryAt := time.Now().Add(defaultRetryDelay)
	w.updateStatus(ctx, item.ID, db.FactCheckStatusPending, err.Error(), &retryAt)
}

func (w *Worker) updateStatus(ctx context.Context, queueID, status, errMsg string, retryAt *time.Time) {
	if err := w.db.UpdateFactCheckStatus(ctx, queueID, status, errMsg, retryAt); err != nil {
		w.logger.Warn().Err(err).Msg("failed to update fact check status")
	}
}

func (w *Worker) cleanupCache(ctx context.Context) {
	cacheTTL := time.Duration(w.cfg.FactCheckCacheTTLHours) * time.Hour
	if cacheTTL <= 0 {
		cacheTTL = defaultFactCheckCacheTTL
	}

	cutoff := time.Now().Add(-cacheTTL)

	deleted, err := w.db.DeleteFactCheckCacheBefore(ctx, cutoff)
	if err != nil {
		w.logger.Warn().Err(err).Msg("failed to clean fact check cache")
		return
	}

	if deleted > 0 {
		w.logger.Info().Int64("deleted", deleted).Msg("cleaned fact check cache")
	}
}
