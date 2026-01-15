package bot

import (
	"context"
	"time"

	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

// Repository defines the storage operations required by the Bot.
type Repository interface {
	// Settings operations
	GetSetting(ctx context.Context, key string, target interface{}) error
	SaveSettingWithHistory(ctx context.Context, key string, value interface{}, userID int64) error
	DeleteSettingWithHistory(ctx context.Context, key string, userID int64) error
	GetAllSettings(ctx context.Context) (map[string]interface{}, error)
	GetRecentSettingHistory(ctx context.Context, limit int) ([]db.SettingHistory, error)

	// Rating operations
	SaveRating(ctx context.Context, digestID string, userID int64, rating int16, feedback string) error
	SaveItemRating(ctx context.Context, itemID string, userID int64, rating, feedback string) error
	GetItemRatingSummary(ctx context.Context, since time.Time) ([]db.RatingSummary, error)
	GetLatestChannelRatingStats(ctx context.Context, limit int) ([]db.RatingStatsSummary, error)
	GetLatestGlobalRatingStats(ctx context.Context) (*db.GlobalRatingStats, error)

	// Annotation operations
	EnqueueAnnotationItems(ctx context.Context, since time.Time, limit int) (int, error)
	AssignNextAnnotation(ctx context.Context, userID int64) (*db.AnnotationItem, error)
	LabelAssignedAnnotation(ctx context.Context, userID int64, label, comment string) (*db.AnnotationItem, error)
	SkipAssignedAnnotation(ctx context.Context, userID int64) (*db.AnnotationItem, error)
	GetAnnotationStats(ctx context.Context) (map[string]int, error)

	// Channel operations
	GetActiveChannels(ctx context.Context) ([]db.Channel, error)
	CountActiveChannels(ctx context.Context) (int, error)
	CountRecentlyActiveChannels(ctx context.Context) (int, error)
	GetChannelStats(ctx context.Context) (map[string]db.ChannelStats, error)
	AddChannelByUsername(ctx context.Context, username string) error
	AddChannelByInviteLink(ctx context.Context, inviteLink string) error
	AddChannelByID(ctx context.Context, id int64) error
	DeactivateChannel(ctx context.Context, identifier string) error
	UpdateChannelContext(ctx context.Context, username, context string) error
	UpdateChannelMetadata(ctx context.Context, username, category, tone, freq string, relevance, importance float32) error
	UpdateChannelRelevanceDelta(ctx context.Context, channelID string, delta float32, enabled bool) error
	GetChannelWeight(ctx context.Context, identifier string) (*db.ChannelWeight, error)
	UpdateChannelWeight(ctx context.Context, identifier string, weight float32, autoEnabled, override bool, reason string, userID int64) (*db.UpdateChannelWeightResult, error)

	// Filter operations
	GetActiveFilters(ctx context.Context) ([]db.Filter, error)
	AddFilter(ctx context.Context, filterType, pattern string) error
	DeactivateFilter(ctx context.Context, pattern string) error

	// Item operations
	CountReadyItems(ctx context.Context) (int, error)
	GetBacklogCount(ctx context.Context) (int, error)
	RetryFailedItems(ctx context.Context) error
	RetryItem(ctx context.Context, id string) error
	GetImportanceStats(ctx context.Context, since time.Time, threshold float32) (db.ImportanceStats, error)
	GetTopItemScores(ctx context.Context, since time.Time, limit int) ([]db.ItemScore, error)
	GetScoreDebugStats(ctx context.Context, since time.Time) (db.ScoreDebugStats, error)
	GetItemStatusStats(ctx context.Context, since time.Time) (db.ItemStatusStats, error)
	GetDropReasonStats(ctx context.Context, since time.Time, limit int) ([]db.DropReasonStat, error)

	// Digest operations
	GetLastPostedDigest(ctx context.Context) (*db.LastDigestInfo, error)
	GetRecentErrors(ctx context.Context, limit int) ([]db.Item, error)
	ClearDigestErrors(ctx context.Context) error
	GetDigestCoverImage(ctx context.Context, start, end time.Time, threshold float32) ([]byte, error)
	GetItemsForWindowWithMedia(ctx context.Context, start, end time.Time, threshold float32, limit int) ([]db.ItemWithMedia, error)

	// Discovery operations
	GetPendingDiscoveries(ctx context.Context, limit int) ([]db.DiscoveredChannel, error)
	ApproveDiscovery(ctx context.Context, username string, userID int64) error
	RejectDiscovery(ctx context.Context, username string, userID int64) error
	GetDiscoveryStats(ctx context.Context) (*db.DiscoveryStats, error)
}

// Compile-time assertion that *db.DB implements Repository.
var _ Repository = (*db.DB)(nil)
