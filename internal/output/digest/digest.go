package digest

import (
	"context"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/htmlutils"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/observability"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/schedule"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

var topicEmojis = map[string]string{
	"Technology":    "💻",
	"Finance":       "💰",
	"Politics":      "⚖️",
	"Sports":        "🏆",
	"Entertainment": "🎬",
	"Science":       "🔬",
	"Health":        "🏥",
	"Business":      "📊",
	"World News":    "🌍",
	"Local News":    "📍",
	"Culture":       "🎨",
	"Education":     "📚",
	"Humor":         "😂",
}

// RichDigestItem represents a digest item with media for inline display.
type RichDigestItem struct {
	Summary    string
	Topic      string
	Importance float32
	Channel    string
	ChannelID  int64
	MsgID      int64
	MediaData  []byte
}

// RichDigestContent holds content for inline image digest display.
type RichDigestContent struct {
	Header   string
	Items    []RichDigestItem
	DigestID string
}

type DigestPoster interface {
	SendDigest(ctx context.Context, chatID int64, text string, digestID string) (int64, error)
	SendDigestWithImage(ctx context.Context, chatID int64, text string, digestID string, imageData []byte) (int64, error)
	SendRichDigest(ctx context.Context, chatID int64, content RichDigestContent) (int64, error)
	SendNotification(ctx context.Context, text string) error
}

type Scheduler struct {
	cfg       *config.Config
	database  Repository
	bot       DigestPoster
	llmClient llm.Client
	logger    *zerolog.Logger
}

func New(cfg *config.Config, database Repository, bot DigestPoster, llmClient llm.Client, logger *zerolog.Logger) *Scheduler {
	return &Scheduler{
		cfg:       cfg,
		database:  database,
		bot:       bot,
		llmClient: llmClient,
		logger:    logger,
	}
}

func (s *Scheduler) getLockID() int64 {
	// Simple hash of the lease name to an int64 for Postgres advisory lock
	var h int64

	for _, c := range s.cfg.LeaderElectionLeaseName {
		h = HashMultiplier*h + int64(c)
	}

	return h
}

func (s *Scheduler) Run(ctx context.Context) error {
	s.logger.Info().Msg("Starting digest scheduler")

	// Run once immediately
	s.runOnceWithLock(ctx)

	tickInterval, err := time.ParseDuration(s.cfg.SchedulerTickInterval)
	if err != nil {
		s.logger.Error().Err(err).Str("interval", s.cfg.SchedulerTickInterval).Msg("invalid scheduler tick interval, using 10m")

		tickInterval = DefaultTickIntervalMinutes * time.Minute
	}

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	// Auto-weight update ticker (check every hour, run weekly)
	autoWeightTicker := time.NewTicker(time.Hour)
	defer autoWeightTicker.Stop()

	var (
		lastAutoWeightRun    time.Time
		lastAutoRelevanceRun time.Time
		lastThresholdRun     time.Time
		lastRatingStatsRun   time.Time
	)

	for { // select loop immediately follows declarations
		select {
		case <-ctx.Done():
			return fmt.Errorf("context canceled: %w", ctx.Err())
		case <-ticker.C:
			s.runOnceWithLock(ctx)
		case <-autoWeightTicker.C:
			s.maybeRunAutoWeightUpdate(ctx, &lastAutoWeightRun)
			s.maybeRunAutoRelevanceUpdate(ctx, &lastAutoRelevanceRun)
			s.maybeRunThresholdTuning(ctx, &lastThresholdRun)
			s.maybeRunRatingStatsUpdate(ctx, &lastRatingStatsRun)
		}
	}
}

// maybeRunAutoWeightUpdate checks if it's time for weekly auto-weight update
func (s *Scheduler) maybeRunAutoWeightUpdate(ctx context.Context, lastRun *time.Time) {
	// Check if auto-weight is enabled
	var autoWeightEnabled = true
	if err := s.database.GetSetting(ctx, "auto_weight_enabled", &autoWeightEnabled); err != nil {
		s.logger.Debug().Err(err).Msg("auto_weight_enabled not set, defaulting to true")
	}

	if !autoWeightEnabled {
		return
	}

	now := time.Now()

	// Run on Sundays at midnight (or if never run and it's past Sunday)
	isSunday := now.Weekday() == time.Sunday
	isMidnightHour := now.Hour() == 0
	notRunThisWeek := lastRun.IsZero() || now.Sub(*lastRun) > 6*HoursPerDay*time.Hour

	if isSunday && isMidnightHour && notRunThisWeek {
		logger := s.logger.With().Str(LogFieldTask, "auto-weight").Logger()
		logger.Info().Msg("Starting weekly auto-weight update")

		if err := s.UpdateAutoWeights(ctx, &logger); err != nil {
			logger.Error().Err(err).Msg("failed to update auto-weights")
		} else {
			*lastRun = now
		}
	}
}

func (s *Scheduler) maybeRunAutoRelevanceUpdate(ctx context.Context, lastRun *time.Time) {
	autoRelevanceEnabled := true
	if err := s.database.GetSetting(ctx, "auto_relevance_enabled", &autoRelevanceEnabled); err != nil {
		s.logger.Debug().Err(err).Msg("auto_relevance_enabled not set, defaulting to true")
	}

	if !autoRelevanceEnabled {
		return
	}

	now := time.Now()
	isSunday := now.Weekday() == time.Sunday
	isMidnightHour := now.Hour() == 0
	notRunThisWeek := lastRun.IsZero() || now.Sub(*lastRun) > 6*HoursPerDay*time.Hour

	if isSunday && isMidnightHour && notRunThisWeek {
		logger := s.logger.With().Str(LogFieldTask, "auto-relevance").Logger()
		logger.Info().Msg("Starting weekly auto-relevance update")

		if err := s.UpdateAutoRelevance(ctx, &logger); err != nil {
			logger.Error().Err(err).Msg("failed to update auto-relevance deltas")
		} else {
			*lastRun = now
		}
	}
}

func (s *Scheduler) maybeRunThresholdTuning(ctx context.Context, lastRun *time.Time) {
	now := time.Now()
	isSunday := now.Weekday() == time.Sunday
	isMidnightHour := now.Hour() == 0
	notRunThisWeek := lastRun.IsZero() || now.Sub(*lastRun) > 6*HoursPerDay*time.Hour

	if isSunday && isMidnightHour && notRunThisWeek {
		logger := s.logger.With().Str(LogFieldTask, "threshold-tuning").Logger()
		logger.Info().Msg("Starting weekly threshold tuning")

		if err := s.UpdateGlobalThresholds(ctx, &logger); err != nil {
			logger.Error().Err(err).Msg("failed to update global thresholds")
		} else {
			*lastRun = now
		}
	}
}

func (s *Scheduler) maybeRunRatingStatsUpdate(ctx context.Context, lastRun *time.Time) {
	now := time.Now()
	isSunday := now.Weekday() == time.Sunday
	isMidnightHour := now.Hour() == 0
	notRunThisWeek := lastRun.IsZero() || now.Sub(*lastRun) > 6*HoursPerDay*time.Hour

	if isSunday && isMidnightHour && notRunThisWeek {
		logger := s.logger.With().Str(LogFieldTask, "rating-stats").Logger()
		logger.Info().Msg("Starting weekly rating stats aggregation")

		if err := s.UpdateRatingStats(ctx, &logger); err != nil {
			logger.Error().Err(err).Msg("failed to update rating stats")
		} else {
			*lastRun = now
		}
	}
}

func (s *Scheduler) runOnceWithLock(ctx context.Context) {
	correlationID := uuid.New().String()
	logger := s.logger.With().Str(LogFieldCorrelationID, correlationID).Logger()
	logger.Info().Msg("Starting digest check")

	if !s.cfg.LeaderElectionEnabled {
		if err := s.processDigest(ctx, &logger); err != nil {
			logger.Error().Err(err).Msg(MsgFailedToProcessDigest)
		}

		return
	}

	lockID := s.getLockID()

	acquired, err := s.database.TryAcquireAdvisoryLock(ctx, lockID)
	if err != nil {
		logger.Error().Err(err).Msg("failed to acquire lock")
		return
	}

	if !acquired {
		logger.Debug().Msg("did not acquire lock, skipping")

		return
	}

	defer func() {
		if err := s.database.ReleaseAdvisoryLock(ctx, lockID); err != nil {
			logger.Error().Err(err).Msg("failed to release lock")
		}
	}()

	if err := s.processDigest(ctx, &logger); err != nil {
		logger.Error().Err(err).Msg(MsgFailedToProcessDigest)
	}
}

func (s *Scheduler) RunOnce(ctx context.Context) error {
	correlationID := uuid.New().String()
	logger := s.logger.With().Str(LogFieldCorrelationID, correlationID).Logger()
	logger.Info().Msg("Starting single digest run")

	if !s.cfg.LeaderElectionEnabled {
		return s.processDigest(ctx, &logger)
	}

	lockID := s.getLockID()

	acquired, err := s.database.TryAcquireAdvisoryLock(ctx, lockID)
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}

	if !acquired {
		logger.Info().Msg("did not acquire lock, another instance is probably running. Skipping RunOnce.")
		return nil
	}

	defer func() {
		if err := s.database.ReleaseAdvisoryLock(ctx, lockID); err != nil {
			logger.Warn().Err(err).Msg("failed to release advisory lock")
		}
	}()

	return s.processDigest(ctx, &logger)
}

