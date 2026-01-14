package db

import (
	"context"
	"fmt"
	"time"

	"github.com/lueurxax/telegram-digest-bot/internal/storage/sqlc"
)

type ClusterWithItems struct {
	ID    string
	Topic string
	Items []Item
}

func (db *DB) CreateCluster(ctx context.Context, start, end time.Time, topic string) (string, error) {
	id, err := db.Queries.CreateCluster(ctx, sqlc.CreateClusterParams{
		WindowStart: toTimestamptz(start),
		WindowEnd:   toTimestamptz(end),
		Topic:       toText(topic),
	})
	if err != nil {
		return "", fmt.Errorf("create cluster: %w", err)
	}

	return fromUUID(id), nil
}

func (db *DB) DeleteClustersForWindow(ctx context.Context, start, end time.Time) error {
	if err := db.Queries.DeleteClustersForWindow(ctx, sqlc.DeleteClustersForWindowParams{
		WindowStart: toTimestamptz(start),
		WindowEnd:   toTimestamptz(end),
	}); err != nil {
		return fmt.Errorf("delete clusters for window: %w", err)
	}

	return nil
}

func (db *DB) AddToCluster(ctx context.Context, clusterID, itemID string) error {
	if err := db.Queries.AddToCluster(ctx, sqlc.AddToClusterParams{
		ClusterID: toUUID(clusterID),
		ItemID:    toUUID(itemID),
	}); err != nil {
		return fmt.Errorf("add to cluster: %w", err)
	}

	return nil
}

func (db *DB) GetClustersForWindow(ctx context.Context, start, end time.Time) ([]ClusterWithItems, error) {
	sqlcRows, err := db.Queries.GetClustersForWindow(ctx, sqlc.GetClustersForWindowParams{
		WindowStart: toTimestamptz(start),
		WindowEnd:   toTimestamptz(end),
	})
	if err != nil {
		return nil, fmt.Errorf("get clusters for window: %w", err)
	}

	clusterMap := make(map[string]*ClusterWithItems)

	var clusters []string // to keep order

	for _, row := range sqlcRows {
		cID := fromUUID(row.ClusterID)
		if _, ok := clusterMap[cID]; !ok {
			clusterMap[cID] = &ClusterWithItems{ID: cID, Topic: row.ClusterTopic.String}
			clusters = append(clusters, cID)
		}

		clusterMap[cID].Items = append(clusterMap[cID].Items, Item{
			ID:              fromUUID(row.ItemID),
			Summary:         row.ItemSummary.String,
			SourceChannel:   row.ChannelUsername.String,
			SourceChannelID: row.ChannelPeerID,
			SourceMsgID:     row.RmMsgID,
		})
	}

	result := make([]ClusterWithItems, 0, len(clusters))
	for _, id := range clusters {
		result = append(result, *clusterMap[id])
	}

	return result, nil
}
