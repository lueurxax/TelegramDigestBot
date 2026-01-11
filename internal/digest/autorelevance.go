package digest

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/rs/zerolog"
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
	ageDays := now.Sub(createdAt).Hours() / 24
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

func (s *Scheduler) UpdateAutoRelevance(ctx context.Context, logger *zerolog.Logger) error {
	now := time.Now()
	since := now.AddDate(0, 0, -autoRelevanceWindowDays)

	ratings, err := s.database.GetItemRatingsSince(ctx, since)
	if err != nil {
		return err
	}

	stats := make(map[string]*ratingStats)
	globalCount := 0
	globalWeighted := 0.0

	for _, r := range ratings {
		if r.ChannelID == "" {
			continue
		}
		weight := decayWeight(now, r.CreatedAt)
		if weight <= 0 {
			continue
		}

		globalCount++
		globalWeighted += weight

		st := stats[r.ChannelID]
		if st == nil {
			st = &ratingStats{}
			stats[r.ChannelID] = st
		}
		st.count++
		st.weightedTotal += weight

		switch strings.ToLower(r.Rating) {
		case "good":
			st.weightedGood += weight
		case "bad":
			st.weightedBad += weight
		case "irrelevant":
			st.weightedIrrelevant += weight
		default:
			st.weightedBad += weight
		}
	}

	if globalCount < s.cfg.RatingMinSampleGlobal {
		logger.Info().
			Int("global_count", globalCount).
			Int("min_global", s.cfg.RatingMinSampleGlobal).
			Msg("Skipping auto-relevance update due to insufficient global ratings")
		return nil
	}

	channels, err := s.database.GetActiveChannels(ctx)
	if err != nil {
		return err
	}

	updated := 0
	skipped := 0

	for _, ch := range channels {
		if !ch.AutoRelevanceEnabled {
			skipped++
			continue
		}

		st := stats[ch.ID]
		if st == nil || st.count < s.cfg.RatingMinSampleChannel || st.weightedTotal <= 0 {
			if ch.RelevanceThresholdDelta != 0 {
				if err := s.database.UpdateChannelRelevanceDelta(ctx, ch.ID, 0, ch.AutoRelevanceEnabled); err != nil {
					logger.Warn().Err(err).Str("channel_id", ch.ID).Msg("failed to reset relevance delta")
					continue
				}
				updated++
			} else {
				skipped++
			}
			continue
		}

		reliability := st.weightedGood / st.weightedTotal
		if reliability < 0 {
			reliability = 0
		} else if reliability > 1 {
			reliability = 1
		}

		delta := computeRelevanceDelta(reliability)

		if math.Abs(float64(delta-ch.RelevanceThresholdDelta)) < autoRelevanceDeltaEpsilon {
			skipped++
			continue
		}

		if err := s.database.UpdateChannelRelevanceDelta(ctx, ch.ID, delta, ch.AutoRelevanceEnabled); err != nil {
			logger.Warn().Err(err).Str("channel_id", ch.ID).Msg("failed to update relevance delta")
			continue
		}

		name := ch.Username
		if name == "" {
			if ch.Title != "" {
				name = ch.Title
			} else {
				name = ch.ID
			}
		}

		logger.Info().
			Str("channel", name).
			Float32("old_delta", ch.RelevanceThresholdDelta).
			Float32("new_delta", delta).
			Int("rating_count", st.count).
			Float64("reliability", reliability).
			Msg("Updated auto-relevance delta")
		updated++
	}

	logger.Info().
		Int("updated", updated).
		Int("skipped", skipped).
		Int("total", len(channels)).
		Int("global_count", globalCount).
		Float64("global_weighted", globalWeighted).
		Msg("Auto-relevance update completed")

	return nil
}
