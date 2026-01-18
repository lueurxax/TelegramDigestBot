package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/lueurxax/telegram-digest-bot/internal/storage/sqlc"
)

const (
	errAddChannelByUsername = "add channel by username: %w"
	errMarkDiscoveryAdded   = "mark discovery added: %w"
	errCheckChannelTracked  = "check if channel tracked: %w"
)

// normalizeUsername converts username to lowercase for consistent storage
func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimPrefix(username, "@"))
}

type Channel struct {
	ID                      string
	TGPeerID                int64
	Username                string
	Title                   string
	IsActive                bool
	AccessHash              int64
	InviteLink              string
	Context                 string
	Description             string
	LastTGMessageID         int64
	Category                string
	Tone                    string
	UpdateFreq              string
	RelevanceThreshold      float32
	ImportanceThreshold     float32
	ImportanceWeight        float32
	AutoWeightEnabled       bool
	WeightOverride          bool
	AutoRelevanceEnabled    bool
	RelevanceThresholdDelta float32
}

func (db *DB) GetActiveChannels(ctx context.Context) ([]Channel, error) {
	sqlcChannels, err := db.Queries.GetActiveChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("get active channels: %w", err)
	}

	channels := make([]Channel, len(sqlcChannels))

	for i, c := range sqlcChannels {
		// Default weight if not set
		weight := c.ImportanceWeight.Float32
		if !c.ImportanceWeight.Valid || weight == 0 {
			weight = DefaultImportanceWeight
		}

		channels[i] = Channel{
			ID:                      fromUUID(c.ID),
			TGPeerID:                c.TgPeerID,
			Username:                c.Username.String,
			Title:                   c.Title.String,
			IsActive:                c.IsActive,
			AccessHash:              c.AccessHash.Int64,
			InviteLink:              c.InviteLink.String,
			Context:                 c.Context.String,
			Description:             c.Description.String,
			LastTGMessageID:         c.LastTgMessageID,
			Category:                c.Category.String,
			Tone:                    c.Tone.String,
			UpdateFreq:              c.UpdateFreq.String,
			RelevanceThreshold:      c.RelevanceThreshold.Float32,
			ImportanceThreshold:     c.ImportanceThreshold.Float32,
			ImportanceWeight:        weight,
			AutoWeightEnabled:       c.AutoWeightEnabled.Bool,
			WeightOverride:          c.WeightOverride.Bool,
			AutoRelevanceEnabled:    c.AutoRelevanceEnabled.Bool,
			RelevanceThresholdDelta: c.RelevanceThresholdDelta.Float32,
		}
	}

	return channels, nil
}

func (db *DB) AddChannel(ctx context.Context, peerID int64, username, title string) error {
	if err := db.Queries.AddChannel(ctx, sqlc.AddChannelParams{
		TgPeerID: peerID,
		Username: toText(normalizeUsername(username)),
		Title:    toText(title),
	}); err != nil {
		return fmt.Errorf("add channel: %w", err)
	}

	if err := db.markDiscoveryAdded(ctx, username, peerID, ""); err != nil {
		return fmt.Errorf(errMarkDiscoveryAdded, err)
	}

	return nil
}

func (db *DB) AddChannelByUsername(ctx context.Context, username string) error {
	if err := db.Queries.AddChannelByUsername(ctx, toText(normalizeUsername(username))); err != nil {
		return fmt.Errorf(errAddChannelByUsername, err)
	}

	if err := db.markDiscoveryAdded(ctx, username, 0, ""); err != nil {
		return fmt.Errorf(errMarkDiscoveryAdded, err)
	}

	return nil
}

func (db *DB) AddChannelByID(ctx context.Context, peerID int64) error {
	if err := db.Queries.AddChannelByID(ctx, peerID); err != nil {
		return fmt.Errorf("add channel by id: %w", err)
	}

	if err := db.markDiscoveryAdded(ctx, "", peerID, ""); err != nil {
		return fmt.Errorf(errMarkDiscoveryAdded, err)
	}

	return nil
}

