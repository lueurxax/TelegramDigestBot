package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/lueurxax/telegram-digest-bot/internal/storage/sqlc"
)

const defaultLockTTL = 5 * time.Minute

// TryAcquireAdvisoryLock is deprecated - use TryAcquireSchedulerLock instead.
// Advisory locks don't work reliably with connection pooling.
func (db *DB) TryAcquireAdvisoryLock(ctx context.Context, lockID int64) (bool, error) {
	acquired, err := db.Queries.TryAcquireAdvisoryLock(ctx, lockID)
	if err != nil {
		return false, fmt.Errorf("try acquire advisory lock: %w", err)
	}

	return acquired, nil
}

// ReleaseAdvisoryLock is deprecated - use ReleaseSchedulerLock instead.
func (db *DB) ReleaseAdvisoryLock(ctx context.Context, lockID int64) error {
	if err := db.Queries.ReleaseAdvisoryLock(ctx, lockID); err != nil {
		return fmt.Errorf("release advisory lock: %w", err)
	}

	return nil
}

// TryAcquireSchedulerLock tries to acquire a row-based lock.
// Returns true if acquired, false if already held by another holder.
// Uses TTL-based expiry to handle stale locks automatically.
func (db *DB) TryAcquireSchedulerLock(ctx context.Context, lockName, holderID string, ttl time.Duration) (bool, error) {
	if ttl == 0 {
		ttl = defaultLockTTL
	}

	ttlInterval := pgtype.Interval{
		Microseconds: ttl.Microseconds(),
		Valid:        true,
	}

	_, err := db.Queries.TryAcquireSchedulerLock(ctx, sqlc.TryAcquireSchedulerLockParams{
		LockName: lockName,
		HolderID: holderID,
		Column3:  ttlInterval,
	})
	if err != nil {
		// No rows returned means lock is held by another (non-expired) holder
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}

		return false, fmt.Errorf("try acquire scheduler lock: %w", err)
	}

	return true, nil
}

// ExtendSchedulerLock extends the TTL of a held lock (heartbeat).
func (db *DB) ExtendSchedulerLock(ctx context.Context, lockName, holderID string, ttl time.Duration) error {
	if ttl == 0 {
		ttl = defaultLockTTL
	}

	ttlInterval := pgtype.Interval{
		Microseconds: ttl.Microseconds(),
		Valid:        true,
	}

	if err := db.Queries.ExtendSchedulerLock(ctx, sqlc.ExtendSchedulerLockParams{
		LockName: lockName,
		HolderID: holderID,
		Column3:  ttlInterval,
	}); err != nil {
		return fmt.Errorf("extend scheduler lock: %w", err)
	}

	return nil
}

// ReleaseSchedulerLock releases a held lock.
func (db *DB) ReleaseSchedulerLock(ctx context.Context, lockName, holderID string) error {
	if err := db.Queries.ReleaseSchedulerLock(ctx, sqlc.ReleaseSchedulerLockParams{
		LockName: lockName,
		HolderID: holderID,
	}); err != nil {
		return fmt.Errorf("release scheduler lock: %w", err)
	}

	return nil
}
