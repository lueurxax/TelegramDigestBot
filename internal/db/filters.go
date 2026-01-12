package db

import (
	"context"

	"github.com/lueurxax/telegram-digest-bot/internal/db/sqlc"
)

type Filter struct {
	ID       string
	Type     string
	Pattern  string
	IsActive bool
}

func (db *DB) GetActiveFilters(ctx context.Context) ([]Filter, error) {
	sqlcFilters, err := db.Queries.GetActiveFilters(ctx)
	if err != nil {
		return nil, err
	}

	filters := make([]Filter, len(sqlcFilters))

	for i, f := range sqlcFilters {
		filters[i] = Filter{
			ID:       fromUUID(f.ID),
			Type:     f.Type,
			Pattern:  f.Pattern,
			IsActive: f.IsActive,
		}
	}

	return filters, nil
}

func (db *DB) AddFilter(ctx context.Context, fType, pattern string) error {
	return db.Queries.AddFilter(ctx, sqlc.AddFilterParams{
		Type:    fType,
		Pattern: pattern,
	})
}

func (db *DB) DeactivateFilter(ctx context.Context, pattern string) error {
	return db.Queries.DeactivateFilter(ctx, pattern)
}
