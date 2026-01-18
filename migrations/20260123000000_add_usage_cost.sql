-- +goose Up
-- +goose StatementBegin
ALTER TABLE enrichment_usage ADD COLUMN cost_usd NUMERIC(12, 6) NOT NULL DEFAULT 0;
CREATE INDEX enrichment_usage_cost_idx ON enrichment_usage (cost_usd) WHERE cost_usd > 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS enrichment_usage_cost_idx;
ALTER TABLE enrichment_usage DROP COLUMN IF EXISTS cost_usd;
-- +goose StatementEnd
