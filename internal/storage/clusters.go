package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/lueurxax/telegram-digest-bot/internal/storage/sqlc"
)

type ClusterWithItems struct {
	ID    string
	Topic string
	Items []Item
}

const (
	ClusterSourceDigest   = "digest"
	ClusterSourceResearch = "research"
)

func (db *DB) CreateCluster(ctx context.Context, start, end time.Time, topic string) (string, error) {
	return db.CreateClusterWithSource(ctx, start, end, topic, ClusterSourceDigest)
}

func (db *DB) CreateClusterWithSource(ctx context.Context, start, end time.Time, topic, source string) (string, error) {
	row := db.Pool.QueryRow(ctx, `
		INSERT INTO clusters (window_start, window_end, topic, source)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, toTimestamptz(start), toTimestamptz(end), toText(topic), toText(source))

	var id pgtype.UUID
	if err := row.Scan(&id); err != nil {
		return "", fmt.Errorf("create cluster: %w", err)
	}

	return fromUUID(id), nil
}

func (db *DB) DeleteClustersForWindow(ctx context.Context, start, end time.Time) error {
	return db.DeleteClustersForWindowAndSource(ctx, start, end, ClusterSourceDigest)
}

func (db *DB) DeleteClustersForWindowAndSource(ctx context.Context, start, end time.Time, source string) error {
	if _, err := db.Pool.Exec(ctx, `
		DELETE FROM clusters WHERE window_start = $1 AND window_end = $2 AND source = $3
	`, toTimestamptz(start), toTimestamptz(end), toText(source)); err != nil {
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
	rows, err := db.Pool.Query(ctx, `
		SELECT c.id as cluster_id, c.topic as cluster_topic, i.id as item_id, i.summary as item_summary,
		       ch.username as channel_username, ch.tg_peer_id as channel_peer_id, rm.tg_message_id as rm_msg_id
		FROM clusters c
		JOIN cluster_items ci ON c.id = ci.cluster_id
		JOIN items i ON ci.item_id = i.id
		JOIN raw_messages rm ON i.raw_message_id = rm.id
		JOIN channels ch ON rm.channel_id = ch.id
		WHERE c.window_start = $1 AND c.window_end = $2 AND c.source = $3
		ORDER BY c.id
	`, toTimestamptz(start), toTimestamptz(end), toText(ClusterSourceDigest))
	if err != nil {
		return nil, fmt.Errorf("get clusters for window: %w", err)
	}
	defer rows.Close()

	clusterMap := make(map[string]*ClusterWithItems)

	var clusters []string // to keep order

	for rows.Next() {
		var (
			clusterID       pgtype.UUID
			clusterTopic    pgtype.Text
			itemID          pgtype.UUID
			itemSummary     pgtype.Text
			channelUsername pgtype.Text
			channelPeerID   int64
			rmMsgID         int64
		)

		if err := rows.Scan(&clusterID, &clusterTopic, &itemID, &itemSummary, &channelUsername, &channelPeerID, &rmMsgID); err != nil {
			return nil, fmt.Errorf("scan clusters for window: %w", err)
		}

		cID := fromUUID(clusterID)
		if _, ok := clusterMap[cID]; !ok {
			clusterMap[cID] = &ClusterWithItems{ID: cID, Topic: clusterTopic.String}
			clusters = append(clusters, cID)
		}

		clusterMap[cID].Items = append(clusterMap[cID].Items, Item{
			ID:              fromUUID(itemID),
			Summary:         itemSummary.String,
			SourceChannel:   channelUsername.String,
			SourceChannelID: channelPeerID,
			SourceMsgID:     rmMsgID,
		})
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate clusters for window: %w", rows.Err())
	}

	result := make([]ClusterWithItems, 0, len(clusters))
	for _, id := range clusters {
		result = append(result, *clusterMap[id])
	}

	return result, nil
}

// ClusterItemInfo is a simplified view of a cluster item for display.
type ClusterItemInfo struct {
	ID              string
	Summary         string
	Text            string // Full message text for maximum context prompts
	ChannelUsername string
	ChannelPeerID   int64
	MessageID       int64
}

// GetClusterForItem returns the cluster containing the given item, along with all items in that cluster.
func (db *DB) GetClusterForItem(ctx context.Context, itemID string) (*ClusterWithItems, []ClusterItemInfo, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT c.id, c.topic, ci2.item_id, i.summary, rm.text, ch.username, ch.tg_peer_id, rm.tg_message_id
		FROM cluster_items ci
		JOIN clusters c ON ci.cluster_id = c.id
		JOIN cluster_items ci2 ON c.id = ci2.cluster_id
		JOIN items i ON ci2.item_id = i.id
		JOIN raw_messages rm ON i.raw_message_id = rm.id
		JOIN channels ch ON rm.channel_id = ch.id
		WHERE ci.item_id = $1
		ORDER BY i.importance_score DESC
	`, toUUID(itemID))
	if err != nil {
		return nil, nil, fmt.Errorf("get cluster for item: %w", err)
	}
	defer rows.Close()

	var cluster *ClusterWithItems

	var items []ClusterItemInfo

	seenItems := make(map[string]struct{})

	for rows.Next() {
		var (
			clusterIDRaw pgtype.UUID
			clusterTopic pgtype.Text
			itemIDRaw    pgtype.UUID
			summary      pgtype.Text
			text         pgtype.Text
			username     pgtype.Text
			peerID       pgtype.Int8
			msgID        pgtype.Int8
		)

		if err := rows.Scan(&clusterIDRaw, &clusterTopic, &itemIDRaw, &summary, &text, &username, &peerID, &msgID); err != nil {
			return nil, nil, fmt.Errorf("scan cluster item: %w", err)
		}

		if cluster == nil {
			cluster = &ClusterWithItems{
				ID:    fromUUID(clusterIDRaw),
				Topic: clusterTopic.String,
			}
		}

		iID := fromUUID(itemIDRaw)
		// Skip the item we're looking up (we don't want to show it in "related items")
		if iID != itemID {
			if _, ok := seenItems[iID]; ok {
				continue
			}

			seenItems[iID] = struct{}{}

			items = append(items, ClusterItemInfo{
				ID:              iID,
				Summary:         summary.String,
				Text:            text.String,
				ChannelUsername: username.String,
				ChannelPeerID:   peerID.Int64,
				MessageID:       msgID.Int64,
			})
		}
	}

	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate cluster rows: %w", err)
	}

	return cluster, items, nil
}
