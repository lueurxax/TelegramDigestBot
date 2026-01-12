package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/lueurxax/telegram-digest-bot/internal/db/sqlc"
	"github.com/lueurxax/telegram-digest-bot/migrations"
	"github.com/pressly/goose/v3"
)

type DB struct {
	Pool    *pgxpool.Pool
	Queries *sqlc.Queries
}

func New(ctx context.Context, dsn string) (*DB, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}

	var pool *pgxpool.Pool
	for i := 0; i < 10; i++ {
		pool, err = pgxpool.NewWithConfig(ctx, config)
		if err == nil {
			if err = pool.Ping(ctx); err == nil {
				return &DB{Pool: pool, Queries: sqlc.New(pool)}, nil
			}
		}

		if pool != nil {
			pool.Close()
		}

		time.Sleep(2 * time.Second)
	}

	return nil, fmt.Errorf("failed to connect to database after retries: %w", err)
}

func (db *DB) Close() {
	db.Pool.Close()
}

const migrationLockID = 1000

func (db *DB) Migrate(ctx context.Context) error {
	conn, err := db.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	// Acquire blocking advisory lock to ensure only one migration runs at a time
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return err
	}

	defer func() {
		_, _ = conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", migrationLockID)
	}()

	dbSQL := stdlib.OpenDB(*db.Pool.Config().ConnConfig)

	defer func() {
		_ = dbSQL.Close()
	}()

	goose.SetBaseFS(migrations.FS)

	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}

	if err := goose.Up(dbSQL, "."); err != nil {
		return err
	}

	return nil
}

// Helpers

func toUUID(id string) pgtype.UUID {
	u, err := uuid.Parse(id)
	if err != nil {
		return pgtype.UUID{Valid: false}
	}

	return pgtype.UUID{Bytes: u, Valid: true}
}

func fromUUID(uid pgtype.UUID) string {
	if !uid.Valid {
		return ""
	}

	return uuid.UUID(uid.Bytes).String()
}

func toText(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}

func toTimestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: !t.IsZero()}
}

func toInt8(i int64) pgtype.Int8 {
	return pgtype.Int8{Int64: i, Valid: i != 0}
}

func toInt4(i int) pgtype.Int4 {
	return pgtype.Int4{Int32: int32(i), Valid: true}
}
