package digest

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/db"
)

const (
	autoRelevanceWindowDays    = 30
	autoRelevanceHalfLifeDays  = 14.0
	autoRelevancePenaltyFactor = 0.2
	autoRelevanceDeltaEpsilon  = 0.01
)

type ratingStats struct {
	count              int
	weightedTotal      float64
	weightedGood       float64
	weightedBad        float64
	weightedIrrelevant float64
}

func decayWeight(now time.Time, createdAt time.Time) float64 {
	ageDays := now.Sub(createdAt).Hours() / HoursPerDay
	if ageDays < 0 {
		ageDays = 0
	}

	return math.Exp(-ageDays * math.Ln2 / autoRelevanceHalfLifeDays)
}

func computeRelevanceDelta(reliability float64) float32 {
	penalty := (1.0 - reliability) * autoRelevancePenaltyFactor
	if penalty < 0 {
		penalty = 0
	} else if penalty > autoRelevancePenaltyFactor {
		penalty = autoRelevancePenaltyFactor
	}

	return float32(penalty)
}

func applyRating(stats *ratingStats, rating string, weight float64) {
	if stats == nil {
		return
	}

	stats.count++
	stats.weightedTotal += weight

	switch strings.ToLower(rating) {
	case RatingGood:
		stats.weightedGood += weight
	case RatingBad:
		stats.weightedBad += weight
	case RatingIrrelevant:
		stats.weightedIrrelevant += weight
	default:
		stats.weightedBad += weight
	}
}

// UpdateAutoRelevance updates the auto-relevance delta for all active channels.
func (s *Scheduler) UpdateAutoRelevance(ctx context.Context, logger *zerolog.Logger) error {
	now := time.Now()
	since := now.AddDate(0, 0, -autoRelevanceWindowDays)

	ratings, err := s.database.GetItemRatingsSince(ctx, since)
	if err != nil {
		return err
	}

	stats, globalStats := s.aggregateRatingStats(now, ratings)

	if globalStats.count < s.cfg.RatingMinSampleGlobal {
		logger.Info().
			Int(LogFieldGlobalCount, globalStats.count).
			Int(LogFieldMinGlobal, s.cfg.RatingMinSampleGlobal).
			Msg("Skipping auto-relevance update due to insufficient global ratings")

		return nil
	}

	channels, err := s.database.GetActiveChannels(ctx)
	if err != nil {
		return err
	}

	updated, skipped := s.processChannelsAutoRelevance(ctx, channels, stats, logger)

	logger.Info().
		Int(LogFieldUpdated, updated).
		Int(LogFieldSkipped, skipped).
		Int(LogFieldTotal, len(channels)).
		Int(LogFieldGlobalCount, globalStats.count).
		Float64("global_weighted", globalStats.weightedTotal).
		Msg("Auto-relevance update completed")

	return nil
}

func (s *Scheduler) aggregateRatingStats(now time.Time, ratings []db.ItemRating) (map[string]*ratingStats, *ratingStats) {
	stats := make(map[string]*ratingStats)
	globalStats := &ratingStats{}

	for _, r := range ratings {
		if r.ChannelID == "" {
			continue
		}

		weight := decayWeight(now, r.CreatedAt)
		if weight <= 0 {
			continue
		}

		applyRating(globalStats, r.Rating, weight)

		st := stats[r.ChannelID]
		if st == nil {
			st = &ratingStats{}
			stats[r.ChannelID] = st
		}

		applyRating(st, r.Rating, weight)
	}

	return stats, globalStats
}

func (s *Scheduler) processChannelsAutoRelevance(ctx context.Context, channels []db.Channel, stats map[string]*ratingStats, logger *zerolog.Logger) (int, int) {
	updated := 0
	skipped := 0

	for _, ch := range channels {
		result := s.processSingleChannelAutoRelevance(ctx, ch, stats[ch.ID], logger)
		if result {
			updated++
		} else {
			skipped++
		}
	}

	return updated, skipped
}

// processSingleChannelAutoRelevance processes auto-relevance for a single channel.
// Returns true if updated, false if skipped.
func (s *Scheduler) processSingleChannelAutoRelevance(ctx context.Context, ch db.Channel, st *ratingStats, logger *zerolog.Logger) bool {
	if !ch.AutoRelevanceEnabled {
		return false
	}

	if st == nil || st.count < s.cfg.RatingMinSampleChannel || st.weightedTotal <= 0 {
		return s.resetChannelRelevanceDelta(ctx, ch, logger)
	}

	reliability := clampFloat64(st.weightedGood/st.weightedTotal, 0, 1)
	delta := computeRelevanceDelta(reliability)

	if math.Abs(float64(delta-ch.RelevanceThresholdDelta)) < autoRelevanceDeltaEpsilon {
		return false
	}

	if err := s.database.UpdateChannelRelevanceDelta(ctx, ch.ID, delta, ch.AutoRelevanceEnabled); err != nil {
		logger.Warn().Err(err).Str(LogFieldChannelID, ch.ID).Msg("failed to update relevance delta")

		return false
	}

	s.logAutoRelevanceUpdate(ch, delta, st, reliability, logger)

	return true
}

func (s *Scheduler) resetChannelRelevanceDelta(ctx context.Context, ch db.Channel, logger *zerolog.Logger) bool {
	if ch.RelevanceThresholdDelta == 0 {
		return false
	}

	if err := s.database.UpdateChannelRelevanceDelta(ctx, ch.ID, 0, ch.AutoRelevanceEnabled); err != nil {
		logger.Warn().Err(err).Str(LogFieldChannelID, ch.ID).Msg("failed to reset relevance delta")

		return false
	}

	return true
}

func clampFloat64(value, minVal, maxVal float64) float64 {
	if value < minVal {
		return minVal
	}

	if value > maxVal {
		return maxVal
	}

	return value
}

func (s *Scheduler) logAutoRelevanceUpdate(ch db.Channel, delta float32, st *ratingStats, reliability float64, logger *zerolog.Logger) {
	name := ch.Username
	if name == "" {
		if ch.Title != "" {
			name = ch.Title
		} else {
			name = ch.ID
		}
	}

	logger.Info().
		Str(LogFieldChannel, name).
		Float32("old_delta", ch.RelevanceThresholdDelta).
		Float32("new_delta", delta).
		Int(LogFieldRatingCount, st.count).
		Float64("reliability", reliability).
		Msg("Updated auto-relevance delta")
}
