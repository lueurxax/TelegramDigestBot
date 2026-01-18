package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/lueurxax/telegram-digest-bot/internal/storage/sqlc"
)

// DiscoveredChannel represents a channel found through discovery
type DiscoveredChannel struct {
	ID              string
	Username        string
	TGPeerID        int64
	InviteLink      string
	Title           string
	Description     string
	SourceType      string
	DiscoveryCount  int
	FirstSeenAt     time.Time
	LastSeenAt      time.Time
	MaxViews        int
	MaxForwards     int
	EngagementScore float64
}

// DiscoveryStats contains statistics about channel discovery
type DiscoveryStats struct {
	PendingCount     int64 // Actionable pending (with username)
	UnresolvedCount  int64 // Pending but no username (can't approve/reject)
	ApprovedCount    int64
	RejectedCount    int64
	AddedCount       int64
	TotalCount       int64
	TotalDiscoveries int64
}

// DiscoveryFilterStats captures filter reason counts for pending discoveries.
type DiscoveryFilterStats struct {
	MatchedChannelIDCount int64
	BelowThresholdCount   int64
	AlreadyTrackedCount   int64
}

// Discovery represents info about a channel to be discovered
type Discovery struct {
	Username      string
	TGPeerID      int64
	InviteLink    string
	Title         string
	SourceType    string // "forward", "link", "mention", "reply", "entity_*"
	FromChannelID string
	Views         int
	Forwards      int
	AccessHash    int64
}

// RecordDiscovery records a discovered channel, incrementing count if already known
func (db *DB) RecordDiscovery(ctx context.Context, d Discovery) error {
	normalizedUsername := normalizeUsername(d.Username)

	skip, err := db.shouldSkipDiscovery(ctx, normalizedUsername, d)
	if err != nil {
		return err
	}

	if skip {
		return nil
	}

	return db.upsertDiscovery(ctx, normalizedUsername, d)
}

// shouldSkipDiscovery checks if the discovery should be skipped (already tracked or rejected)
func (db *DB) shouldSkipDiscovery(ctx context.Context, normalizedUsername string, d Discovery) (bool, error) {
	tracked, err := db.Queries.IsChannelTracked(ctx, sqlc.IsChannelTrackedParams{
		Username:   toText(normalizedUsername),
		TgPeerID:   d.TGPeerID,
		InviteLink: toText(d.InviteLink),
	})
	if err != nil {
		return false, fmt.Errorf("check if channel tracked: %w", err)
	}

	if tracked {
		return true, nil
	}

	rejected, err := db.Queries.IsChannelDiscoveredRejected(ctx, sqlc.IsChannelDiscoveredRejectedParams{
		Username:   toText(normalizedUsername),
		TgPeerID:   toInt8(d.TGPeerID),
		InviteLink: toText(d.InviteLink),
	})
	if err != nil {
		return false, fmt.Errorf("check if channel rejected: %w", err)
	}

	return rejected, nil
}