// anomalyInfo tracks empty window anomalies for consolidated reporting
type anomalyInfo struct {
	start       time.Time
	end         time.Time
	totalItems  int
	readyItems  int
	threshold   float32
	isBacklog   bool
	backlogSize int
}

type digestProcessConfig struct {
	window                      time.Duration
	targetChatID                int64
	importanceThreshold         float32
	catchupWindow               time.Duration
	anomalyNotificationsEnabled bool
	schedule                    *schedule.Schedule
	scheduleAnchor              time.Time
}

func (s *Scheduler) loadDigestProcessConfig(ctx context.Context, logger *zerolog.Logger) digestProcessConfig {
	cfg := digestProcessConfig{
		window:                      time.Hour,
		targetChatID:                s.cfg.TargetChatID,
		importanceThreshold:         s.cfg.ImportanceThreshold,
		catchupWindow:               DefaultCatchupWindowHours * time.Hour,
		anomalyNotificationsEnabled: true,
	}

	s.loadWindowConfig(ctx, &cfg, logger)
	s.loadThresholdConfig(ctx, &cfg, logger)
	s.loadScheduleConfig(ctx, &cfg, logger)

	return cfg
}

func (s *Scheduler) loadWindowConfig(ctx context.Context, cfg *digestProcessConfig, logger *zerolog.Logger) {
	windowStr := s.cfg.DigestWindow
	if err := s.database.GetSetting(ctx, "digest_window", &windowStr); err != nil {
		logger.Debug().Err(err).Msg("could not get digest_window from DB, using default")
	}

	if w, err := time.ParseDuration(windowStr); err == nil {
		cfg.window = w
	} else {
		logger.Error().Err(err).Str(LogFieldWindow, windowStr).Msg("invalid digest window duration, using 1h")
	}

	if cw, err := time.ParseDuration(s.cfg.SchedulerCatchupWindow); err == nil {
		cfg.catchupWindow = cw
	} else {
		logger.Error().Err(err).Str(LogFieldWindow, s.cfg.SchedulerCatchupWindow).Msg("invalid scheduler catchup window, using 24h")
	}
}

