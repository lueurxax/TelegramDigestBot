package digest

import (
	"context"
	"time"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

// Repository defines the storage operations required by the Scheduler.
type Repository interface {
	// Settings operations
	GetSetting(ctx context.Context, key string, target interface{}) error
	SaveSetting(ctx context.Context, key string, value interface{}) error

	// Advisory lock operations
	TryAcquireAdvisoryLock(ctx context.Context, lockID int64) (bool, error)
	ReleaseAdvisoryLock(ctx context.Context, lockID int64) error

	// Digest operations
	DigestExists(ctx context.Context, start, end time.Time) (bool, error)
	SaveDigest(ctx context.Context, id string, start, end time.Time, chatID, msgID int64) (string, error)
	SaveDigestError(ctx context.Context, start, end time.Time, chatID int64, err error) error
	SaveDigestEntries(ctx context.Context, digestID string, entries []db.DigestEntry) error
	GetDigestCoverImage(ctx context.Context, start, end time.Time, threshold float32) ([]byte, error)

	// Item operations
	GetItemsForWindow(ctx context.Context, start, end time.Time, threshold float32, limit int) ([]db.Item, error)
	GetItemsForWindowWithMedia(ctx context.Context, start, end time.Time, threshold float32, limit int) ([]db.ItemWithMedia, error)
	CountItemsInWindow(ctx context.Context, start, end time.Time) (int, error)
	CountReadyItemsInWindow(ctx context.Context, start, end time.Time) (int, error)
	MarkItemsAsDigested(ctx context.Context, ids []string) error
	GetItemEmbedding(ctx context.Context, id string) ([]float32, error)
	GetBacklogCount(ctx context.Context) (int, error)
	GetLinksForMessage(ctx context.Context, msgID string) ([]domain.ResolvedLink, error)
	GetFactChecksForItems(ctx context.Context, itemIDs []string) (map[string]db.FactCheckMatch, error)
	GetEvidenceForItems(ctx context.Context, itemIDs []string) (map[string][]db.ItemEvidenceWithSource, error)
	GetClusterSummaryCache(ctx context.Context, digestLanguage string, since time.Time) ([]db.ClusterSummaryCacheEntry, error)
	GetClusterSummaryCacheEntry(ctx context.Context, digestLanguage, fingerprint string) (*db.ClusterSummaryCacheEntry, error)
	UpsertClusterSummaryCache(ctx context.Context, entry *db.ClusterSummaryCacheEntry) error

	// Cluster operations
	GetClustersForWindow(ctx context.Context, start, end time.Time) ([]db.ClusterWithItems, error)
	DeleteClustersForWindow(ctx context.Context, start, end time.Time) error
	CreateCluster(ctx context.Context, start, end time.Time, topic string) (string, error)
	AddToCluster(ctx context.Context, clusterID, itemID string) error

	// Rating operations
	GetItemRatingsSince(ctx context.Context, since time.Time) ([]db.ItemRating, error)
	UpsertChannelRatingStats(ctx context.Context, stats *db.RatingStats) error
	UpsertGlobalRatingStats(ctx context.Context, stats *db.RatingStats) error
	InsertThresholdTuningLog(ctx context.Context, entry *db.ThresholdTuningLogEntry) error

	// Channel operations
	GetActiveChannels(ctx context.Context) ([]db.Channel, error)
	GetChannelsForAutoWeight(ctx context.Context) ([]db.ChannelForAutoWeight, error)
	GetChannelStatsRolling(ctx context.Context, channelID string, since time.Time) (*db.RollingStats, error)
	UpdateChannelAutoWeight(ctx context.Context, channelID string, weight float32) error
	UpdateChannelRelevanceDelta(ctx context.Context, channelID string, delta float32, enabled bool) error
	CollectAndSaveChannelStats(ctx context.Context, start, end time.Time) error
}

// Compile-time assertion that *db.DB implements Repository.
var _ Repository = (*db.DB)(nil)
