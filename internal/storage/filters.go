package db

import (
	"context"
	"fmt"

	"github.com/lueurxax/telegram-digest-bot/internal/storage/sqlc"
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
		return nil, fmt.Errorf("get active filters: %w", err)
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
	if err := db.Queries.AddFilter(ctx, sqlc.AddFilterParams{
		Type:    SanitizeUTF8(fType),
		Pattern: SanitizeUTF8(pattern),
	}); err != nil {
		return fmt.Errorf("add filter: %w", err)
	}

	return nil
}

func (db *DB) DeactivateFilter(ctx context.Context, pattern string) error {
	if err := db.Queries.DeactivateFilter(ctx, SanitizeUTF8(pattern)); err != nil {
		return fmt.Errorf("deactivate filter: %w", err)
	}

	return nil
}
