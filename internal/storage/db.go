// Package db provides PostgreSQL database access for the telegram-digest-bot.
//
// This package contains:
//   - DB: Connection pool and query interface wrapper
//   - Repository methods for all domain entities (messages, items, channels, etc.)
//   - Migration support via goose
//   - Type conversions between Go and PostgreSQL types
//
// The package uses pgx for connection pooling and sqlc-generated queries
// for type-safe database operations.
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
	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/storage/sqlc"
	"github.com/lueurxax/telegram-digest-bot/migrations"
	"github.com/pressly/goose/v3"
)

// DB wraps a PostgreSQL connection pool and provides repository methods
// for all domain entities.
type DB struct {
	Pool    *pgxpool.Pool
	Queries *sqlc.Queries
	Logger  *zerolog.Logger
}

// PoolOptions configures the database connection pool.
type PoolOptions struct {
	MaxConns          int32
	MinConns          int32
	MaxConnIdleTime   time.Duration
	MaxConnLifetime   time.Duration
	HealthCheckPeriod time.Duration
}

// DefaultPoolOptions returns sensible default pool configuration.
func DefaultPoolOptions() PoolOptions {
	return PoolOptions{
		MaxConns:          defaultMaxConns,
		MinConns:          defaultMinConns,
		MaxConnIdleTime:   defaultMaxConnIdleTime,
		MaxConnLifetime:   defaultMaxConnLifetime,
		HealthCheckPeriod: defaultHealthCheckPeriod,
	}
}

// New creates a new database connection with default pool options.
func New(ctx context.Context, dsn string, logger *zerolog.Logger) (*DB, error) {
	return NewWithOptions(ctx, dsn, DefaultPoolOptions(), logger)
}

// NewWithOptions creates a new database connection with custom pool options.
func NewWithOptions(ctx context.Context, dsn string, opts PoolOptions, logger *zerolog.Logger) (*DB, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse db config: %w", err)
	}

	applyPoolOptions(config, opts)

	return connectWithRetries(ctx, config, logger)
}

// applyPoolOptions applies non-zero pool options to the config.
func applyPoolOptions(config *pgxpool.Config, opts PoolOptions) {
	if opts.MaxConns > 0 {
		config.MaxConns = opts.MaxConns
	}

	if opts.MinConns > 0 {
		config.MinConns = opts.MinConns
	}

	if opts.MaxConnIdleTime > 0 {
		config.MaxConnIdleTime = opts.MaxConnIdleTime
	}

	if opts.MaxConnLifetime > 0 {
		config.MaxConnLifetime = opts.MaxConnLifetime
	}

	if opts.HealthCheckPeriod > 0 {
		config.HealthCheckPeriod = opts.HealthCheckPeriod
	}
}

// connectWithRetries attempts to connect to the database with retries.
func connectWithRetries(ctx context.Context, config *pgxpool.Config, logger *zerolog.Logger) (*DB, error) {
	var pool *pgxpool.Pool

	var err error

	for i := 0; i < maxConnectionRetries; i++ {
		pool, err = pgxpool.NewWithConfig(ctx, config)
		if err == nil {
			if err = pool.Ping(ctx); err == nil {
				return &DB{Pool: pool, Queries: sqlc.New(pool), Logger: logger}, nil
			}
		}

		if pool != nil {
			pool.Close()
		}

		time.Sleep(ConnectionRetrySleep)
	}

	return nil, fmt.Errorf("failed to connect to database after retries: %w", err)
}

// Close closes the database connection pool.
func (db *DB) Close() {
	db.Pool.Close()
}

const migrationLockID = 1000

type gooseLogger struct {
	logger *zerolog.Logger
}

func (l *gooseLogger) Fatalf(format string, v ...interface{}) {
	l.logger.Fatal().Msgf(format, v...)
}

func (l *gooseLogger) Printf(format string, v ...interface{}) {
	l.logger.Info().Msgf(format, v...)
}

// Migrate runs database migrations using goose.
// It acquires an advisory lock to ensure only one migration runs at a time
// across multiple instances.
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
	goose.SetLogger(&gooseLogger{logger: db.Logger})

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

// SanitizeUTF8 removes invalid UTF-8 sequences from a string.
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

func toFloat4(f float32) pgtype.Float4 {
	return pgtype.Float4{Float32: f, Valid: true}
}

func fromFloat4(f pgtype.Float4) float32 {
	if !f.Valid {
		return 0
	}

	return f.Float32
}

func fromText(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}

	return t.String
}

func fromTimestamptz(t pgtype.Timestamptz) time.Time {
	if !t.Valid {
		return time.Time{}
	}

	return t.Time
}