func (s *Scheduler) loadThresholdConfig(ctx context.Context, cfg *digestProcessConfig, logger *zerolog.Logger) {
	if err := s.database.GetSetting(ctx, SettingTargetChatID, &cfg.targetChatID); err != nil {
		logger.Debug().Err(err).Msg("could not get target_chat_id from DB, using default")
	}

	if err := s.database.GetSetting(ctx, SettingImportanceThreshold, &cfg.importanceThreshold); err != nil {
		logger.Debug().Err(err).Msg("could not get importance_threshold from DB, using default")
	}

	if err := s.database.GetSetting(ctx, "anomaly_notifications", &cfg.anomalyNotificationsEnabled); err != nil {
		logger.Debug().Err(err).Msg("could not get anomaly_notifications from DB, defaulting to enabled")
	}
}

func (s *Scheduler) loadScheduleConfig(ctx context.Context, cfg *digestProcessConfig, logger *zerolog.Logger) {
	var sched schedule.Schedule
	if err := s.database.GetSetting(ctx, schedule.SettingDigestSchedule, &sched); err != nil {
		logger.Debug().Err(err).Msg("could not get digest_schedule from DB")

		return
	}

	if sched.IsEmpty() {
		return
	}

	if err := sched.Validate(); err != nil {
		logger.Warn().Err(err).Msg("invalid digest schedule, falling back to digest_window")

		return
	}

	cfg.schedule = &sched

	if err := s.database.GetSetting(ctx, schedule.SettingDigestScheduleAnchor, &cfg.scheduleAnchor); err != nil {
		logger.Debug().Err(err).Msg("could not get digest_schedule_anchor from DB")
	}
}

func (s *Scheduler) processDigest(ctx context.Context, logger *zerolog.Logger) error {
	cfg := s.loadDigestProcessConfig(ctx, logger)
	now := time.Now()

	var anomalies []anomalyInfo

	var err error

	if cfg.schedule != nil {
		anomalies, err = s.processScheduledDigest(ctx, cfg, now, logger)
	} else {
		anomalies, err = s.processLegacyDigest(ctx, cfg, now, logger)
	}

	if err != nil {
		return err
	}

	if cfg.anomalyNotificationsEnabled && len(anomalies) > 0 {
		s.sendConsolidatedAnomalyNotification(ctx, anomalies, cfg.importanceThreshold, logger)
	}

	return nil
}

func (s *Scheduler) processScheduledDigest(ctx context.Context, cfg digestProcessConfig, now time.Time, logger *zerolog.Logger) ([]anomalyInfo, error) {
	windows, err := s.buildScheduledWindows(cfg, now, logger)
	if err != nil {
		return nil, err
	}

	var anomalies []anomalyInfo

	for _, window := range windows {
		anomaly, err := s.processWindow(ctx, window.start, window.end, cfg.targetChatID, cfg.importanceThreshold, logger)
		if err != nil {
			logger.Error().Err(err).
				Time(LogFieldStart, window.start).
				Time(LogFieldEnd, window.end).
				Int64(SettingTargetChatID, cfg.targetChatID).
				Msg(msgFailedToProcessWindow)
		}

		if anomaly != nil {
			anomalies = append(anomalies, *anomaly)
		}
	}

	return anomalies, nil
}

