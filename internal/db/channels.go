package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/lueurxax/telegram-digest-bot/internal/db/sqlc"
)

type Channel struct {
	ID                  string
	TGPeerID            int64
	Username            string
	Title               string
	IsActive            bool
	AccessHash          int64
	InviteLink          string
	Context             string
	Description         string
	LastTGMessageID     int64
	Category            string
	Tone                string
	UpdateFreq          string
	RelevanceThreshold  float32
	ImportanceThreshold float32
	ImportanceWeight    float32
	AutoWeightEnabled   bool
	WeightOverride      bool
	AutoRelevanceEnabled   bool
	RelevanceThresholdDelta float32
}

func (db *DB) GetActiveChannels(ctx context.Context) ([]Channel, error) {
	sqlcChannels, err := db.Queries.GetActiveChannels(ctx)
	if err != nil {
		return nil, err
	}

	channels := make([]Channel, len(sqlcChannels))
	for i, c := range sqlcChannels {
		// Default weight to 1.0 if not set
		weight := c.ImportanceWeight.Float32
		if !c.ImportanceWeight.Valid || weight == 0 {
			weight = 1.0
		}
		channels[i] = Channel{
			ID:                  fromUUID(c.ID),
			TGPeerID:            c.TgPeerID,
			Username:            c.Username.String,
			Title:               c.Title.String,
			IsActive:            c.IsActive,
			AccessHash:          c.AccessHash.Int64,
			InviteLink:          c.InviteLink.String,
			Context:             c.Context.String,
			Description:         c.Description.String,
			LastTGMessageID:     c.LastTgMessageID,
			Category:            c.Category.String,
			Tone:                c.Tone.String,
			UpdateFreq:          c.UpdateFreq.String,
			RelevanceThreshold:  c.RelevanceThreshold.Float32,
			ImportanceThreshold: c.ImportanceThreshold.Float32,
			ImportanceWeight:    weight,
			AutoWeightEnabled:   c.AutoWeightEnabled.Bool,
			WeightOverride:      c.WeightOverride.Bool,
			AutoRelevanceEnabled:   c.AutoRelevanceEnabled.Bool,
			RelevanceThresholdDelta: c.RelevanceThresholdDelta.Float32,
		}
	}
	return channels, nil
}

func (db *DB) AddChannel(ctx context.Context, peerID int64, username, title string) error {
	return db.Queries.AddChannel(ctx, sqlc.AddChannelParams{
		TgPeerID: peerID,
		Username: toText(username),
		Title:    toText(title),
	})
}

func (db *DB) AddChannelByUsername(ctx context.Context, username string) error {
	return db.Queries.AddChannelByUsername(ctx, toText(username))
}

func (db *DB) AddChannelByID(ctx context.Context, peerID int64) error {
	return db.Queries.AddChannelByID(ctx, peerID)
}

func (db *DB) AddChannelByInviteLink(ctx context.Context, inviteLink string) error {
	return db.Queries.AddChannelByInviteLink(ctx, toText(inviteLink))
}

func (db *DB) UpdateChannel(ctx context.Context, id string, peerID int64, title string, accessHash int64, username string, description string) error {
	return db.Queries.UpdateChannel(ctx, sqlc.UpdateChannelParams{
		ID:          toUUID(id),
		TgPeerID:    peerID,
		Title:       toText(title),
		AccessHash:  toInt8(accessHash),
		Username:    toText(username),
		Description: toText(description),
	})
}

func (db *DB) DeactivateChannel(ctx context.Context, identifier string) error {
	return db.Queries.DeactivateChannel(ctx, toText(identifier))
}

func (db *DB) UpdateChannelContext(ctx context.Context, identifier, context string) error {
	return db.Queries.UpdateChannelContext(ctx, sqlc.UpdateChannelContextParams{
		Username: toText(identifier),
		Context:  toText(context),
	})
}

