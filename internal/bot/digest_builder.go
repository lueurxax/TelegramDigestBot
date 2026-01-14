package bot

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

// DigestBuilder provides digest building capabilities for preview commands.
// This interface is implemented by output/digest.Scheduler.
type DigestBuilder interface {
	// BuildDigest builds a digest for the given time window.
	// Returns the formatted text, items included, clusters, and any error.
	// The fourth return value is package-private anomaly info (ignored by bot).
	BuildDigest(ctx context.Context, start, end time.Time, importanceThreshold float32, logger *zerolog.Logger) (string, []db.Item, []db.ClusterWithItems, any, error)
}
