package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

// extractAndStoreBullets extracts bullets from the message and stores them.
// This is a non-fatal operation - failures are logged but don't block the pipeline.
// Only extracts bullets for items above the importance threshold to reduce LLM costs.
func (p *Pipeline) extractAndStoreBullets(ctx context.Context, logger zerolog.Logger, c llm.MessageInput, item *db.Item, digestLanguage string) {
	if !p.cfg.BulletExtractionEnabled || item.Status != StatusReady {
		return
	}

	// Only extract bullets for high-importance items to reduce LLM costs
	if item.ImportanceScore < p.cfg.BulletMinImportance {
		return
	}

	maxBullets := p.cfg.BulletBatchSize
	if maxBullets <= 0 {
		maxBullets = defaultMaxBullets
	}

	input := llm.BulletExtractionInput{
		Text:        c.Text,
		PreviewText: c.PreviewText,
		Summary:     item.Summary,
		MaxBullets:  maxBullets,
	}

	extracted, err := p.llmClient.ExtractBullets(ctx, input, digestLanguage, "")
	if err != nil {
		logger.Warn().Err(err).Str(LogFieldItemID, item.ID).Msg("bullet extraction failed")

		return
	}

	if len(extracted.Bullets) == 0 {
		return
	}

	p.storeBullets(ctx, logger, extracted.Bullets, item)
	logger.Debug().Str(LogFieldItemID, item.ID).Int(LogFieldCount, len(extracted.Bullets)).Msg("bullets extracted and stored")
}

// storeBullets saves extracted bullets to the database and generates embeddings.
func (p *Pipeline) storeBullets(ctx context.Context, logger zerolog.Logger, bullets []llm.ExtractedBullet, item *db.Item) {
	for i, b := range bullets {
		bullet := p.createBulletFromExtracted(i, b, item)

		if err := p.database.InsertBullet(ctx, bullet); err != nil {
			logger.Warn().Err(err).Str(LogFieldItemID, item.ID).Int("bullet_index", i).Msg("failed to insert bullet")

			continue
		}

		p.updateBulletEmbedding(ctx, logger, bullet)
	}
}

// createBulletFromExtracted creates a domain Bullet from an extracted bullet.
// Topic is inherited from the item (which comes from clustering) to avoid fragmentation.
// LLM-generated bullet topics are only used as fallback when item has no topic.
func (p *Pipeline) createBulletFromExtracted(index int, b llm.ExtractedBullet, item *db.Item) *domain.Bullet {
	bullet := &domain.Bullet{
		ItemID:             item.ID,
		BulletIndex:        index,
		Text:               b.Text,
		Topic:              coalesceTopic(item.Topic, b.Topic), // Prefer item topic to avoid fragmentation
		RelevanceScore:     b.RelevanceScore,
		ImportanceScore:    b.ImportanceScore,
		Status:             domain.BulletStatusPending,
		SourceChannel:      item.SourceChannel,
		SourceChannelTitle: item.SourceChannelTitle,
		SourceChannelID:    item.SourceChannelID,
		SourceMsgID:        item.SourceMsgID,
		TGDate:             item.TGDate,
	}
	bullet.BulletHash = generateBulletHash(bullet.Text)

	return bullet
}

// updateBulletEmbedding generates and stores embedding for a bullet.
func (p *Pipeline) updateBulletEmbedding(ctx context.Context, logger zerolog.Logger, bullet *domain.Bullet) {
	if p.embeddingClient == nil {
		return
	}

	embedding, embErr := p.embeddingClient.GetEmbedding(ctx, bullet.Text)
	if embErr != nil || len(embedding) == 0 {
		return
	}

	if updateErr := p.database.UpdateBulletEmbedding(ctx, bullet.ID, embedding); updateErr != nil {
		logger.Warn().Err(updateErr).Str(LogFieldBulletID, bullet.ID).Msg("failed to update bullet embedding")
	}
}

// coalesceTopic returns the first non-empty topic.
func coalesceTopic(topics ...string) string {
	for _, t := range topics {
		if t != "" {
			return t
		}
	}

	return ""
}

// generateBulletHash creates a hash of the bullet text for deduplication.
func generateBulletHash(text string) string {
	hash := sha256.Sum256([]byte(text))

	return hex.EncodeToString(hash[:16]) // Use first 16 bytes (32 hex chars)
}

// defaultMaxBullets is the default number of bullets to extract per message.
const defaultMaxBullets = 3
