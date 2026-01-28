package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/lueurxax/telegram-digest-bot/internal/storage/sqlc"
)

func (db *DB) SaveRelevanceGateLog(ctx context.Context, rawMsgID string, decision string, confidence *float32, reason, model, gateVersion string) error {
	conf := pgtype.Float4{Valid: false}
	if confidence != nil {
		conf = pgtype.Float4{Float32: *confidence, Valid: true}
	}

	err := db.Queries.SaveRelevanceGateLog(ctx, sqlc.SaveRelevanceGateLogParams{
		RawMessageID: toUUID(rawMsgID),
		Decision:     SanitizeUTF8(decision),
		Confidence:   conf,
		Reason:       toText(reason),
		Model:        toText(model),
		GateVersion:  toText(gateVersion),
	})
	if err != nil {
		return fmt.Errorf("save relevance gate log: %w", err)
	}

	return nil
}
