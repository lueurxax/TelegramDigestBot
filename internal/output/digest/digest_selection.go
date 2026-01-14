package digest

import (
	"context"
	"sort"
	"time"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/storage"
	"github.com/lueurxax/telegram-digest-bot/internal/process/dedup"
)

// applySmartSelection applies time-decay and diversity adjustments to items
func (s *Scheduler) applySmartSelection(items []db.Item, settings digestSettings) []db.Item {
	channelCounts := make(map[string]int)

	for _, item := range items {
		channelCounts[item.SourceChannel]++
	}

	for i := range items {
		items[i].ImportanceScore = applyFreshnessDecay(items[i].ImportanceScore, items[i].TGDate, settings.freshnessDecayHours, settings.freshnessFloor)

		if channelCounts[items[i].SourceChannel] == 1 {
			items[i].ImportanceScore += SourceDiversityBonus
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].ImportanceScore != items[j].ImportanceScore {
			return items[i].ImportanceScore > items[j].ImportanceScore
		}

		return items[i].RelevanceScore > items[j].RelevanceScore
	})

	return items
}

// deduplicateItems removes semantically similar items
func (s *Scheduler) deduplicateItems(items []db.Item, logger *zerolog.Logger) []db.Item {
	var dedupedItems []db.Item

	for _, item := range items {
		if len(item.Embedding) == 0 {
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

	return dedupedItems
}

// applyTopicBalanceAndLimit applies topic balancing and limits items to TopN
func (s *Scheduler) applyTopicBalanceAndLimit(items []db.Item, settings digestSettings, logger *zerolog.Logger) []db.Item {
	if settings.topicsEnabled && settings.topicDiversityCap > 0 && settings.topicDiversityCap < 1 && len(items) > 0 {
		result := applyTopicBalance(items, s.cfg.DigestTopN, settings.topicDiversityCap, settings.minTopicCount)
		items = result.Items

		if result.Relaxed {
			logger.Warn().
				Int("topics_available", result.TopicsAvailable).
				Int("topics_selected", result.TopicsSelected).
				Int("max_per_topic", result.MaxPerTopic).
				Float32("cap", settings.topicDiversityCap).
				Msg("Topic diversity cap relaxed due to limited candidates")
		}
	} else if len(items) > s.cfg.DigestTopN {
		items = items[:s.cfg.DigestTopN]
	}

	return items
}

// checkEmptyWindow checks if the window is empty and returns appropriate anomaly info
func (s *Scheduler) checkEmptyWindow(ctx context.Context, items []db.Item, start, end time.Time, totalItems, readyItems int, importanceThreshold float32, logger *zerolog.Logger) *anomalyInfo {
	if len(items) > 0 {
		return nil
	}

	if totalItems > 0 {
		logger.Info().Time(LogFieldStart, start).Time(LogFieldEnd, end).
			Int("total_items", totalItems).
			Int("ready_items", readyItems).
			Float32("threshold", importanceThreshold).
			Msg("No items reached importance threshold for digest window")

		return &anomalyInfo{
			start:      start,
			end:        end,
			totalItems: totalItems,
			readyItems: readyItems,
			threshold:  importanceThreshold,
		}
	}

	backlog, err := s.database.GetBacklogCount(ctx)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to get backlog count")
	}

	if backlog > BacklogThreshold {
		logger.Warn().Int("backlog", backlog).Msg("Large backlog - pipeline is catching up, messages not yet processed for this digest window")

		return &anomalyInfo{
			start:       start,
			end:         end,
			isBacklog:   true,
			backlogSize: backlog,
		}
	}

	logger.Debug().Time(LogFieldStart, start).Time(LogFieldEnd, end).Msg("No items for digest window")

	return nil
}