func (s *Scheduler) processLegacyDigest(ctx context.Context, cfg digestProcessConfig, now time.Time, logger *zerolog.Logger) ([]anomalyInfo, error) {
	now = now.Truncate(cfg.window)

	var anomalies []anomalyInfo

	for t := now.Add(-cfg.catchupWindow); !t.After(now.Add(-cfg.window)); t = t.Add(cfg.window) {
		start := t
		end := t.Add(cfg.window)

		anomaly, err := s.processWindow(ctx, start, end, cfg.targetChatID, cfg.importanceThreshold, logger)
		if err != nil {
			logger.Error().Err(err).
				Time(LogFieldStart, start).
				Time(LogFieldEnd, end).
				Int64(SettingTargetChatID, cfg.targetChatID).
				Msg(msgFailedToProcessWindow)
		}

		if anomaly != nil {
			anomalies = append(anomalies, *anomaly)
		}
	}

	return anomalies, nil
}

type scheduleWindow struct {
	start time.Time
	end   time.Time
}

func (s *Scheduler) buildScheduledWindows(cfg digestProcessConfig, now time.Time, logger *zerolog.Logger) ([]scheduleWindow, error) {
	if cfg.schedule == nil {
		return nil, nil
	}

	minStart := s.getScheduleMinStart(cfg, now)

	times, err := cfg.schedule.TimesBetween(minStart, now)
	if err != nil {
		return nil, fmt.Errorf("expand digest schedule: %w", err)
	}

	if len(times) == 0 {
		logger.Debug().Msg("no scheduled digest windows to process")

		return nil, nil
	}

	prev, err := s.getSchedulePreviousTime(cfg.schedule, minStart)
	if err != nil {
		return nil, err
	}

	windows := buildWindowsFromScheduleTimes(times, prev, minStart)
	s.logScheduleWindows(cfg.schedule, windows, logger)

	return windows, nil
}

func (s *Scheduler) getScheduleMinStart(cfg digestProcessConfig, now time.Time) time.Time {
	minStart := now.Add(-cfg.catchupWindow)

	if !cfg.scheduleAnchor.IsZero() && cfg.scheduleAnchor.After(minStart) {
		minStart = cfg.scheduleAnchor
	}

	return minStart
}

func (s *Scheduler) getSchedulePreviousTime(sched *schedule.Schedule, minStart time.Time) (time.Time, error) {
	prev, ok, err := sched.PreviousTimeBefore(minStart)
	if err != nil {
		return time.Time{}, fmt.Errorf("find previous schedule time: %w", err)
	}

	if !ok || prev.Before(minStart) {
		return minStart, nil
	}

	return prev, nil
}

func buildWindowsFromScheduleTimes(times []time.Time, prev, minStart time.Time) []scheduleWindow {
	windows := make([]scheduleWindow, 0, len(times))

	for _, t := range times {
		if !t.After(prev) {
			continue
		}

		start := prev
		if start.Before(minStart) {
			start = minStart
		}

		if start.Before(t) {
			windows = append(windows, scheduleWindow{start: start.UTC(), end: t.UTC()})
		}

		prev = t
	}

	return windows
}

func (s *Scheduler) logScheduleWindows(sched *schedule.Schedule, windows []scheduleWindow, logger *zerolog.Logger) {
	if sched == nil {
		return
	}

	loc, err := sched.Location()
	if err != nil {
		logger.Debug().Err(err).Msg("schedule timezone invalid, skipping window log")
		return
	}

	if len(windows) == 0 {
		logger.Debug().Msg("schedule produced no windows")
		return
	}

	for _, w := range windows {
		logger.Debug().
			Time("start_local", w.start.In(loc)).
			Time("end_local", w.end.In(loc)).
			Msg("scheduled digest window")
	}
}

func (s *Scheduler) processWindow(ctx context.Context, start, end time.Time, targetChatID int64, importanceThreshold float32, logger *zerolog.Logger) (*anomalyInfo, error) {
	// Check if already posted
	exists, err := s.database.DigestExists(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to check if digest exists: %w", err)
	}

	if exists {
		logger.Debug().Time(LogFieldStart, start).Time(LogFieldEnd, end).Msg("Digest already exists for window")

		return nil, nil //nolint:nilnil // nil,nil indicates digest already exists
	}

	text, items, clusters, anomalyAny, err := s.BuildDigest(ctx, start, end, importanceThreshold, logger)
	if err != nil {
		return nil, err
	}

	if text == "" {
		if anomalyAny == nil {
			return nil, nil //nolint:nilnil // nil,nil indicates no items but no anomaly
		}

		anomaly, ok := anomalyAny.(*anomalyInfo)
		if !ok {
			return nil, nil //nolint:nilnil // unexpected type, treat as no anomaly
		}

		return anomaly, nil
	}

	// Generate digest ID early to use in rating buttons
	digestID := uuid.New().String()

	msgID, err := s.postDigest(ctx, targetChatID, text, digestID, start, end, importanceThreshold, items, clusters, logger)
	if err != nil {
		return nil, err
	}

	s.finalizeDigest(ctx, digestID, start, end, targetChatID, msgID, items, clusters, logger)

	return nil, nil //nolint:nilnil // nil,nil indicates successful completion with no anomaly
}

