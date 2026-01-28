// Package ports provides domain-centric interfaces for external dependencies.
// These interfaces follow the ports and adapters (hexagonal) architecture pattern,
// allowing business logic to remain independent of infrastructure concerns.
package ports

import (
	"context"
	"time"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
)

// SettingsReader provides read access to application settings.
type SettingsReader interface {
	GetSetting(ctx context.Context, key string, target interface{}) error
}

// SettingsWriter provides write access to application settings.
type SettingsWriter interface {
	SaveSettingWithHistory(ctx context.Context, key string, value interface{}, userID int64) error
	DeleteSettingWithHistory(ctx context.Context, key string, userID int64) error
}

// SettingsStore combines settings read and write operations.
type SettingsStore interface {
	SettingsReader
	SettingsWriter
}

// MessageRepository handles raw message operations.
type MessageRepository interface {
	GetUnprocessedMessages(ctx context.Context, limit int) ([]RawMessage, error)
	GetBacklogCount(ctx context.Context) (int, error)
	MarkAsProcessed(ctx context.Context, id string) error
	ReleaseClaimedMessage(ctx context.Context, id string) error
	RecoverStuckPipelineMessages(ctx context.Context, stuckThreshold time.Duration) (int64, error)
	GetRecentMessagesForChannel(ctx context.Context, channelID string, before time.Time, limit int) ([]string, error)
}

// ItemRepository handles item CRUD operations.
type ItemRepository interface {
	SaveItem(ctx context.Context, item *Item) error
	SaveItemError(ctx context.Context, rawMsgID string, errJSON []byte) error
	SaveEmbedding(ctx context.Context, itemID string, embedding []float32) error
}

// DuplicateChecker handles deduplication checks.
type DuplicateChecker interface {
	CheckStrictDuplicate(ctx context.Context, hash string, id string) (bool, error)
	FindSimilarItem(ctx context.Context, embedding []float32, threshold float32, minCreatedAt time.Time) (string, error)
	FindSimilarItemForChannel(ctx context.Context, embedding []float32, channelID string, threshold float32, minCreatedAt time.Time) (string, error)
}

// EnrichmentRepository handles enrichment queue and evidence operations.
type EnrichmentRepository interface {
	ClaimNextEnrichment(ctx context.Context) (*EnrichmentQueueItem, error)
	UpdateEnrichmentStatus(ctx context.Context, queueID, status, errMsg string, retryAt *time.Time) error
	EnqueueEnrichment(ctx context.Context, itemID, summary string) error
	CountPendingEnrichments(ctx context.Context) (int, error)
	RecoverStuckEnrichmentItems(ctx context.Context, stuckThreshold time.Duration) (int64, error)
}

// EvidenceRepository handles evidence source and claim operations.
type EvidenceRepository interface {
	GetEvidenceSource(ctx context.Context, urlHash string) (*EvidenceSource, error)
	SaveEvidenceSource(ctx context.Context, src *EvidenceSource) (string, error)
	SaveEvidenceClaim(ctx context.Context, claim *EvidenceClaim) (string, error)
	SaveItemEvidence(ctx context.Context, ie *ItemEvidence) error
	GetClaimsForSource(ctx context.Context, sourceID string) ([]EvidenceClaim, error)
	FindSimilarClaim(ctx context.Context, evidenceID string, embedding []float32, similarity float32) (*EvidenceClaim, error)
	DeleteExpiredEvidenceSources(ctx context.Context) (int64, error)
	CleanupExcessEvidencePerItem(ctx context.Context, maxPerItem int) (int64, error)
	DeduplicateEvidenceClaims(ctx context.Context) (int64, error)
}

// FactCheckRepository handles fact check queue operations.
type FactCheckRepository interface {
	EnqueueFactCheck(ctx context.Context, itemID, claim, normalizedClaim string) error
	CountPendingFactChecks(ctx context.Context) (int, error)
	UpdateItemFactCheckScore(ctx context.Context, itemID string, score float32, tier, notes string) error
}

// CacheRepository handles various caching operations.
type CacheRepository interface {
	GetSummaryCache(ctx context.Context, canonicalHash, digestLanguage string) (*SummaryCacheEntry, error)
	UpsertSummaryCache(ctx context.Context, entry *SummaryCacheEntry) error
	GetTranslation(ctx context.Context, query, targetLang string) (string, error)
	SaveTranslation(ctx context.Context, query, targetLang, translatedText string, ttl time.Duration) error
	CleanupExpiredTranslations(ctx context.Context) (int64, error)
}

// BudgetRepository handles enrichment budget tracking.
type BudgetRepository interface {
	GetDailyEnrichmentCount(ctx context.Context) (int, error)
	GetMonthlyEnrichmentCount(ctx context.Context) (int, error)
	GetDailyEnrichmentCost(ctx context.Context) (float64, error)
	GetMonthlyEnrichmentCost(ctx context.Context) (float64, error)
	IncrementEnrichmentUsage(ctx context.Context, provider string, cost float64) error
	IncrementEmbeddingUsage(ctx context.Context, cost float64) error
}

// ChannelRepository handles channel operations.
type ChannelRepository interface {
	GetChannelStats(ctx context.Context) (map[string]ChannelStats, error)
	GetActiveFilters(ctx context.Context) ([]Filter, error)
}

// LinkRepository handles link operations.
type LinkRepository interface {
	LinkMessageToLink(ctx context.Context, rawMsgID, linkCacheID string, position int) error
	GetLinksForMessage(ctx context.Context, msgID string) ([]domain.ResolvedLink, error)
}

// LogRepository handles logging of pipeline events.
type LogRepository interface {
	SaveRelevanceGateLog(ctx context.Context, rawMsgID string, decision string, confidence *float32, reason, model, gateVersion string) error
	SaveRawMessageDropLog(ctx context.Context, rawMsgID, reason, detail string) error
}

// Type aliases for domain types used in repository interfaces.
// These allow packages to use ports interfaces without importing storage directly.
type (
	RawMessage          = interface{ GetID() string }
	Item                = interface{ GetID() string }
	EnrichmentQueueItem = interface{ GetID() string }
	EvidenceSource      = interface{ GetID() string }
	EvidenceClaim       = interface{ GetID() string }
	ItemEvidence        = interface{}
	SummaryCacheEntry   = interface{}
	ChannelStats        = interface{}
	Filter              = interface{ GetPattern() string }
)

// EmbeddingClient provides embedding generation for semantic operations.
type EmbeddingClient interface {
	GetEmbedding(ctx context.Context, text string) ([]float32, error)
}

// TranslationClient provides text translation services.
type TranslationClient interface {
	Translate(ctx context.Context, text string, targetLanguage string) (string, error)
}

// LinkResolver resolves URLs and extracts content.
type LinkResolver interface {
	ResolveLinks(ctx context.Context, text string, maxLinks int, webTTL, tgTTL time.Duration) ([]domain.ResolvedLink, error)
}
