package db

import (
	"context"

	"github.com/lueurxax/telegram-digest-bot/internal/core/links"
)

// ChannelRepoAdapter wraps *DB to implement links.ChannelRepository.
type ChannelRepoAdapter struct {
	db *DB
}

// NewChannelRepoAdapter creates a new ChannelRepoAdapter.
func NewChannelRepoAdapter(db *DB) *ChannelRepoAdapter {
	return &ChannelRepoAdapter{db: db}
}

// GetChannelByPeerID returns channel info for link resolution.
func (a *ChannelRepoAdapter) GetChannelByPeerID(ctx context.Context, peerID int64) (*links.ChannelInfo, error) {
	ch, err := a.db.GetChannelByPeerID(ctx, peerID)
	if err != nil {
		return nil, err
	}

	return &links.ChannelInfo{
		TGPeerID:   ch.TGPeerID,
		AccessHash: ch.AccessHash,
	}, nil
}

// Ensure ChannelRepoAdapter implements the interface.
var _ links.ChannelRepository = (*ChannelRepoAdapter)(nil)

// Ensure *DB implements LinkCacheRepository (GetLinkCache and SaveLinkCache are already defined).
var _ links.LinkCacheRepository = (*DB)(nil)