func (s *Scheduler) postDigest(ctx context.Context, targetChatID int64, text, digestID string, start, end time.Time, importanceThreshold float32, items []db.Item, clusters []db.ClusterWithItems, logger *zerolog.Logger) (int64, error) {
	// Check if inline images are enabled
	var inlineImagesEnabled bool

	if err := s.database.GetSetting(ctx, "digest_inline_images", &inlineImagesEnabled); err != nil {
		logger.Debug().Err(err).Msg("could not get digest_inline_images from DB, defaulting to disabled")
	}

	// If inline images are enabled, use rich digest mode
	if inlineImagesEnabled {
		return s.postRichDigest(ctx, targetChatID, text, digestID, start, end, importanceThreshold, items, logger)
	}

	// Fetch cover image
	coverImage := s.fetchCoverImage(ctx, start, end, importanceThreshold, items, clusters, logger)

	// Post digest (with or without image)
	msgID, err := s.sendDigest(ctx, targetChatID, text, digestID, coverImage)
	if err != nil {
		observability.DigestsPosted.WithLabelValues(StatusError).Inc()

		if errSave := s.database.SaveDigestError(ctx, start, end, targetChatID, err); errSave != nil {
			logger.Error().Err(errSave).Msg(ErrMsgFailedToSaveDigestError)
		}

		return 0, fmt.Errorf("failed to send digest: %w", err)
	}

	logger.Info().Int64(LogFieldMsgID, msgID).Msg("Digest posted successfully")
	observability.DigestsPosted.WithLabelValues(StatusPosted).Inc()

	return msgID, nil
}

// fetchCoverImage fetches or generates a cover image for the digest.
func (s *Scheduler) fetchCoverImage(ctx context.Context, start, end time.Time, importanceThreshold float32, items []db.Item, clusters []db.ClusterWithItems, logger *zerolog.Logger) []byte {
	// Check if AI cover generation is enabled (independent of cover_image setting)
	var aiCoverEnabled bool

	if err := s.database.GetSetting(ctx, "digest_ai_cover", &aiCoverEnabled); err != nil {
		logger.Debug().Err(err).Msg("could not get digest_ai_cover from DB, defaulting to disabled")
	}

	// Try AI-generated cover first if enabled
	if aiCoverEnabled && s.llmClient != nil {
		topics := extractTopicsFromDigest(items, clusters)

		coverImage, err := s.llmClient.GenerateDigestCover(ctx, topics, "")
		if err != nil {
			logger.Warn().Err(err).Msg("failed to generate AI cover, falling back to original image")
		} else {
			logger.Info().Int("topics_count", len(topics)).Msg("AI cover generated successfully")

			return coverImage
		}
	}

	// Check if regular cover images are enabled
	var coverImageEnabled = true

	if err := s.database.GetSetting(ctx, "digest_cover_image", &coverImageEnabled); err != nil {
		logger.Debug().Err(err).Msg("could not get digest_cover_image from DB, defaulting to enabled")
	}

	if !coverImageEnabled {
		return nil
	}

	// Fall back to original cover image selection
	coverImage, err := s.database.GetDigestCoverImage(ctx, start, end, importanceThreshold)
	if err != nil {
		logger.Debug().Err(err).Msg("no cover image available for digest")

		return nil
	}

	return coverImage
}

// sendDigest sends the digest using the bot.
func (s *Scheduler) sendDigest(ctx context.Context, targetChatID int64, text, digestID string, coverImage []byte) (int64, error) {
	var (
		msgID int64
		err   error
	)

	if len(coverImage) > 0 {
		msgID, err = s.bot.SendDigestWithImage(ctx, targetChatID, text, digestID, coverImage)
	} else {
		msgID, err = s.bot.SendDigest(ctx, targetChatID, text, digestID)
	}

	if err != nil {
		return 0, fmt.Errorf("failed to send digest message: %w", err)
	}

	return msgID, nil
}

// postRichDigest sends the digest with inline images per item.
func (s *Scheduler) postRichDigest(ctx context.Context, targetChatID int64, headerText, digestID string, start, end time.Time, importanceThreshold float32, items []db.Item, logger *zerolog.Logger) (int64, error) {
	// Fetch items with media data
	itemsWithMedia, err := s.database.GetItemsForWindowWithMedia(ctx, start, end, importanceThreshold, len(items))
	if err != nil {
		logger.Warn().Err(err).Msg("failed to fetch items with media, falling back to text-only")

		// Fallback to regular text digest
		msgID, sendErr := s.bot.SendDigest(ctx, targetChatID, headerText, digestID)
		if sendErr != nil {
			return 0, fmt.Errorf("failed to send text digest: %w", sendErr)
		}

		return msgID, nil
	}

	// Build header from the text (extract first part before items)
	header := extractDigestHeader(headerText)

	// Convert items to RichDigestItem format
	richItems := make([]RichDigestItem, 0, len(itemsWithMedia))

	for _, item := range itemsWithMedia {
		richItems = append(richItems, RichDigestItem{
			Summary:    item.Summary,
			Topic:      item.Topic,
			Importance: item.ImportanceScore,
			Channel:    item.SourceChannel,
			ChannelID:  item.SourceChannelID,
			MsgID:      item.SourceMsgID,
			MediaData:  item.MediaData,
		})
	}

	content := RichDigestContent{
		Header:   header,
		Items:    richItems,
		DigestID: digestID,
	}

	msgID, err := s.bot.SendRichDigest(ctx, targetChatID, content)
	if err != nil {
		observability.DigestsPosted.WithLabelValues(StatusError).Inc()

		if errSave := s.database.SaveDigestError(ctx, start, end, targetChatID, err); errSave != nil {
			logger.Error().Err(errSave).Msg(ErrMsgFailedToSaveDigestError)
		}

		return 0, fmt.Errorf("failed to send rich digest: %w", err)
	}

	logger.Info().Int64(LogFieldMsgID, msgID).Int("items_with_images", countItemsWithMedia(itemsWithMedia)).Msg("Rich digest posted successfully")
	observability.DigestsPosted.WithLabelValues(StatusPosted).Inc()

	return msgID, nil
}

