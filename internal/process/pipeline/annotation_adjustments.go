package pipeline

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/observability"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

func (p *Pipeline) loadChannelBias(ctx context.Context, logger zerolog.Logger) map[string]float32 {
	since := time.Now().AddDate(0, 0, -AnnotationBiasLookbackDays)

	summaries, err := p.database.GetWeightedChannelRatingSummary(ctx, since, AnnotationBiasHalfLifeDays)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to load weighted channel ratings")

		return nil
	}

	biases := make(map[string]float32, len(summaries))

	for _, s := range summaries {
		if s.TotalCount < AnnotationBiasMinRatings {
			continue
		}

		if s.WeightedTotal <= 0 {
			continue
		}

		score := (s.WeightedGood - (s.WeightedBad + s.WeightedIrrelevant)) / s.WeightedTotal
		bias := clampFloat32(float32(score*AnnotationBiasScale), -AnnotationBiasMax, AnnotationBiasMax)

		if bias != 0 {
			biases[s.ChannelID] = bias
		}
	}

	return biases
}

func (p *Pipeline) applyChannelBias(c llm.MessageInput, res *llm.BatchResult, biasMap map[string]float32) float32 {
	if len(biasMap) == 0 {
		return 0
	}

	bias := biasMap[c.ChannelID]

	if bias == 0 {
		return 0
	}

	res.RelevanceScore = clampScore(res.RelevanceScore + bias*AnnotationBiasRelevanceFactor)

	channelLabel := c.ChannelTitle
	if channelLabel == "" {
		channelLabel = c.ChannelID
	}

	observability.AnnotationBiasAppliedTotal.WithLabelValues(channelLabel).Inc()

	return bias
}

func (p *Pipeline) applyIrrelevantSuppression(
	ctx context.Context,
	logger zerolog.Logger,
	msgID string,
	embedding []float32,
	res *llm.BatchResult,
) (bool, float64) {
	if len(embedding) == 0 {
		return false, 0
	}

	since := time.Now().AddDate(0, 0, -IrrelevantSimilarityLookbackDays)

	match, err := p.database.FindSimilarIrrelevantItem(ctx, embedding, since)
	if err != nil {
		if errors.Is(err, db.ErrSimilarIrrelevantItemNotFound) {
			return false, 0
		}

		logger.Warn().Err(err).Str(LogFieldMsgID, msgID).Msg("irrelevant similarity lookup failed")

		return false, 0
	}

	if match == nil {
		return false, 0
	}

	observability.IrrelevantSimilarityScore.Observe(match.Similarity)

	if match.Similarity >= IrrelevantSimilarityPenaltyMin {
		observability.IrrelevantSimilarityHitsTotal.Inc()

		res.ImportanceScore = clampScore(res.ImportanceScore - IrrelevantSimilarityPenaltyImp)
		res.RelevanceScore = clampScore(res.RelevanceScore - IrrelevantSimilarityPenaltyRel)
	}

	if match.Similarity >= IrrelevantSimilarityRejectMin {
		observability.IrrelevantSimilarityRejectsTotal.Inc()

		res.ImportanceScore = 0
		res.RelevanceScore = 0

		logger.Debug().Str(LogFieldMsgID, msgID).Float64("similarity", match.Similarity).Msg("rejected due to irrelevant similarity")

		return true, match.Similarity
	}

	return false, match.Similarity
}

func clampScore(value float32) float32 {
	return clampFloat32(value, 0, 1)
}

func clampFloat32(value, minValue, maxValue float32) float32 {
	return float32(math.Max(float64(minValue), math.Min(float64(maxValue), float64(value))))
}
