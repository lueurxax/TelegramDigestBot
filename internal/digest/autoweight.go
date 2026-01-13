package digest

import (
	"context"
	"math"
	"time"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/db"
)

// AutoWeightConfig holds configuration for auto-weight calculation
type AutoWeightConfig struct {
	MinMessages       int     // Minimum messages before auto-weight applies
	ExpectedFrequency float32 // Expected messages per day for consistency scoring
	AutoMin           float32 // Minimum auto-calculated weight
	AutoMax           float32 // Maximum auto-calculated weight
	RollingDays       int     // Number of days to look back for stats
}

// DefaultAutoWeightConfig returns sensible defaults
func DefaultAutoWeightConfig() AutoWeightConfig {
	return AutoWeightConfig{
		MinMessages:       AutoWeightDefaultMinMessages,
		ExpectedFrequency: AutoWeightDefaultExpectedFrequency,
		AutoMin:           AutoWeightDefaultMinWeight,
		AutoMax:           AutoWeightDefaultMaxWeight,
		RollingDays:       AutoWeightDefaultRollingDays,
	}
}

// CalculateAutoWeight computes a channel's weight based on historical stats
func CalculateAutoWeight(stats *db.RollingStats, cfg AutoWeightConfig, days int) float32 {
	// Guard: insufficient data - return neutral weight
	if stats.TotalMessages < cfg.MinMessages {
		return 1.0
	}

	// Calculate derived metrics with null/zero guards
	var inclusionScore float32 = 0.0
	if stats.TotalItemsCreated > 0 {
		inclusionScore = float32(stats.TotalItemsDigested) / float32(stats.TotalItemsCreated)
	}

	// Use avg_importance directly (already 0-1 scale)
	importanceScore := float32(stats.AvgImportance)
	if stats.TotalItemsDigested == 0 {
		importanceScore = 0.5 // No data to judge quality
	}

	// Calculate messages per day
	if days < 1 {
		days = 1
	}

	messagesPerDay := float32(stats.TotalMessages) / float32(days)
	consistencyScore := float32(math.Min(1.0, float64(messagesPerDay/cfg.ExpectedFrequency)))

	// Signal-to-noise with divide-by-zero guard
	var signalScore float32 = 0.0
	if stats.TotalMessages > 0 {
		signalScore = float32(stats.TotalItemsCreated) / float32(stats.TotalMessages)
	}

	// Weighted sum (each component is 0-1)
	rawScore := (inclusionScore * AutoWeightInclusionFactor) +
		(importanceScore * AutoWeightImportanceFactor) +
		(consistencyScore * AutoWeightConsistencyFactor) +
		(signalScore * AutoWeightSignalFactor)

	// Map to weight range and clamp to configured bounds
	// rawScore 0.0 -> weight 0.5; rawScore 1.0 -> weight 1.5
	weight := AutoWeightBaseOffset + rawScore
	weight = float32(math.Max(float64(cfg.AutoMin), math.Min(float64(cfg.AutoMax), float64(weight))))

	return weight
}

func (s *Scheduler) loadAutoWeightConfig(ctx context.Context, logger *zerolog.Logger) AutoWeightConfig {
	cfg := DefaultAutoWeightConfig()

	if err := s.database.GetSetting(ctx, "auto_weight_min_messages", &cfg.MinMessages); err != nil {
		logger.Debug().Err(err).Msg("using default min_messages for auto-weight")
	}

	if err := s.database.GetSetting(ctx, "auto_weight_expected_freq", &cfg.ExpectedFrequency); err != nil {
		logger.Debug().Err(err).Msg("using default expected_frequency for auto-weight")
	}

	if err := s.database.GetSetting(ctx, "auto_weight_min", &cfg.AutoMin); err != nil {
		logger.Debug().Err(err).Msg("using default auto_min for auto-weight")
	}

	if err := s.database.GetSetting(ctx, "auto_weight_max", &cfg.AutoMax); err != nil {
		logger.Debug().Err(err).Msg("using default auto_max for auto-weight")
	}

	if err := s.database.GetSetting(ctx, "auto_weight_rolling_days", &cfg.RollingDays); err != nil {
		logger.Debug().Err(err).Msg("using default rolling_days for auto-weight")
	}

	return cfg
}

// UpdateAutoWeights recalculates weights for all eligible channels
func (s *Scheduler) UpdateAutoWeights(ctx context.Context, logger *zerolog.Logger) error {
	cfg := s.loadAutoWeightConfig(ctx, logger)

	channels, err := s.database.GetChannelsForAutoWeight(ctx)
	if err != nil {
		return err
	}

	since := time.Now().AddDate(0, 0, -cfg.RollingDays)
	updated := 0
	skipped := 0

	for _, ch := range channels {
		stats, err := s.database.GetChannelStatsRolling(ctx, ch.ID, since)
		if err != nil {
			logger.Warn().Err(err).Str(LogFieldChannelID, ch.ID).Msg("failed to get rolling stats")
			continue
		}

		newWeight := CalculateAutoWeight(stats, cfg, cfg.RollingDays)

		// Only update if weight changed significantly (> 0.05)
		if math.Abs(float64(newWeight-ch.ImportanceWeight)) < 0.05 {
			skipped++
			continue
		}

		if err := s.database.UpdateChannelAutoWeight(ctx, ch.ID, newWeight); err != nil {
			logger.Warn().Err(err).Str(LogFieldChannelID, ch.ID).Msg("failed to update auto-weight")
			continue
		}

		logger.Info().
			Str(LogFieldChannel, ch.Username).
			Float32("old_weight", ch.ImportanceWeight).
			Float32("new_weight", newWeight).
			Msg("Updated channel auto-weight")

		updated++
	}

	logger.Info().
		Int(LogFieldUpdated, updated).
		Int(LogFieldSkipped, skipped).
		Int(LogFieldTotal, len(channels)).
		Msg("Auto-weight update completed")

	return nil
}
