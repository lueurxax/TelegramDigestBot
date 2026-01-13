package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/lueurxax/telegram-digest-bot/internal/db/sqlc"
)

func (db *DB) SaveSetting(ctx context.Context, key string, value interface{}) error {
	return db.SaveSettingWithHistory(ctx, key, value, 0)
}

func (db *DB) SaveSettingWithHistory(ctx context.Context, key string, value interface{}, changedBy int64) error {
	// Get old value
	var oldVal []byte

	rawOld, err := db.Queries.GetSetting(ctx, key)
	if err == nil {
		oldVal = rawOld
	}

	val, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal setting value: %w", err)
	}

	if err := db.Queries.SaveSetting(ctx, sqlc.SaveSettingParams{
		Key:   key,
		Value: val,
	}); err != nil {
		return fmt.Errorf("failed to save setting to DB: %w", err)
	}

	// Only add history if changedBy is provided
	if changedBy != 0 {
		//nolint:errcheck // history logging is best-effort, should not fail the main operation
		_ = db.Queries.AddSettingHistory(ctx, sqlc.AddSettingHistoryParams{
			Key:       key,
			OldValue:  pgtype.Text{String: string(oldVal), Valid: len(oldVal) > 0},
			NewValue:  pgtype.Text{String: string(val), Valid: true},
			ChangedBy: changedBy,
		})
	}

	return nil
}

func (db *DB) GetSetting(ctx context.Context, key string, target interface{}) error {
	val, err := db.Queries.GetSetting(ctx, key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}

		return fmt.Errorf("failed to get setting from DB: %w", err)
	}

	if err := json.Unmarshal(val, target); err != nil {
		return fmt.Errorf("failed to unmarshal setting value: %w", err)
	}

	return nil
}

func (db *DB) DeleteSetting(ctx context.Context, key string) error {
	return db.DeleteSettingWithHistory(ctx, key, 0)
}

func (db *DB) DeleteSettingWithHistory(ctx context.Context, key string, changedBy int64) error {
	// Get old value
	var oldVal []byte

	rawOld, err := db.Queries.GetSetting(ctx, key)
	if err == nil {
		oldVal = rawOld
	}

	if err := db.Queries.DeleteSetting(ctx, key); err != nil {
		return fmt.Errorf("failed to delete setting from DB: %w", err)
	}

	// Only add history if changedBy is provided
	if changedBy != 0 {
		//nolint:errcheck // history logging is best-effort, should not fail the main operation
		_ = db.Queries.AddSettingHistory(ctx, sqlc.AddSettingHistoryParams{
			Key:       key,
			OldValue:  pgtype.Text{String: string(oldVal), Valid: len(oldVal) > 0},
			NewValue:  pgtype.Text{String: "", Valid: false}, // NULL indicates deletion
			ChangedBy: changedBy,
		})
	}

	return nil
}

type SettingHistory struct {
	Key       string
	OldValue  string
	NewValue  string
	ChangedBy int64
	ChangedAt time.Time
}

func (db *DB) GetRecentSettingHistory(ctx context.Context, limit int) ([]SettingHistory, error) {
	rows, err := db.Queries.GetRecentSettingHistory(ctx, safeIntToInt32(limit))
	if err != nil {
		return nil, fmt.Errorf("failed to get setting history: %w", err)
	}

	res := make([]SettingHistory, len(rows))

	for i, row := range rows {
		res[i] = SettingHistory{
			Key:       row.Key,
			OldValue:  row.OldValue.String,
			NewValue:  row.NewValue.String,
			ChangedBy: row.ChangedBy,
			ChangedAt: row.ChangedAt.Time,
		}
	}

	return res, nil
}

func (db *DB) GetAllSettings(ctx context.Context) (map[string]interface{}, error) {
	rows, err := db.Queries.GetAllSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get all settings from DB: %w", err)
	}

	res := make(map[string]interface{})

	for _, row := range rows {
		var val interface{}

		if err := json.Unmarshal(row.Value, &val); err != nil {
			continue
		}

		res[row.Key] = val
	}

	return res, nil
}