// upsertDiscovery records the discovery based on which identifier is available
func (db *DB) upsertDiscovery(ctx context.Context, normalizedUsername string, d Discovery) error {
	fromChannelUUID := toUUID(d.FromChannelID)

	if d.TGPeerID != 0 {
		if err := db.Queries.UpsertDiscoveredChannelByPeerID(ctx, sqlc.UpsertDiscoveredChannelByPeerIDParams{
			TgPeerID:                toInt8(d.TGPeerID),
			Title:                   toText(d.Title),
			SourceType:              d.SourceType,
			DiscoveredFromChannelID: fromChannelUUID,
			MaxViews:                toInt4(d.Views),
			MaxForwards:             toInt4(d.Forwards),
			AccessHash:              toInt8(d.AccessHash),
		}); err != nil {
			return fmt.Errorf("upsert discovered channel by peer id: %w", err)
		}

		return nil
	}

	if normalizedUsername != "" {
		if err := db.Queries.UpsertDiscoveredChannelByUsername(ctx, sqlc.UpsertDiscoveredChannelByUsernameParams{
			Username:                toText(normalizedUsername),
			Title:                   toText(d.Title),
			SourceType:              d.SourceType,
			DiscoveredFromChannelID: fromChannelUUID,
			MaxViews:                toInt4(d.Views),
			MaxForwards:             toInt4(d.Forwards),
		}); err != nil {
			return fmt.Errorf("upsert discovered channel by username: %w", err)
		}

		return nil
	}

	if d.InviteLink != "" {
		if err := db.Queries.UpsertDiscoveredChannelByInvite(ctx, sqlc.UpsertDiscoveredChannelByInviteParams{
			InviteLink:              toText(d.InviteLink),
			SourceType:              d.SourceType,
			DiscoveredFromChannelID: fromChannelUUID,
			MaxViews:                toInt4(d.Views),
			MaxForwards:             toInt4(d.Forwards),
		}); err != nil {
			return fmt.Errorf("upsert discovered channel by invite: %w", err)
		}

		return nil
	}

	return nil
}

// GetPendingDiscoveries returns pending discoveries sorted by count
func (db *DB) GetPendingDiscoveries(ctx context.Context, limit int, minSeen int, minEngagement float32) ([]DiscoveredChannel, error) {
	rows, err := db.Queries.GetPendingDiscoveries(ctx, sqlc.GetPendingDiscoveriesParams{
		DiscoveryCount:  safeIntToInt32(minSeen),
		EngagementScore: pgtype.Float4{Float32: minEngagement, Valid: true},
		Limit:           safeIntToInt32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("get pending discoveries: %w", err)
	}

	result := make([]DiscoveredChannel, len(rows))
	for i, r := range rows {
		result[i] = DiscoveredChannel{
			ID:              fromUUID(r.ID),
			Username:        r.Username.String,
			TGPeerID:        r.TgPeerID.Int64,
			InviteLink:      r.InviteLink.String,
			Title:           r.Title.String,
			Description:     r.Description.String,
			SourceType:      r.SourceType,
			DiscoveryCount:  int(r.DiscoveryCount),
			FirstSeenAt:     r.FirstSeenAt.Time,
			LastSeenAt:      r.LastSeenAt.Time,
			MaxViews:        int(r.MaxViews.Int32),
			MaxForwards:     int(r.MaxForwards.Int32),
			EngagementScore: float64(r.EngagementScore.Float32),
		}
	}

	return result, nil
}

// GetRejectedDiscoveries returns rejected discoveries sorted by last seen time.
func (db *DB) GetRejectedDiscoveries(ctx context.Context, limit int) ([]DiscoveredChannel, error) {
	rows, err := db.Queries.GetRejectedDiscoveries(ctx, safeIntToInt32(limit))
	if err != nil {
		return nil, fmt.Errorf("get rejected discoveries: %w", err)
	}

	result := make([]DiscoveredChannel, len(rows))
	for i, r := range rows {
		result[i] = DiscoveredChannel{
			ID:              fromUUID(r.ID),
			Username:        r.Username.String,
			TGPeerID:        r.TgPeerID.Int64,
			InviteLink:      r.InviteLink.String,
			Title:           r.Title.String,
			Description:     r.Description.String,
			SourceType:      r.SourceType,
			DiscoveryCount:  int(r.DiscoveryCount),
			FirstSeenAt:     r.FirstSeenAt.Time,
			LastSeenAt:      r.LastSeenAt.Time,
			MaxViews:        int(r.MaxViews.Int32),
			MaxForwards:     int(r.MaxForwards.Int32),
			EngagementScore: float64(r.EngagementScore.Float32),
		}
	}

	return result, nil
}

// ApproveDiscovery approves a discovery and adds it to active channels
func (db *DB) ApproveDiscovery(ctx context.Context, username string, adminID int64) error {
	normalizedUsername := normalizeUsername(username)

	// Add to channels first
	if err := db.Queries.AddChannelByUsername(ctx, toText(normalizedUsername)); err != nil {
		return fmt.Errorf(errAddChannelByUsername, err)
	}

	// Update status to added (not just approved)
	if err := db.Queries.UpdateDiscoveryStatusByUsername(ctx, sqlc.UpdateDiscoveryStatusByUsernameParams{
		Username:        toText(normalizedUsername),
		Status:          DiscoveryStatusAdded,
		StatusChangedBy: toInt8(adminID),
	}); err != nil {
		return fmt.Errorf("update discovery status: %w", err)
	}

	return nil
}

// RejectDiscovery marks a discovery as rejected
func (db *DB) RejectDiscovery(ctx context.Context, username string, adminID int64) error {
	if err := db.Queries.UpdateDiscoveryStatusByUsername(ctx, sqlc.UpdateDiscoveryStatusByUsernameParams{
		Username:        toText(normalizeUsername(username)),
		Status:          DiscoveryStatusRejected,
		StatusChangedBy: toInt8(adminID),
	}); err != nil {
		return fmt.Errorf("reject discovery: %w", err)
	}

	return nil
}

// GetDiscoveryStats returns statistics about channel discovery
func (db *DB) GetDiscoveryStats(ctx context.Context) (*DiscoveryStats, error) {
	row, err := db.Queries.GetDiscoveryStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("get discovery stats: %w", err)
	}

	// TotalDiscoveries is interface{} from COALESCE(SUM(...), 0)
	var totalDiscoveries int64

	switch v := row.TotalDiscoveries.(type) {
	case int64:
		totalDiscoveries = v
	case float64:
		totalDiscoveries = int64(v)
	}

	return &DiscoveryStats{
		PendingCount:     row.PendingCount,
		UnresolvedCount:  row.UnresolvedCount,
		ApprovedCount:    row.ApprovedCount,
		RejectedCount:    row.RejectedCount,
		AddedCount:       row.AddedCount,
		TotalCount:       row.TotalCount,
		TotalDiscoveries: totalDiscoveries,
	}, nil
}