func (db *DB) AddChannelByInviteLink(ctx context.Context, inviteLink string) error {
	if err := db.Queries.AddChannelByInviteLink(ctx, toText(inviteLink)); err != nil {
		return fmt.Errorf("add channel by invite link: %w", err)
	}

	if err := db.markDiscoveryAdded(ctx, "", 0, inviteLink); err != nil {
		return fmt.Errorf(errMarkDiscoveryAdded, err)
	}

	return nil
}

// IsChannelTracked returns true when the channel is already active.
func (db *DB) IsChannelTracked(ctx context.Context, username string, peerID int64, inviteLink string) (bool, error) {
	tracked, err := db.Queries.IsChannelTracked(ctx, sqlc.IsChannelTrackedParams{
		Lower:      normalizeUsername(username),
		TgPeerID:   peerID,
		InviteLink: toText(inviteLink),
	})
	if err != nil {
		return false, fmt.Errorf(errCheckChannelTracked, err)
	}

	return tracked, nil
}

func (db *DB) markDiscoveryAdded(ctx context.Context, username string, peerID int64, inviteLink string) error {
	normalized := normalizeUsername(username)
	statuses := []string{DiscoveryStatusPending, DiscoveryStatusRejected, DiscoveryStatusAdded}

	_, err := db.Pool.Exec(ctx, `
		UPDATE discovered_channels dc
		SET matched_channel_id = c.id,
			status = $2,
			status_changed_at = now(),
			status_changed_by = NULL
		FROM channels c
		WHERE c.is_active = TRUE
		  AND dc.matched_channel_id IS NULL
		  AND dc.status = ANY($3)
		  AND (
			($1 != '' AND c.username != '' AND dc.username != '' AND lower(dc.username) = lower($1) AND lower(c.username) = lower($1)) OR
			($4 != 0 AND dc.tg_peer_id = $4 AND c.tg_peer_id = $4) OR
			($5 != '' AND dc.invite_link = $5 AND c.invite_link = $5)
		  )
	`, normalized, DiscoveryStatusAdded, statuses, peerID, inviteLink)
	if err != nil {
		return fmt.Errorf("update discovery matches: %w", err)
	}

	return nil
}

func (db *DB) UpdateChannel(ctx context.Context, id string, peerID int64, title string, accessHash int64, username string, description string) error {
	if err := db.Queries.UpdateChannel(ctx, sqlc.UpdateChannelParams{
		ID:          toUUID(id),
		TgPeerID:    peerID,
		Title:       toText(title),
		AccessHash:  toInt8(accessHash),
		Username:    toText(normalizeUsername(username)),
		Description: toText(description),
	}); err != nil {
		return fmt.Errorf("update channel: %w", err)
	}

	return nil
}

func (db *DB) DeactivateChannel(ctx context.Context, identifier string) error {
	if err := db.Queries.DeactivateChannel(ctx, toText(identifier)); err != nil {
		return fmt.Errorf("deactivate channel: %w", err)
	}

	return nil
}

func (db *DB) UpdateChannelContext(ctx context.Context, identifier, context string) error {
	if err := db.Queries.UpdateChannelContext(ctx, sqlc.UpdateChannelContextParams{
		Username: toText(identifier),
		Context:  toText(context),
	}); err != nil {
		return fmt.Errorf("update channel context: %w", err)
	}

	return nil
}

func (db *DB) UpdateChannelMetadata(ctx context.Context, identifier, category, tone, updateFreq string, relevanceThreshold, importanceThreshold float32) error {
	if err := db.Queries.UpdateChannelMetadata(ctx, sqlc.UpdateChannelMetadataParams{
		Username:            toText(identifier),
		Category:            toText(category),
		Tone:                toText(tone),
		UpdateFreq:          toText(updateFreq),
		RelevanceThreshold:  pgtype.Float4{Float32: relevanceThreshold, Valid: relevanceThreshold > 0},
		ImportanceThreshold: pgtype.Float4{Float32: importanceThreshold, Valid: importanceThreshold > 0},
	}); err != nil {
		return fmt.Errorf("update channel metadata: %w", err)
	}

	return nil
}

func (db *DB) UpdateChannelLastMessageID(ctx context.Context, id string, lastMsgID int64) error {
	if err := db.Queries.UpdateChannelLastMessageID(ctx, sqlc.UpdateChannelLastMessageIDParams{
		ID:              toUUID(id),
		LastTgMessageID: lastMsgID,
	}); err != nil {
		return fmt.Errorf("update channel last message id: %w", err)
	}

	return nil
}

