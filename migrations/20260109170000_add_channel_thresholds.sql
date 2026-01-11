-- +goose Up
-- +goose StatementBegin
ALTER TABLE channels ADD COLUMN IF NOT EXISTS relevance_threshold FLOAT4;
ALTER TABLE channels ADD COLUMN IF NOT EXISTS importance_threshold FLOAT4;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE channels DROP COLUMN IF EXISTS relevance_threshold;
ALTER TABLE channels DROP COLUMN IF EXISTS importance_threshold;
-- +goose StatementEnd
