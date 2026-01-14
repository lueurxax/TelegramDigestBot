package db

import (
	"context"
	"fmt"
)

func (db *DB) TryAcquireAdvisoryLock(ctx context.Context, lockID int64) (bool, error) {
	acquired, err := db.Queries.TryAcquireAdvisoryLock(ctx, lockID)
	if err != nil {
		return false, fmt.Errorf("try acquire advisory lock: %w", err)
	}

	return acquired, nil
}

func (db *DB) ReleaseAdvisoryLock(ctx context.Context, lockID int64) error {
	if err := db.Queries.ReleaseAdvisoryLock(ctx, lockID); err != nil {
		return fmt.Errorf("release advisory lock: %w", err)
	}

	return nil
}