func (db *DB) GetChannelByPeerID(ctx context.Context, peerID int64) (*Channel, error) {
	c, err := db.Queries.GetChannelByPeerID(ctx, peerID)
	if err != nil {
		return nil, fmt.Errorf("get channel by peer id: %w", err)
	}

	return &Channel{
		ID:                      fromUUID(c.ID),
		TGPeerID:                c.TgPeerID,
		Username:                c.Username.String,
		Title:                   c.Title.String,
		IsActive:                c.IsActive,
		AccessHash:              c.AccessHash.Int64,
		InviteLink:              c.InviteLink.String,
		Context:                 c.Context.String,
		Description:             c.Description.String,
		LastTGMessageID:         c.LastTgMessageID,
		Category:                c.Category.String,
		Tone:                    c.Tone.String,
		UpdateFreq:              c.UpdateFreq.String,
		AutoRelevanceEnabled:    c.AutoRelevanceEnabled.Bool,
		RelevanceThresholdDelta: c.RelevanceThresholdDelta.Float32,
	}, nil
}

// ChannelWeight holds weight configuration for a channel
type ChannelWeight struct {
	Username             string
	Title                string
	ImportanceWeight     float32
	AutoWeightEnabled    bool
	WeightOverride       bool
	WeightOverrideReason string
	WeightUpdatedAt      *string
}

func (db *DB) GetChannelWeight(ctx context.Context, identifier string) (*ChannelWeight, error) {
	c, err := db.Queries.GetChannelWeight(ctx, toText(identifier))
	if err != nil {
		return nil, fmt.Errorf("get channel weight: %w", err)
	}

	weight := c.ImportanceWeight.Float32
	if !c.ImportanceWeight.Valid || weight == 0 {
		weight = DefaultImportanceWeight
	}

	var updatedAt *string

	if c.WeightUpdatedAt.Valid {
		s := c.WeightUpdatedAt.Time.Format("2006-01-02 15:04")
		updatedAt = &s
	}

	return &ChannelWeight{
		Username:             c.Username.String,
		Title:                c.Title.String,
		ImportanceWeight:     weight,
		AutoWeightEnabled:    c.AutoWeightEnabled.Bool,
		WeightOverride:       c.WeightOverride.Bool,
		WeightOverrideReason: c.WeightOverrideReason.String,
		WeightUpdatedAt:      updatedAt,
	}, nil
}

// UpdateChannelWeightResult contains info about the updated channel
type UpdateChannelWeightResult struct {
	Username string
	Title    string
}

func (db *DB) UpdateChannelWeight(ctx context.Context, identifier string, weight float32, autoEnabled bool, override bool, reason string, updatedBy int64) (*UpdateChannelWeightResult, error) {
	row, err := db.Queries.UpdateChannelWeight(ctx, sqlc.UpdateChannelWeightParams{
		Username:             toText(identifier),
		ImportanceWeight:     pgtype.Float4{Float32: weight, Valid: true},
		AutoWeightEnabled:    pgtype.Bool{Bool: autoEnabled, Valid: true},
		WeightOverride:       pgtype.Bool{Bool: override, Valid: true},
		WeightOverrideReason: toText(reason),
		WeightUpdatedBy:      toInt8(updatedBy),
	})
	if err != nil {
		return nil, fmt.Errorf("update channel weight: %w", err)
	}

	return &UpdateChannelWeightResult{
		Username: row.Username.String,
		Title:    row.Title.String,
	}, nil
}

func (db *DB) UpdateChannelRelevanceDelta(ctx context.Context, channelID string, delta float32, autoEnabled bool) error {
	if err := db.Queries.UpdateChannelRelevanceDelta(ctx, sqlc.UpdateChannelRelevanceDeltaParams{
		ID:                      toUUID(channelID),
		RelevanceThresholdDelta: pgtype.Float4{Float32: delta, Valid: true},
		AutoRelevanceEnabled:    pgtype.Bool{Bool: autoEnabled, Valid: true},
	}); err != nil {
		return fmt.Errorf("update channel relevance delta: %w", err)
	}

	return nil
}
