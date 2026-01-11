package digest

import (
	"context"
	"fmt"
	"html"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/config"
	"github.com/lueurxax/telegram-digest-bot/internal/db"
	"github.com/lueurxax/telegram-digest-bot/internal/dedup"
	"github.com/lueurxax/telegram-digest-bot/internal/htmlutils"
	"github.com/lueurxax/telegram-digest-bot/internal/llm"
	"github.com/lueurxax/telegram-digest-bot/internal/observability"
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

type DigestPoster interface {
	SendDigest(ctx context.Context, chatID int64, text string, digestID string) (int64, error)
	SendDigestWithImage(ctx context.Context, chatID int64, text string, digestID string, imageData []byte) (int64, error)
	SendNotification(ctx context.Context, text string) error
}

type Scheduler struct {
	cfg       *config.Config
	database  *db.DB
	bot       DigestPoster
	llmClient llm.Client
	logger    *zerolog.Logger
}

func New(cfg *config.Config, database *db.DB, bot DigestPoster, llmClient llm.Client, logger *zerolog.Logger) *Scheduler {
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
		h = 31*h + int64(c)
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
		tickInterval = 10 * time.Minute
	}

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	// Auto-weight update ticker (check every hour, run weekly)
	autoWeightTicker := time.NewTicker(time.Hour)
	defer autoWeightTicker.Stop()
	var lastAutoWeightRun time.Time
	var lastAutoRelevanceRun time.Time

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			s.runOnceWithLock(ctx)
		case <-autoWeightTicker.C:
			s.maybeRunAutoWeightUpdate(ctx, &lastAutoWeightRun)
			s.maybeRunAutoRelevanceUpdate(ctx, &lastAutoRelevanceRun)
		}
	}
}

