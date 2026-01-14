package links

import (
	"context"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
)

// LinkCacheRepository provides link caching operations.
type LinkCacheRepository interface {
	GetLinkCache(ctx context.Context, url string) (*domain.ResolvedLink, error)
	SaveLinkCache(ctx context.Context, link *domain.ResolvedLink) (string, error)
}

// ChannelInfo contains the minimum channel information needed for link resolution.
type ChannelInfo struct {
	TGPeerID   int64
	AccessHash int64
}

// ChannelRepository provides channel lookup operations.
type ChannelRepository interface {
	GetChannelByPeerID(ctx context.Context, peerID int64) (*ChannelInfo, error)
}
