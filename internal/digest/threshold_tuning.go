package digest

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

const (
	thresholdTuningWindowDays = 30
)

func (s *Scheduler) UpdateGlobalThresholds(ctx context.Context, logger *zerolog.Logger) error {
	enabled := s.cfg.AutoThresholdTuningEnabled
	if err := s.database.GetSetting(ctx, "auto_threshold_tuning_enabled", &enabled); err != nil {
		logger.Debug().Err(err).Msg("could not get auto_threshold_tuning_enabled from DB")
	}

	if !enabled {
		return nil
	}

	now := time.Now()
	since := now.AddDate(0, 0, -thresholdTuningWindowDays)

	ratings, err := s.database.GetItemRatingsSince(ctx, since)
	if err != nil {
		return err
	}

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

		return nil
	}

	net := (weightedGood - (weightedBad + weightedIrrelevant)) / weightedTotal
	step := s.cfg.ThresholdTuningStep

	if step <= 0 {
		step = 0.05
	}

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

	delta := float32(0)
	if net > float64(s.cfg.ThresholdTuningNetPositive) {
		delta = -step
	} else if net < float64(s.cfg.ThresholdTuningNetNegative) {
		delta = step
	}

	if delta == 0 {
		logger.Info().Float64(LogFieldNetScore, net).Msg("Threshold tuning skipped (within neutral band)")

		return nil
	}

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
			return err
		}
	}

	if newImportance != importance {
		if err := s.database.SaveSetting(ctx, "importance_threshold", newImportance); err != nil {
			return err
		}
	}

	logger.Info().
		Float64(LogFieldNetScore, net).
		Float32("delta", delta).
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