// extractDigestHeader extracts the header portion of the digest text.
func extractDigestHeader(text string) string {
	// Find the first item marker or section header
	// The header typically contains: separator, title, metadata
	lines := strings.Split(text, "\n")

	var header strings.Builder

	for _, line := range lines {
		// Stop at the first section marker (Breaking, Notable, Also)
		if strings.Contains(line, "🔴") || strings.Contains(line, "📌") || strings.Contains(line, "📝") {
			break
		}
		// Stop at topic headers
		if strings.HasPrefix(line, "┌") || strings.HasPrefix(line, "│") {
			break
		}

		header.WriteString(line)
		header.WriteString("\n")
	}

	return strings.TrimRight(header.String(), "\n")
}

// countItemsWithMedia counts how many items have media data.
func countItemsWithMedia(items []db.ItemWithMedia) int {
	count := 0

	for _, item := range items {
		if len(item.MediaData) > 0 {
			count++
		}
	}

	return count
}

// extractTopicsFromDigest extracts unique topics from items and clusters for AI cover generation.
func extractTopicsFromDigest(items []db.Item, clusters []db.ClusterWithItems) []string {
	topicSet := make(map[string]struct{})

	// Extract topics from clusters first (they're more specific)
	for _, c := range clusters {
		if c.Topic != "" {
			topicSet[c.Topic] = struct{}{}
		}
	}

	// Extract topics from individual items
	for _, item := range items {
		if item.Topic != "" {
			topicSet[item.Topic] = struct{}{}
		}
	}

	topics := make([]string, 0, len(topicSet))
	for topic := range topicSet {
		topics = append(topics, topic)
	}

	return topics
}

func (s *Scheduler) finalizeDigest(ctx context.Context, digestID string, start, end time.Time, targetChatID, msgID int64, items []db.Item, clusters []db.ClusterWithItems, logger *zerolog.Logger) {
	// Mark items as digested
	itemIDs := make([]string, len(items))

	for i, item := range items {
		itemIDs[i] = item.ID
	}

	if err := s.database.MarkItemsAsDigested(ctx, itemIDs); err != nil {
		logger.Error().Err(err).Msg("failed to mark items as digested")
	}

	// Save digest record
	_, err := s.database.SaveDigest(ctx, digestID, start, end, targetChatID, msgID)
	if err != nil {
		logger.Error().Err(err).Msg("failed to save digest record")
	}

	// Save digest entries
	entries := s.createDigestEntries(items, clusters)

	if err := s.database.SaveDigestEntries(ctx, digestID, entries); err != nil {
		logger.Error().Err(err).Msg("failed to save digest entries")
	}

	if err := s.updateStatsAfterDigest(ctx, start, end, logger); err != nil {
		logger.Debug().Err(err).Msg("stats collection failed (non-fatal)")
	}
}

func (s *Scheduler) createDigestEntries(items []db.Item, clusters []db.ClusterWithItems) []db.DigestEntry {
	var entries []db.DigestEntry

	if len(clusters) > 0 {
		for _, c := range clusters {
			entry := db.DigestEntry{
				Title: c.Topic,
				Body:  "",
			}
			for _, item := range c.Items {
				entry.Body += fmt.Sprintf("• %s\n", item.Summary)
				entry.Sources = append(entry.Sources, db.DigestSource{
					Channel: item.SourceChannel,
					MsgID:   item.SourceMsgID,
				})
			}

			entries = append(entries, entry)
		}
	} else {
		for _, item := range items {
			entries = append(entries, db.DigestEntry{
				Body: fmt.Sprintf("• %s", item.Summary),
				Sources: []db.DigestSource{{
					Channel: item.SourceChannel,
					MsgID:   item.SourceMsgID,
				}},
			})
		}
	}

	return entries
}

func (s *Scheduler) updateStatsAfterDigest(ctx context.Context, start, end time.Time, logger *zerolog.Logger) error {
	if err := s.database.CollectAndSaveChannelStats(ctx, start, end); err != nil {
		logger.Error().Err(err).Msg("failed to collect channel stats")
		return fmt.Errorf("failed to collect and save channel stats: %w", err)
	}

	return nil
}

