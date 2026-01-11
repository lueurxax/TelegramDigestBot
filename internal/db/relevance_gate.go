package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

func (db *DB) SaveRelevanceGateLog(ctx context.Context, rawMsgID string, decision string, confidence *float32, reason, model, gateVersion string) error {
	conf := pgtype.Float4{Valid: false}
	if confidence != nil {
		conf = pgtype.Float4{Float32: *confidence, Valid: true}
	}

	_, err := db.Pool.Exec(ctx, `
		INSERT INTO relevance_gate_log (raw_message_id, decision, confidence, reason, model, gate_version)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, toUUID(rawMsgID), decision, conf, toText(reason), toText(model), toText(gateVersion))
	return err
}