func (db *DB) UpdateChannelMetadata(ctx context.Context, identifier, category, tone, updateFreq string, relevanceThreshold, importanceThreshold float32) error {
	return db.Queries.UpdateChannelMetadata(ctx, sqlc.UpdateChannelMetadataParams{
		Username:            toText(identifier),
		Category:            toText(category),
		Tone:                toText(tone),
		UpdateFreq:          toText(updateFreq),
		RelevanceThreshold:  pgtype.Float4{Float32: relevanceThreshold, Valid: relevanceThreshold > 0},
		ImportanceThreshold: pgtype.Float4{Float32: importanceThreshold, Valid: importanceThreshold > 0},
	})
}

func (db *DB) UpdateChannelLastMessageID(ctx context.Context, id string, lastMsgID int64) error {
	return db.Queries.UpdateChannelLastMessageID(ctx, sqlc.UpdateChannelLastMessageIDParams{
		ID:              toUUID(id),
		LastTgMessageID: lastMsgID,
	})
}

func (db *DB) GetChannelByPeerID(ctx context.Context, peerID int64) (*Channel, error) {
	c, err := db.Queries.GetChannelByPeerID(ctx, peerID)
	if err != nil {
		return nil, err
	}
	return &Channel{
		ID:              fromUUID(c.ID),
		TGPeerID:        c.TgPeerID,
		Username:        c.Username.String,
		Title:           c.Title.String,
		IsActive:        c.IsActive,
		AccessHash:      c.AccessHash.Int64,
		InviteLink:      c.InviteLink.String,
		Context:         c.Context.String,
		Description:     c.Description.String,
		LastTGMessageID: c.LastTgMessageID,
		Category:        c.Category.String,
		Tone:            c.Tone.String,
		UpdateFreq:      c.UpdateFreq.String,
		AutoRelevanceEnabled:   c.AutoRelevanceEnabled.Bool,
		RelevanceThresholdDelta: c.RelevanceThresholdDelta.Float32,
	}, nil
}

// ChannelWeight holds weight configuration for a channel
type ChannelWeight struct {
	Username            string
	Title               string
	ImportanceWeight    float32
	AutoWeightEnabled   bool
	WeightOverride      bool
	WeightOverrideReason string
	WeightUpdatedAt     *string
}

func (db *DB) GetChannelWeight(ctx context.Context, identifier string) (*ChannelWeight, error) {
	c, err := db.Queries.GetChannelWeight(ctx, toText(identifier))
	if err != nil {
		return nil, err
	}

	weight := c.ImportanceWeight.Float32
	if !c.ImportanceWeight.Valid || weight == 0 {
		weight = 1.0
	}

	var updatedAt *string
	if c.WeightUpdatedAt.Valid {
		s := c.WeightUpdatedAt.Time.Format("2006-01-02 15:04")
		updatedAt = &s
	}

	return &ChannelWeight{
		Username:            c.Username.String,
		Title:               c.Title.String,
		ImportanceWeight:    weight,
		AutoWeightEnabled:   c.AutoWeightEnabled.Bool,
		WeightOverride:      c.WeightOverride.Bool,
		WeightOverrideReason: c.WeightOverrideReason.String,
		WeightUpdatedAt:     updatedAt,
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
		return nil, err
	}
	return &UpdateChannelWeightResult{
		Username: row.Username.String,
		Title:    row.Title.String,
	}, nil
}

func (db *DB) UpdateChannelRelevanceDelta(ctx context.Context, channelID string, delta float32, autoEnabled bool) error {
	return db.Queries.UpdateChannelRelevanceDelta(ctx, sqlc.UpdateChannelRelevanceDeltaParams{
		ID:                      toUUID(channelID),
		RelevanceThresholdDelta: pgtype.Float4{Float32: delta, Valid: true},
		AutoRelevanceEnabled:    pgtype.Bool{Bool: autoEnabled, Valid: true},
	})
}
