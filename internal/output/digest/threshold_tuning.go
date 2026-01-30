package digest

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/lueurxax/telegram-digest-bot/internal/storage"
	"github.com/rs/zerolog"
)

const (
	thresholdTuningWindowDays = 30
	thresholdDeltaCap         = 0.3
)

// UpdateGlobalThresholds recalculates the global importance and relevance thresholds based on recent data.
func (s *Scheduler) UpdateGlobalThresholds(ctx context.Context, logger *zerolog.Logger) error {
	enabled := s.getAutoThresholdTuningEnabled(ctx, logger)
	if !enabled {
		return nil
	}

	now := time.Now()
	since := now.AddDate(0, 0, -thresholdTuningWindowDays)

	ratings, err := s.database.GetItemRatingsSince(ctx, since)
	if err != nil {
		return fmt.Errorf(errGetItemRatings, err)
	}

	_, _, net, ok := s.calculateNetScore(now, ratings, logger)
	if !ok {
		return nil
	}

	step := s.getThresholdTuningStep()
	minVal, maxVal := s.getThresholdTuningBounds()

	delta := s.calculateThresholdDelta(net, step)
	if delta == 0 {
		logger.Info().Float64(LogFieldNetScore, net).Msg("Threshold tuning skipped (within neutral band)")
		return nil
	}

	return s.applyThresholdUpdates(ctx, delta, minVal, maxVal, net, logger)
}

func (s *Scheduler) getAutoThresholdTuningEnabled(ctx context.Context, logger *zerolog.Logger) bool {
	// Default to enabled; can be disabled via DB setting
	enabled := true
	if err := s.database.GetSetting(ctx, "auto_threshold_tuning_enabled", &enabled); err != nil {
		logger.Debug().Err(err).Msg("could not get auto_threshold_tuning_enabled from DB")
	}

	return enabled
}

func (s *Scheduler) calculateNetScore(now time.Time, ratings []db.ItemRating, logger *zerolog.Logger) (int, float64, float64, bool) {
	var (
		totalCount         int
		weightedGood       float64
		weightedBad        float64
		weightedIrrelevant float64
		weightedTotal      float64
	)

	for _, r := range ratings {
		weight := decayWeight(now, r.CreatedAt)
		if weight <= 0 {
			continue
		}

		totalCount++
		weightedTotal += weight

		switch r.Rating {
		case RatingGood:
			weightedGood += weight
		case RatingBad:
			weightedBad += weight
		case RatingIrrelevant:
			weightedIrrelevant += weight
		default:
			weightedBad += weight
		}
	}

	if totalCount < s.cfg.RatingMinSampleGlobal || weightedTotal == 0 {
		logger.Info().
			Int(LogFieldGlobalCount, totalCount).
			Int(LogFieldMinGlobal, s.cfg.RatingMinSampleGlobal).
			Msg("Skipping threshold tuning due to insufficient ratings")

		return 0, 0, 0, false
	}

	net := (weightedGood - (weightedBad + weightedIrrelevant)) / weightedTotal

	return totalCount, weightedTotal, net, true
}

func (s *Scheduler) getThresholdTuningStep() float32 {
	step := s.cfg.ThresholdTuningStep
	if step <= 0 {
		step = DefaultThresholdTuningStep
	}

	return step
}

func (s *Scheduler) getThresholdTuningBounds() (float32, float32) {
	minVal := s.cfg.ThresholdTuningMin
	maxVal := s.cfg.ThresholdTuningMax

	if minVal < 0 {
		minVal = 0
	}

	if maxVal > 1 {
		maxVal = 1
	}

	if maxVal < minVal {
		maxVal = minVal
	}

	return minVal, maxVal
}

func (s *Scheduler) calculateThresholdDelta(net float64, step float32) float32 {
	if step <= 0 {
		step = DefaultThresholdTuningStep
	}

	capped := net
	if capped > thresholdDeltaCap {
		capped = thresholdDeltaCap
	} else if capped < -thresholdDeltaCap {
		capped = -thresholdDeltaCap
	}

	delta := float32(capped) * step
	if math.Abs(float64(delta)) < 1e-6 {
		return 0
	}

	return delta
}

func (s *Scheduler) applyThresholdUpdates(ctx context.Context, delta, minVal, maxVal float32, net float64, logger *zerolog.Logger) error {
	relevance := s.cfg.RelevanceThreshold
	if err := s.database.GetSetting(ctx, SettingRelevanceThreshold, &relevance); err != nil {
		logger.Debug().Err(err).Msg("could not get relevance_threshold from DB")
	}

	importance := s.cfg.ImportanceThreshold
	if err := s.database.GetSetting(ctx, SettingImportanceThreshold, &importance); err != nil {
		logger.Debug().Err(err).Msg("could not get importance_threshold from DB")
	}

	newRelevance := clampThreshold(relevance+delta, minVal, maxVal)
	newImportance := clampThreshold(importance+delta, minVal, maxVal)

	if newRelevance != relevance {
		if err := s.database.SaveSetting(ctx, "relevance_threshold", newRelevance); err != nil {
			return fmt.Errorf("failed to save relevance threshold: %w", err)
		}
	}

	if newImportance != importance {
		if err := s.database.SaveSetting(ctx, "importance_threshold", newImportance); err != nil {
			return fmt.Errorf("failed to save importance threshold: %w", err)
		}
	}

	if err := s.database.InsertThresholdTuningLog(ctx, &db.ThresholdTuningLogEntry{
		TunedAt:             time.Now(),
		NetScore:            net,
		Delta:               delta,
		RelevanceThreshold:  newRelevance,
		ImportanceThreshold: newImportance,
	}); err != nil {
		logger.Warn().Err(err).Msg("failed to write threshold tuning log")
	}

	logger.Info().
		Float64(LogFieldNetScore, net).
		Float32(LogFieldDelta, delta).
		Float32("relevance", relevance).
		Float32("relevance_new", newRelevance).
		Float32("importance", importance).
		Float32("importance_new", newImportance).
		Msg("Updated global thresholds from ratings")

	return nil
}

func clampThreshold(value float32, minVal float32, maxVal float32) float32 {
	if value < minVal {
		return minVal
	}

	if value > maxVal {
		return maxVal
	}

	return value
}