// GetDiscoveryFilterStats returns counts for discovery filter reasons.
func (db *DB) GetDiscoveryFilterStats(ctx context.Context, minSeen int, minEngagement float32) (*DiscoveryFilterStats, error) {
	row, err := db.Queries.GetDiscoveryFilterStats(ctx, sqlc.GetDiscoveryFilterStatsParams{
		DiscoveryCount:  safeIntToInt32(minSeen),
		EngagementScore: pgtype.Float4{Float32: minEngagement, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("get discovery filter stats: %w", err)
	}

	return &DiscoveryFilterStats{
		MatchedChannelIDCount: row.MatchedChannelIDCount,
		BelowThresholdCount:   row.BelowThresholdCount,
		AlreadyTrackedCount:   row.AlreadyTrackedCount,
	}, nil
}

// UnresolvedDiscovery represents a discovery that needs channel info resolution
type UnresolvedDiscovery struct {
	ID         string
	TGPeerID   int64
	AccessHash int64
}

// GetDiscoveriesNeedingResolution returns discoveries with peer IDs but no title/username
func (db *DB) GetDiscoveriesNeedingResolution(ctx context.Context, limit int) ([]UnresolvedDiscovery, error) {
	rows, err := db.Queries.GetDiscoveriesNeedingResolution(ctx, safeIntToInt32(limit))
	if err != nil {
		return nil, fmt.Errorf("get discoveries needing resolution: %w", err)
	}

	result := make([]UnresolvedDiscovery, len(rows))
	for i, r := range rows {
		result[i] = UnresolvedDiscovery{
			ID:         fromUUID(r.ID),
			TGPeerID:   r.TgPeerID.Int64,
			AccessHash: r.AccessHash,
		}
	}

	return result, nil
}

// UpdateDiscoveryChannelInfo updates the title, username, and description for a discovery.
func (db *DB) UpdateDiscoveryChannelInfo(ctx context.Context, id string, title string, username string, description string) error {
	if err := db.Queries.UpdateDiscoveryChannelInfo(ctx, sqlc.UpdateDiscoveryChannelInfoParams{
		ID:          toUUID(id),
		Title:       title,
		Username:    normalizeUsername(username),
		Description: description,
	}); err != nil {
		return fmt.Errorf("update discovery channel info: %w", err)
	}

	return nil
}

// IncrementDiscoveryResolutionAttempts marks that we tried to resolve this discovery
func (db *DB) IncrementDiscoveryResolutionAttempts(ctx context.Context, id string) error {
	if err := db.Queries.IncrementDiscoveryResolutionAttempts(ctx, toUUID(id)); err != nil {
		return fmt.Errorf("increment discovery resolution attempts: %w", err)
	}

	return nil
}

// InviteLinkDiscovery represents a discovery with an invite link needing resolution
type InviteLinkDiscovery struct {
	ID         string
	InviteLink string
}

// GetInviteLinkDiscoveriesNeedingResolution returns discoveries with invite links but no title
func (db *DB) GetInviteLinkDiscoveriesNeedingResolution(ctx context.Context, limit int) ([]InviteLinkDiscovery, error) {
	rows, err := db.Queries.GetInviteLinkDiscoveriesNeedingResolution(ctx, safeIntToInt32(limit))
	if err != nil {
		return nil, fmt.Errorf("get invite link discoveries needing resolution: %w", err)
	}

	result := make([]InviteLinkDiscovery, len(rows))
	for i, r := range rows {
		result[i] = InviteLinkDiscovery{
			ID:         fromUUID(r.ID),
			InviteLink: r.InviteLink.String,
		}
	}

	return result, nil
}

// UpdateDiscoveryFromInvite updates a discovery with info resolved from an invite link.
func (db *DB) UpdateDiscoveryFromInvite(ctx context.Context, id string, title string, username string, description string, peerID int64, accessHash int64) error {
	if err := db.Queries.UpdateDiscoveryFromInvite(ctx, sqlc.UpdateDiscoveryFromInviteParams{
		ID:          toUUID(id),
		Title:       title,
		Username:    normalizeUsername(username),
		Description: description,
		TgPeerID:    peerID,
		AccessHash:  accessHash,
	}); err != nil {
		return fmt.Errorf("update discovery from invite: %w", err)
	}

	return nil
}

// CleanupDiscoveriesBatch marks discoveries as added when a tracked channel matches identifiers.
func (db *DB) CleanupDiscoveriesBatch(ctx context.Context, limit int, adminID int64) (int, error) {
	allowedStatuses := []string{DiscoveryStatusPending, DiscoveryStatusRejected, DiscoveryStatusAdded}

	tag, err := db.Pool.Exec(ctx, `
		WITH candidates AS (
			SELECT DISTINCT ON (dc.id)
				dc.id AS discovery_id,
				c.id AS channel_id
			FROM discovered_channels dc
			JOIN channels c ON c.is_active = TRUE AND (
				(dc.username != '' AND c.username = dc.username) OR
				(dc.tg_peer_id != 0 AND c.tg_peer_id = dc.tg_peer_id) OR
				(dc.invite_link != '' AND c.invite_link = dc.invite_link)
			)
			WHERE dc.matched_channel_id IS NULL
			  AND dc.status = ANY($2)
			ORDER BY dc.id
			LIMIT $1
		)
		UPDATE discovered_channels dc
		SET matched_channel_id = candidates.channel_id,
			status = $3,
			status_changed_at = now(),
			status_changed_by = $4
		FROM candidates
		WHERE dc.id = candidates.discovery_id
	`, limit, allowedStatuses, DiscoveryStatusAdded, toInt8(adminID))
	if err != nil {
		return 0, fmt.Errorf("cleanup discoveries batch: %w", err)
	}

	return int(tag.RowsAffected()), nil
}
