package factcheck

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
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
)

type Repository interface {
	ClaimNextFactCheck(ctx context.Context) (*db.FactCheckQueueItem, error)
	UpdateFactCheckStatus(ctx context.Context, queueID, status, errMsg string, retryAt *time.Time) error
	GetFactCheckCache(ctx context.Context, normalizedClaim string) (*db.FactCheckCacheEntry, error)
	SaveFactCheckCache(ctx context.Context, normalizedClaim string, payload []byte, cachedAt time.Time) error
	SaveItemFactChecks(ctx context.Context, itemID string, matches []db.FactCheckMatch) error
	DeleteFactCheckCacheBefore(ctx context.Context, cutoff time.Time) (int64, error)
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

	pollInterval, err := time.ParseDuration(w.cfg.WorkerPollInterval)
	if err != nil {
		w.logger.Error().Err(err).Str("interval", w.cfg.WorkerPollInterval).Msg("invalid worker poll interval, using 10s")

		pollInterval = defaultFactCheckPollInterval
	}

	lastCleanup := time.Now()

	for {
		item, err := w.db.ClaimNextFactCheck(ctx)
		if err != nil {
			w.logger.Error().Err(err).Msg("failed to claim fact check item")
		} else if item != nil {
			w.processItem(ctx, item)
		}

		if time.Since(lastCleanup) >= defaultFactCheckCleanupInterval {
			w.cleanupCache(ctx)

			lastCleanup = time.Now()
		}

		select {
		case <-ctx.Done():
			return ctx.Err() //nolint:wrapcheck
		case <-time.After(pollInterval):
		}
	}
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
	if len(item.Claim) >= w.factCheckMinLength() || item.RawMessageID == "" {
		return
	}

	links, err := w.db.GetLinksForMessage(ctx, item.RawMessageID)
	if err != nil || len(links) == 0 {
		return
	}

	// Extract from first link's title or content (headline/lead)
	// Simple heuristic: top 1-2 factual sentences from ResolvedLink.Content.
	extracted := links[0].Title
	if links[0].Content != "" {
		sentences := strings.Split(links[0].Content, ".")
		if len(sentences) > 0 {
			extracted += ": " + strings.TrimSpace(sentences[0])
		}
	}

	if len(extracted) >= w.factCheckMinLength() {
		item.Claim = extracted
		// Re-normalizing if needed (NormalizeClaim is in factcheck package)
		item.NormalizedClaim = NormalizeClaim(extracted)
	}
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
