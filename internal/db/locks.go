package db

import (
	"context"
)

func (db *DB) TryAcquireAdvisoryLock(ctx context.Context, lockID int64) (bool, error) {
	return db.Queries.TryAcquireAdvisoryLock(ctx, lockID)
}

func (db *DB) ReleaseAdvisoryLock(ctx context.Context, lockID int64) error {
	return db.Queries.ReleaseAdvisoryLock(ctx, lockID)
}
