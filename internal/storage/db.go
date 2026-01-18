package db

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/lueurxax/telegram-digest-bot/internal/storage/sqlc"
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
		return nil, fmt.Errorf("parse db config: %w", err)
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

		time.Sleep(ConnectionRetrySleep)
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
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	// Acquire blocking advisory lock to ensure only one migration runs at a time
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("acquire advisory lock: %w", err)
	}

	defer func() {
		//nolint:errcheck // advisory unlock in defer is best-effort, lock released on connection close anyway
		_, _ = conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", migrationLockID)
	}()

	dbSQL := stdlib.OpenDB(*db.Pool.Config().ConnConfig)

	defer func() {
		_ = dbSQL.Close()
	}()

	goose.SetBaseFS(migrations.FS)

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}

	if err := goose.Up(dbSQL, "."); err != nil {
		return fmt.Errorf("run migrations: %w", err)
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
	return pgtype.Text{String: SanitizeUTF8(s), Valid: s != ""}
}

func SanitizeUTF8(s string) string {
	if s == "" || utf8.ValidString(s) {
		return s
	}

	return strings.ToValidUTF8(s, "")
}

func toTimestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: !t.IsZero()}
}

func toTimestamptzPtr(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{Valid: false}
	}

	return pgtype.Timestamptz{Time: *t, Valid: true}
}

func toInt8(i int64) pgtype.Int8 {
	return pgtype.Int8{Int64: i, Valid: i != 0}
}

func toInt4(i int) pgtype.Int4 {
	return pgtype.Int4{Int32: safeIntToInt32(i), Valid: true}
}

// safeIntToInt32 safely converts int to int32, clamping to valid range.
func safeIntToInt32(i int) int32 {
	if i > math.MaxInt32 {
		return math.MaxInt32
	}

	if i < math.MinInt32 {
		return math.MinInt32
	}

	return int32(i)
}
