package reader

import (
	"context"

	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

// Repository defines the storage operations required by the Reader.
type Repository interface {
	// Channel operations
	GetActiveChannels(ctx context.Context) ([]db.Channel, error)
	UpdateChannel(ctx context.Context, id string, peerID int64, title string, accessHash int64, username, description string) error
	UpdateChannelLastMessageID(ctx context.Context, id string, msgID int64) error
	DeactivateChannel(ctx context.Context, identifier string) error
	DeactivateChannelByID(ctx context.Context, id string) error

	// Message operations
	SaveRawMessage(ctx context.Context, msg *db.RawMessage) error
	CheckAndMarkDiscoveriesExtracted(ctx context.Context, channelID string, tgMessageID int64) (bool, error)

	// Discovery operations
	RecordDiscovery(ctx context.Context, d db.Discovery) error
	GetDiscoveriesNeedingResolution(ctx context.Context, limit int) ([]db.UnresolvedDiscovery, error)
	GetInviteLinkDiscoveriesNeedingResolution(ctx context.Context, limit int) ([]db.InviteLinkDiscovery, error)
	IncrementDiscoveryResolutionAttempts(ctx context.Context, id string) error
	UpdateDiscoveryChannelInfo(ctx context.Context, id, title, username, description string) error
	UpdateDiscoveryFromInvite(ctx context.Context, id, title, username, description string, peerID, accessHash int64) error
}

// Compile-time assertion that *db.DB implements Repository.
var _ Repository = (*db.DB)(nil)