// maybeRunAutoWeightUpdate checks if it's time for weekly auto-weight update
func (s *Scheduler) maybeRunAutoWeightUpdate(ctx context.Context, lastRun *time.Time) {
	// Check if auto-weight is enabled
	var autoWeightEnabled bool = true
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
	notRunThisWeek := lastRun.IsZero() || now.Sub(*lastRun) > 6*24*time.Hour

	if isSunday && isMidnightHour && notRunThisWeek {
		logger := s.logger.With().Str("task", "auto-weight").Logger()
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
	notRunThisWeek := lastRun.IsZero() || now.Sub(*lastRun) > 6*24*time.Hour

	if isSunday && isMidnightHour && notRunThisWeek {
		logger := s.logger.With().Str("task", "auto-relevance").Logger()
		logger.Info().Msg("Starting weekly auto-relevance update")

		if err := s.UpdateAutoRelevance(ctx, &logger); err != nil {
			logger.Error().Err(err).Msg("failed to update auto-relevance deltas")
		} else {
			*lastRun = now
		}
	}
}

func (s *Scheduler) runOnceWithLock(ctx context.Context) {
	correlationID := uuid.New().String()
	logger := s.logger.With().Str("correlation_id", correlationID).Logger()
	logger.Info().Msg("Starting digest check")

	if !s.cfg.LeaderElectionEnabled {
		if err := s.processDigest(ctx, &logger); err != nil {
			logger.Error().Err(err).Msg("failed to process digest")
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
		logger.Error().Err(err).Msg("failed to process digest")
	}
}

func (s *Scheduler) RunOnce(ctx context.Context) error {
	correlationID := uuid.New().String()
	logger := s.logger.With().Str("correlation_id", correlationID).Logger()
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
	defer s.database.ReleaseAdvisoryLock(ctx, lockID)

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

func (s *Scheduler) processDigest(ctx context.Context, logger *zerolog.Logger) error {
	windowStr := s.cfg.DigestWindow
	if err := s.database.GetSetting(ctx, "digest_window", &windowStr); err != nil {
		logger.Debug().Err(err).Msg("could not get digest_window from DB, using default")
	}
	window, err := time.ParseDuration(windowStr)
	if err != nil {
		logger.Error().Err(err).Str("window", windowStr).Msg("invalid digest window duration, using 1h")
		window = time.Hour
	}

	targetChatID := s.cfg.TargetChatID
	if err := s.database.GetSetting(ctx, "target_chat_id", &targetChatID); err != nil {
		logger.Debug().Err(err).Msg("could not get target_chat_id from DB, using default")
	}

	importanceThreshold := s.cfg.ImportanceThreshold
	if err := s.database.GetSetting(ctx, "importance_threshold", &importanceThreshold); err != nil {
		logger.Debug().Err(err).Msg("could not get importance_threshold from DB, using default")
	}

	catchupWindow, err := time.ParseDuration(s.cfg.SchedulerCatchupWindow)
	if err != nil {
		logger.Error().Err(err).Str("window", s.cfg.SchedulerCatchupWindow).Msg("invalid scheduler catchup window, using 24h")
		catchupWindow = 24 * time.Hour
	}

	// Check if anomaly notifications are enabled
	var anomalyNotificationsEnabled bool = true
	if err := s.database.GetSetting(ctx, "anomaly_notifications", &anomalyNotificationsEnabled); err != nil {
		logger.Debug().Err(err).Msg("could not get anomaly_notifications from DB, defaulting to enabled")
	}

	// Collect anomalies instead of notifying immediately
	var anomalies []anomalyInfo

	now := time.Now().Truncate(window)
	// Check windows from now-catchupWindow to now
	// This allows catching up on missed digests if the bot was down.
	for t := now.Add(-catchupWindow); !t.After(now.Add(-window)); t = t.Add(window) {
		start := t
		end := t.Add(window)

		anomaly, err := s.processWindow(ctx, start, end, targetChatID, importanceThreshold, logger)
		if err != nil {
			logger.Error().Err(err).
				Time("start", start).
				Time("end", end).
				Int64("target_chat_id", targetChatID).
				Msg("failed to process window")
		}
		if anomaly != nil {
			anomalies = append(anomalies, *anomaly)
		}
	}

	// Send consolidated anomaly notification (if any and enabled)
	if anomalyNotificationsEnabled && len(anomalies) > 0 {
		s.sendConsolidatedAnomalyNotification(ctx, anomalies, importanceThreshold, logger)
	}

	return nil
}

func (s *Scheduler) processWindow(ctx context.Context, start, end time.Time, targetChatID int64, importanceThreshold float32, logger *zerolog.Logger) (*anomalyInfo, error) {
	// Check if already posted
	exists, err := s.database.DigestExists(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to check if digest exists: %w", err)
	}
	if exists {
		logger.Debug().Time("start", start).Time("end", end).Msg("Digest already exists for window")
		return nil, nil
	}

	text, items, clusters, anomaly, err := s.BuildDigest(ctx, start, end, importanceThreshold, logger)
	if err != nil {
		return nil, err
	}
	if text == "" {
		return anomaly, nil
	}

	// Generate digest ID early to use in rating buttons
	digestID := uuid.New().String()

	// Check if cover images are enabled
	var coverImageEnabled bool = true
	if err := s.database.GetSetting(ctx, "digest_cover_image", &coverImageEnabled); err != nil {
		logger.Debug().Err(err).Msg("could not get digest_cover_image from DB, defaulting to enabled")
	}

	// Fetch cover image if enabled
	var coverImage []byte
	if coverImageEnabled {
		coverImage, err = s.database.GetDigestCoverImage(ctx, start, end, importanceThreshold)
		if err != nil {
			logger.Debug().Err(err).Msg("no cover image available for digest")
		}
	}

	// Post digest (with or without image)
	var msgID int64
	if len(coverImage) > 0 {
		msgID, err = s.bot.SendDigestWithImage(ctx, targetChatID, text, digestID, coverImage)
	} else {
		msgID, err = s.bot.SendDigest(ctx, targetChatID, text, digestID)
	}
	if err != nil {
		observability.DigestsPosted.WithLabelValues("error").Inc()
		if errSave := s.database.SaveDigestError(ctx, start, end, targetChatID, err); errSave != nil {
			logger.Error().Err(errSave).Msg("failed to save digest error")
		}
		return nil, err
	}

	logger.Info().Int64("msg_id", msgID).Msg("Digest posted successfully")
	observability.DigestsPosted.WithLabelValues("posted").Inc()

	// Mark items as digested
	itemIDs := make([]string, len(items))
	for i, item := range items {
		itemIDs[i] = item.ID
	}
	if err := s.database.MarkItemsAsDigested(ctx, itemIDs); err != nil {
		logger.Error().Err(err).Msg("failed to mark items as digested")
	}

	// Save digest record
	_, err = s.database.SaveDigest(ctx, digestID, start, end, targetChatID, msgID)
	if err != nil {
		return nil, fmt.Errorf("failed to save digest record: %w", err)
	}

	// Save digest entries
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

	if err := s.database.SaveDigestEntries(ctx, digestID, entries); err != nil {
		return nil, fmt.Errorf("failed to save digest entries: %w", err)
	}

	// Collect and save channel stats for this window
	if err := s.database.CollectAndSaveChannelStats(ctx, start, end); err != nil {
		logger.Error().Err(err).Msg("failed to collect channel stats")
		// Don't fail the digest for stats collection errors
	}

	return nil, nil
}

func (s *Scheduler) BuildDigest(ctx context.Context, start, end time.Time, importanceThreshold float32, logger *zerolog.Logger) (string, []db.Item, []db.ClusterWithItems, *anomalyInfo, error) {
	// Diagnostic logging
	totalItems, _ := s.database.CountItemsInWindow(ctx, start, end)
	readyItems, _ := s.database.CountReadyItemsInWindow(ctx, start, end)

	// Fetch more items than TopN to allow for smart selection (time-decay, diversity)
	poolSize := s.cfg.DigestTopN * 3
	items, err := s.database.GetItemsForWindow(ctx, start, end, importanceThreshold, poolSize)
	if err != nil {
		return "", nil, nil, nil, fmt.Errorf("failed to get items for window: %w", err)
	}

	if len(items) == 0 {
		if totalItems > 0 {
			// Return anomaly info instead of sending notification immediately
			anomaly := &anomalyInfo{
				start:      start,
				end:        end,
				totalItems: totalItems,
				readyItems: readyItems,
				threshold:  importanceThreshold,
			}
			logger.Info().Time("start", start).Time("end", end).
				Int("total_items", totalItems).
				Int("ready_items", readyItems).
				Float32("threshold", importanceThreshold).
				Msg("No items reached importance threshold for digest window")
			return "", nil, nil, anomaly, nil
		} else {
			// Check if backlog is large, which might indicate a problem
			backlog, _ := s.database.GetBacklogCount(ctx)
			if backlog > 100 {
				anomaly := &anomalyInfo{
					start:       start,
					end:         end,
					isBacklog:   true,
					backlogSize: backlog,
				}
				logger.Warn().Int("backlog", backlog).Msg("Large backlog - pipeline is catching up, messages not yet processed for this digest window")
				return "", nil, nil, anomaly, nil
			}
			logger.Debug().Time("start", start).Time("end", end).Msg("No items for digest window")
		}
		return "", nil, nil, nil, nil
	}

	var topicsEnabled bool = true
	if err := s.database.GetSetting(ctx, "topics_enabled", &topicsEnabled); err != nil {
		logger.Debug().Err(err).Msg("could not get topics_enabled from DB")
	}

	freshnessDecayHours := s.cfg.FreshnessDecayHours
	if err := s.database.GetSetting(ctx, "freshness_decay_hours", &freshnessDecayHours); err != nil {
		logger.Debug().Err(err).Msg("could not get freshness_decay_hours from DB")
	}

	freshnessFloor := s.cfg.FreshnessFloor
	if err := s.database.GetSetting(ctx, "freshness_floor", &freshnessFloor); err != nil {
		logger.Debug().Err(err).Msg("could not get freshness_floor from DB")
	}

	topicDiversityCap := s.cfg.TopicDiversityCap
	if err := s.database.GetSetting(ctx, "topic_diversity_cap", &topicDiversityCap); err != nil {
		logger.Debug().Err(err).Msg("could not get topic_diversity_cap from DB")
	}

	minTopicCount := s.cfg.MinTopicCount
	if err := s.database.GetSetting(ctx, "min_topic_count", &minTopicCount); err != nil {
		logger.Debug().Err(err).Msg("could not get min_topic_count from DB")
	}

	// Apply smart selection adjustments
	channelCounts := make(map[string]int)
	for _, item := range items {
		channelCounts[item.SourceChannel]++
	}

	for i := range items {
		// 1. Time-decay: reduce importance of older items
		items[i].ImportanceScore = applyFreshnessDecay(items[i].ImportanceScore, items[i].TGDate, freshnessDecayHours, freshnessFloor)

		// 2. Source Diversity Bonus: boost items from channels that only have 1 item in the pool
		if channelCounts[items[i].SourceChannel] == 1 {
			items[i].ImportanceScore += 0.1
		}
	}

	// Re-sort by adjusted importance
	sort.Slice(items, func(i, j int) bool {
		if items[i].ImportanceScore != items[j].ImportanceScore {
			return items[i].ImportanceScore > items[j].ImportanceScore
		}
		return items[i].RelevanceScore > items[j].RelevanceScore
	})

	// Semantic deduplication: remove items that are too similar to already-kept items
	// This catches duplicates that weren't caught during pipeline processing
	var dedupedItems []db.Item
	for _, item := range items {
		if len(item.Embedding) == 0 {
			// No embedding, keep the item (can't check similarity)
			dedupedItems = append(dedupedItems, item)
			continue
		}

		isDuplicate := false
		for _, kept := range dedupedItems {
			if len(kept.Embedding) == 0 {
				continue
			}
			similarity := dedup.CosineSimilarity(item.Embedding, kept.Embedding)
			if similarity > s.cfg.SimilarityThreshold {
				logger.Debug().
					Str("skipped_id", item.ID).
					Str("duplicate_of", kept.ID).
					Float32("similarity", similarity).
					Msg("Skipping semantic duplicate in digest")
				isDuplicate = true
				break
			}
		}
		if !isDuplicate {
			dedupedItems = append(dedupedItems, item)
		}
	}
	items = dedupedItems

	if topicsEnabled && topicDiversityCap > 0 && topicDiversityCap < 1 && len(items) > 0 {
		result := applyTopicBalance(items, s.cfg.DigestTopN, topicDiversityCap, minTopicCount)
		items = result.Items
		if result.Relaxed {
			logger.Warn().
				Int("topics_available", result.TopicsAvailable).
				Int("topics_selected", result.TopicsSelected).
				Int("max_per_topic", result.MaxPerTopic).
				Float32("cap", topicDiversityCap).
				Msg("Topic diversity cap relaxed due to limited candidates")
		}
	} else if len(items) > s.cfg.DigestTopN {
		items = items[:s.cfg.DigestTopN]
	}

	logger.Info().Time("start", start).Time("end", end).
		Int("count", len(items)).
		Msg("Processing items for digest")

	var editorEnabled bool
	if err := s.database.GetSetting(ctx, "editor_enabled", &editorEnabled); err != nil {
		logger.Debug().Err(err).Msg("could not get editor_enabled from DB")
	}

	var smartLLMModel string
	if err := s.database.GetSetting(ctx, "smart_llm_model", &smartLLMModel); err != nil {
		logger.Debug().Err(err).Msg("could not get smart_llm_model from DB")
	}

	var consolidatedClustersEnabled bool
	if err := s.database.GetSetting(ctx, "consolidated_clusters_enabled", &consolidatedClustersEnabled); err != nil {
		logger.Debug().Err(err).Msg("could not get consolidated_clusters_enabled from DB")
	}

	var editorDetailedItems bool = true
	if err := s.database.GetSetting(ctx, "editor_detailed_items", &editorDetailedItems); err != nil {
		logger.Debug().Err(err).Msg("could not get editor_detailed_items from DB")
	}

	// 1. Perform semantic clustering if enabled
	if topicsEnabled {
		if err := s.clusterItems(ctx, items, start, end, logger); err != nil {
			logger.Error().Err(err).Msg("failed to cluster items")
		}
	}

	// 2. Fetch clusters
	var clusters []db.ClusterWithItems
	if topicsEnabled {
		clusters, err = s.database.GetClustersForWindow(ctx, start, end)
		if err != nil {
			return "", nil, nil, nil, fmt.Errorf("failed to get clusters: %w", err)
		}
	}

	var digestLanguage string
	if err := s.database.GetSetting(ctx, "digest_language", &digestLanguage); err != nil {
		logger.Debug().Err(err).Msg("could not get digest_language from DB")
	}

	var digestTone string
	if err := s.database.GetSetting(ctx, "digest_tone", &digestTone); err != nil {
		logger.Debug().Err(err).Msg("could not get digest_tone from DB")
	}

	header := "Digest for"
	switch strings.ToLower(digestLanguage) {
	case "ru":
		header = "Дайджест за"
	case "de":
		header = "Digest für"
	case "es":
		header = "Resumen para"
	case "fr":
		header = "Résumé pour"
	case "it":
		header = "Riassunto per"
	}

	// Format digest
	var sb strings.Builder
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━\n")
	sb.WriteString(fmt.Sprintf("📰 <b>%s</b> • %s - %s\n", html.EscapeString(header), start.Format("15:04"), end.Format("15:04")))
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━\n")

	// Metadata
	uniqueChannels := make(map[string]bool)
	for _, item := range items {
		uniqueChannels[item.SourceChannel] = true
	}
	topicCount := 0
	if topicsEnabled {
		topicCount = len(clusters)
		if topicCount == 0 {
			topicCount = countDistinctTopics(items)
		}
	}
	sb.WriteString(fmt.Sprintf("📊 <i>%d items from %d channels | %d topics</i>\n\n", len(items), len(uniqueChannels), topicCount))

	seenSummaries := make(map[string]bool)
	var narrativeGenerated bool
	if editorEnabled && smartLLMModel != "" {
		narrative, err := s.llmClient.GenerateNarrative(ctx, items, digestLanguage, smartLLMModel, digestTone)
		if err == nil && narrative != "" {
			sb.WriteString("<blockquote>\n")
			sb.WriteString("📝 <b>Overview</b>\n\n")
			sb.WriteString(htmlutils.SanitizeHTML(narrative))
			sb.WriteString("\n</blockquote>\n")
			narrativeGenerated = true
			if editorDetailedItems {
				sb.WriteString("\n─────────────────────\n<b>📋 Detailed items:</b>\n\n")
			}
		} else if err != nil {
			logger.Warn().Err(err).Msg("Editor-in-Chief narrative generation failed")
		}
	}

	if !narrativeGenerated || editorDetailedItems {
		breakingTitle := "Breaking"
		notableTitle := "Notable"
		alsoTitle := "Also"
		switch strings.ToLower(digestLanguage) {
		case "ru":
			breakingTitle = "Срочно"
			notableTitle = "Важное"
			alsoTitle = "Остальное"
		case "de":
			breakingTitle = "Eilmeldung"
			notableTitle = "Wichtig"
			alsoTitle = "Weiteres"
		case "es":
			breakingTitle = "Última hora"
			notableTitle = "Destacado"
			alsoTitle = "Otros"
		case "fr":
			breakingTitle = "Flash info"
			notableTitle = "Important"
			alsoTitle = "Autres"
		case "it":
			breakingTitle = "Ultime notizie"
			notableTitle = "Importante"
			alsoTitle = "Altro"
		}

		type clusterGroup struct {
			clusters []db.ClusterWithItems
			items    []db.Item
		}

		breaking := clusterGroup{}
		notable := clusterGroup{}
		also := clusterGroup{}

		if topicsEnabled && len(clusters) > 0 {
			for _, c := range clusters {
				maxImp := float32(0)
				for _, it := range c.Items {
					if it.ImportanceScore > maxImp {
						maxImp = it.ImportanceScore
					}
				}
				if maxImp >= 0.8 {
					breaking.clusters = append(breaking.clusters, c)
				} else if maxImp >= 0.5 {
					notable.clusters = append(notable.clusters, c)
				} else {
					also.clusters = append(also.clusters, c)
				}
			}
		} else {
			for _, it := range items {
				if it.ImportanceScore >= 0.8 {
					breaking.items = append(breaking.items, it)
				} else if it.ImportanceScore >= 0.5 {
					notable.items = append(notable.items, it)
				} else {
					also.items = append(also.items, it)
				}
			}
		}

		renderGroup := func(group clusterGroup, emoji, title string) {
			if len(group.clusters) == 0 && len(group.items) == 0 {
				return
			}

			var groupSb strings.Builder
			hasContent := false

			if len(group.clusters) > 0 {
				for _, c := range group.clusters {
					if len(c.Items) > 1 {
						if consolidatedClustersEnabled {
							model := smartLLMModel
							if model == "" {
								model = s.cfg.LLMModel
							}
							summary, err := s.llmClient.SummarizeCluster(ctx, c.Items, digestLanguage, model, digestTone)
							if err == nil && summary != "" {
								summary = htmlutils.SanitizeHTML(summary)
								if seenSummaries[summary] {
									continue
								}
								seenSummaries[summary] = true
								hasContent = true
								// Item boundary marker for intelligent splitting
								groupSb.WriteString(htmlutils.ItemStart)
								if c.Topic != "" {
									emoji := topicEmojis[c.Topic]
									if emoji == "" {
										emoji = "📂"
									}
									groupSb.WriteString("┌─────────────────────\n")
									groupSb.WriteString(fmt.Sprintf("│ %s <b>%s</b> (%d)\n", emoji, strings.ToUpper(html.EscapeString(c.Topic)), len(c.Items)))
									groupSb.WriteString("└─────────────────────\n")
								}
								groupSb.WriteString(fmt.Sprintf("%s %s", getImportancePrefix(c.Items[0].ImportanceScore), summary))
								var links []string
								for _, item := range c.Items {
									label := item.SourceChannel
									if label != "" {
										label = "@" + label
									}
									if label == "" {
										label = item.SourceChannelTitle
									}
									if label == "" {
										label = "Source"
									}
									links = append(links, s.formatLink(item, label))
								}
								if len(links) > 0 {
									groupSb.WriteString(fmt.Sprintf(" <i>via %s</i>", strings.Join(links, " • ")))
								}
								groupSb.WriteString(htmlutils.ItemEnd)
								groupSb.WriteString("\n")
								continue
							} else if err != nil {
								logger.Warn().Err(err).Str("cluster", c.Topic).Msg("failed to summarize cluster, falling back to detailed list")
							}
						}
						emoji := topicEmojis[c.Topic]
						if emoji == "" {
							emoji = "📂"
						}
						// Show only the representative (first item, sorted by importance)
						// but aggregate sources from all cluster items
						representative := c.Items[0]
						if seenSummaries[representative.Summary] {
							continue
						}
						seenSummaries[representative.Summary] = true
						hasContent = true

						// Item boundary marker for intelligent splitting
						groupSb.WriteString(htmlutils.ItemStart)
						groupSb.WriteString("┌─────────────────────\n")
						groupSb.WriteString(fmt.Sprintf("│ %s <b>%s</b>\n", emoji, strings.ToUpper(html.EscapeString(c.Topic))))
						groupSb.WriteString("└─────────────────────\n")

						sanitizedSummary := htmlutils.SanitizeHTML(representative.Summary)
						prefix := getImportancePrefix(representative.ImportanceScore)
						groupSb.WriteString(fmt.Sprintf("%s %s", prefix, sanitizedSummary))

						// Collect sources from ALL items in cluster
						var links []string
						for _, item := range c.Items {
							label := item.SourceChannel
							if label != "" {
								label = "@" + label
							}
							if label == "" {
								label = item.SourceChannelTitle
							}
							if label == "" {
								label = "Source"
							}
							links = append(links, s.formatLink(item, label))
						}
						if len(links) > 0 {
							groupSb.WriteString(fmt.Sprintf("\n    ↳ <i>via %s</i>", strings.Join(links, " • ")))
						}
						if len(c.Items) > 1 {
							groupSb.WriteString(fmt.Sprintf(" <i>(+%d related)</i>", len(c.Items)-1))
						}
						groupSb.WriteString(htmlutils.ItemEnd)
						groupSb.WriteString("\n\n")
					} else {
						formatted := s.formatItems(c.Items, true, seenSummaries)
						if formatted != "" {
							hasContent = true
							groupSb.WriteString(formatted)
						}
					}
				}
			} else {
				formatted := s.formatItems(group.items, true, seenSummaries)
				if formatted != "" {
					hasContent = true
					groupSb.WriteString(formatted)
				}
			}

			if hasContent {
				sb.WriteString(fmt.Sprintf("\n%s <b>%s</b>\n", emoji, title))
				sb.WriteString(groupSb.String())
			}
		}

		renderGroup(breaking, "🔴", breakingTitle)
		renderGroup(notable, "📌", notableTitle)
		renderGroup(also, "📝", alsoTitle)
	}

	sb.WriteString("\n━━━━━━━━━━━━━━━━━━━━━━\n")

	return sb.String(), items, clusters, nil, nil
}

// sendConsolidatedAnomalyNotification sends a single notification summarizing all anomalies
func (s *Scheduler) sendConsolidatedAnomalyNotification(ctx context.Context, anomalies []anomalyInfo, threshold float32, logger *zerolog.Logger) {
	if len(anomalies) == 0 {
		return
	}

	var sb strings.Builder
	sb.WriteString("⚠️ <b>Digest Anomaly Report</b>\n\n")

	// Count types
	var thresholdAnomalies, backlogAnomalies int
	var totalItems, totalReady int
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
			anomalies[0].start.Format("15:04"),
			anomalies[len(anomalies)-1].end.Format("15:04")))
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

	_ = s.bot.SendNotification(ctx, sb.String())
	logger.Info().Int("anomaly_count", len(anomalies)).Msg("Sent consolidated anomaly notification")
}

func (s *Scheduler) formatItems(items []db.Item, includeTopic bool, seenSummaries map[string]bool) string {
	if len(items) == 0 {
		return ""
	}

	// Group items by summary to avoid duplicates
	type summaryGroup struct {
		summary         string
		items           []db.Item
		importanceScore float32
	}
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

	var sb strings.Builder
	for _, g := range groups {
		seenSummaries[g.summary] = true
		sanitizedSummary := htmlutils.SanitizeHTML(g.summary)
		prefix := getImportancePrefix(g.importanceScore)

		// Item boundary marker for intelligent splitting
		sb.WriteString(htmlutils.ItemStart)

		if includeTopic && g.items[0].Topic != "" {
			emoji := topicEmojis[g.items[0].Topic]
			if emoji == "" {
				emoji = "•"
			} else {
				emoji = emoji + " •"
			}
			sb.WriteString(fmt.Sprintf("%s %s <b>%s</b>: %s", prefix, emoji, html.EscapeString(g.items[0].Topic), sanitizedSummary))
		} else {
			sb.WriteString(fmt.Sprintf("%s %s", prefix, sanitizedSummary))
		}

		var links []string
		for _, item := range g.items {
			label := item.SourceChannel
			if label != "" {
				label = "@" + label
			}
			if label == "" {
				label = item.SourceChannelTitle
			}
			if label == "" {
				label = "Source"
			}
			links = append(links, s.formatLink(item, label))
		}
		if len(links) > 0 {
			sb.WriteString(fmt.Sprintf("\n    ↳ <i>via %s</i>", strings.Join(links, " • ")))
		}
		sb.WriteString(htmlutils.ItemEnd)
		sb.WriteString("\n")
	}
	return sb.String()
}

func getImportancePrefix(score float32) string {
	switch {
	case score >= 0.8:
		return "🔴" // Breaking/Critical
	case score >= 0.6:
		return "📌" // Notable
	case score >= 0.4:
		return "📝" // Standard
	default:
		return "•" // Minor
	}
}

func (s *Scheduler) formatLink(item db.Item, label string) string {
	if label == "" {
		label = item.SourceChannel
		if label == "" {
			label = item.SourceChannelTitle
		}
		if label == "" {
			label = "Source"
		}
	}
	if item.SourceChannel != "" {
		return fmt.Sprintf("<a href=\"https://t.me/%s/%d\">%s</a>", html.EscapeString(item.SourceChannel), item.SourceMsgID, html.EscapeString(label))
	}
	// For private channels or channels without username
	// Note: tg_peer_id in DB is already the MTProto ID (positive for channels)
	return fmt.Sprintf("<a href=\"https://t.me/c/%d/%d\">%s</a>", item.SourceChannelID, item.SourceMsgID, html.EscapeString(label))
}