// BuildDigest builds a digest for the given window.
// The fourth return value is package-private anomaly info (returned as any for interface compatibility).
func (s *Scheduler) BuildDigest(ctx context.Context, start, end time.Time, importanceThreshold float32, logger *zerolog.Logger) (string, []db.Item, []db.ClusterWithItems, any, error) {
	totalItems, err := s.database.CountItemsInWindow(ctx, start, end)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to count items in window")
	}

	readyItems, err := s.database.CountReadyItemsInWindow(ctx, start, end)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to count ready items in window")
	}

	items, err := s.database.GetItemsForWindow(ctx, start, end, importanceThreshold, s.cfg.DigestTopN*DigestPoolMultiplier)
	if err != nil {
		return "", nil, nil, nil, fmt.Errorf("failed to get items for window: %w", err)
	}

	if anomaly := s.checkEmptyWindow(ctx, items, start, end, totalItems, readyItems, importanceThreshold, logger); anomaly != nil || len(items) == 0 {
		return "", nil, nil, anomaly, nil
	}

	settings := s.getDigestSettings(ctx, logger)
	items = s.applySmartSelection(items, settings)
	items = s.deduplicateItems(items, logger)
	items = s.applyTopicBalanceAndLimit(items, settings, logger)

	logger.Info().Time(LogFieldStart, start).Time(LogFieldEnd, end).Int(LogFieldCount, len(items)).Msg("Processing items for digest")

	clusters, err := s.performClusteringIfEnabled(ctx, items, start, end, settings, logger)
	if err != nil {
		return "", nil, nil, nil, err
	}

	return s.renderDigest(ctx, items, clusters, start, end, settings, logger)
}

// performClusteringIfEnabled runs clustering and fetches clusters if topics are enabled
func (s *Scheduler) performClusteringIfEnabled(ctx context.Context, items []db.Item, start, end time.Time, settings digestSettings, logger *zerolog.Logger) ([]db.ClusterWithItems, error) {
	if !settings.topicsEnabled {
		return nil, nil
	}

	if err := s.clusterItems(ctx, items, start, end, logger); err != nil {
		logger.Error().Err(err).Msg("failed to cluster items")
	}

	clusters, err := s.database.GetClustersForWindow(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to get clusters: %w", err)
	}

	return clusters, nil
}

// renderDigest builds the final digest text
func (s *Scheduler) renderDigest(ctx context.Context, items []db.Item, clusters []db.ClusterWithItems, start, end time.Time, settings digestSettings, logger *zerolog.Logger) (string, []db.Item, []db.ClusterWithItems, *anomalyInfo, error) {
	rc := s.newRenderContext(ctx, settings, items, clusters, start, end, logger)

	var sb strings.Builder

	rc.buildHeaderSection(&sb)
	rc.buildMetadataSection(&sb)

	narrativeGenerated := rc.generateNarrative(ctx, &sb)

	if !narrativeGenerated || settings.editorDetailedItems {
		s.renderDetailedItems(ctx, &sb, rc)
	}

	sb.WriteString("\n" + DigestSeparatorLine)

	return sb.String(), items, clusters, nil, nil
}

// renderDetailedItems renders the breaking/notable/also sections
func (s *Scheduler) renderDetailedItems(ctx context.Context, sb *strings.Builder, rc *digestRenderContext) {
	breakingTitle, notableTitle, alsoTitle := rc.getSectionTitles()
	breaking, notable, also := rc.categorizeByImportance()

	rc.renderGroup(ctx, sb, breaking, EmojiBreaking, breakingTitle)
	rc.renderGroup(ctx, sb, notable, EmojiNotable, notableTitle)
	rc.renderGroup(ctx, sb, also, EmojiStandard, alsoTitle)
}

// sendConsolidatedAnomalyNotification sends a single notification summarizing all anomalies
func (s *Scheduler) sendConsolidatedAnomalyNotification(ctx context.Context, anomalies []anomalyInfo, threshold float32, logger *zerolog.Logger) {
	if len(anomalies) == 0 {
		return
	}

	var sb strings.Builder

	sb.WriteString("⚠️ <b>Digest Anomaly Report</b>\n\n")

	// Count types
	var (
		thresholdAnomalies, backlogAnomalies int
		totalItems, totalReady               int
	)

	for _, a := range anomalies {
		if a.isBacklog {
			backlogAnomalies++
		} else {
			thresholdAnomalies++
			totalItems += a.totalItems
			totalReady += a.readyItems
		}
	}

	if thresholdAnomalies > 0 {
		sb.WriteString(fmt.Sprintf("📊 <b>%d empty windows</b> (items below threshold)\n", thresholdAnomalies))
		sb.WriteString(fmt.Sprintf("• Windows: %s - %s\n",
			anomalies[0].start.Format(TimeFormatHourMinute),
			anomalies[len(anomalies)-1].end.Format(TimeFormatHourMinute)))
		sb.WriteString(fmt.Sprintf("• Total items: <code>%d</code>\n", totalItems))
		sb.WriteString(fmt.Sprintf("• Ready items: <code>%d</code>\n", totalReady))
		sb.WriteString(fmt.Sprintf("• Threshold: <code>%.2f</code>\n", threshold))
		sb.WriteString("\n💡 Consider lowering <code>importance_threshold</code>\n")
	}

	if backlogAnomalies > 0 {
		// Find max backlog size
		maxBacklog := 0

		for _, a := range anomalies {
			if a.isBacklog && a.backlogSize > maxBacklog {
				maxBacklog = a.backlogSize
			}
		}

		sb.WriteString(fmt.Sprintf("\n🔄 <b>Large backlog detected</b> (<code>%d</code> messages)\n", maxBacklog))
		sb.WriteString("Pipeline is catching up - messages pending LLM processing.\n")
	}

	if err := s.bot.SendNotification(ctx, sb.String()); err != nil {
		logger.Warn().Err(err).Msg("failed to send anomaly notification")

		return
	}

	logger.Info().Int("anomaly_count", len(anomalies)).Msg("Sent consolidated anomaly notification")
}

// summaryGroup groups items with the same summary.
type summaryGroup struct {
	summary         string
	items           []db.Item
	importanceScore float32
}

func (s *Scheduler) formatItems(items []db.Item, includeTopic bool, seenSummaries map[string]bool) string {
	if len(items) == 0 {
		return ""
	}

	groups := groupItemsBySummary(items, seenSummaries)

	var sb strings.Builder

	for _, g := range groups {
		seenSummaries[g.summary] = true
		s.formatSummaryGroup(&sb, g, includeTopic)
	}

	return sb.String()
}

func groupItemsBySummary(items []db.Item, seenSummaries map[string]bool) []summaryGroup {
	var groups []summaryGroup

	summaryToIdx := make(map[string]int)

	for _, item := range items {
		if seenSummaries[item.Summary] {
			continue
		}

		idx, seen := summaryToIdx[item.Summary]
		if !seen {
			summaryToIdx[item.Summary] = len(groups)
			groups = append(groups, summaryGroup{
				summary:         item.Summary,
				items:           []db.Item{item},
				importanceScore: item.ImportanceScore,
			})
		} else {
			groups[idx].items = append(groups[idx].items, item)
			if item.ImportanceScore > groups[idx].importanceScore {
				groups[idx].importanceScore = item.ImportanceScore
			}
		}
	}

	return groups
}

func (s *Scheduler) formatSummaryGroup(sb *strings.Builder, g summaryGroup, includeTopic bool) {
	sanitizedSummary := htmlutils.SanitizeHTML(g.summary)
	prefix := getImportancePrefix(g.importanceScore)

	sb.WriteString(htmlutils.ItemStart)
	sb.WriteString(formatSummaryLine(g, includeTopic, prefix, sanitizedSummary))
	fmt.Fprintf(sb, DigestSourceVia, strings.Join(s.formatItemLinks(g.items), DigestSourceSeparator))
	sb.WriteString(htmlutils.ItemEnd)
	sb.WriteString("\n")
}

func formatSummaryLine(g summaryGroup, includeTopic bool, prefix, sanitizedSummary string) string {
	if !includeTopic || g.items[0].Topic == "" {
		return fmt.Sprintf(FormatPrefixSummary, prefix, sanitizedSummary)
	}

	emoji := topicEmojis[g.items[0].Topic]
	if emoji == "" {
		emoji = EmojiBullet
	} else {
		emoji += " " + EmojiBullet
	}

	return fmt.Sprintf("%s %s <b>%s</b>: %s", prefix, emoji, html.EscapeString(g.items[0].Topic), sanitizedSummary)
}

func (s *Scheduler) formatItemLinks(items []db.Item) []string {
	links := make([]string, 0, len(items))

	for _, item := range items {
		label := formatItemLabel(item)
		links = append(links, s.formatLink(item, label))
	}

	return links
}

func formatItemLabel(item db.Item) string {
	if item.SourceChannel != "" {
		return "@" + item.SourceChannel
	}

	if item.SourceChannelTitle != "" {
		return item.SourceChannelTitle
	}

	return DefaultSourceLabel
}

func getImportancePrefix(score float32) string {
	switch {
	case score >= ImportanceScoreBreaking:
		return EmojiBreaking // Breaking/Critical
	case score >= ImportanceScoreNotable:
		return EmojiNotable // Notable
	case score >= ImportanceScoreStandard:
		return EmojiStandard // Standard
	default:
		return EmojiBullet // Minor
	}
}

func (s *Scheduler) formatLink(item db.Item, label string) string {
	if label == "" {
		label = item.SourceChannel
		if label == "" {
			label = item.SourceChannelTitle
		}

		if label == "" {
			label = DefaultSourceLabel
		}
	}

	if item.SourceChannel != "" {
		return fmt.Sprintf("<a href=\"https://t.me/%s/%d\">%s</a>", html.EscapeString(item.SourceChannel), item.SourceMsgID, html.EscapeString(label))
	}
	// For private channels or channels without username
	// Note: tg_peer_id in DB is already the MTProto ID (positive for channels)
	return fmt.Sprintf("<a href=\"https://t.me/c/%d/%d\">%s</a>", item.SourceChannelID, item.SourceMsgID, html.EscapeString(label))
}
