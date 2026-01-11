-- +goose Up
-- +goose StatementBegin
ALTER TABLE channels ADD COLUMN importance_weight FLOAT4 DEFAULT 1.0;
ALTER TABLE channels ADD COLUMN auto_weight_enabled BOOLEAN DEFAULT TRUE;
ALTER TABLE channels ADD COLUMN weight_override BOOLEAN DEFAULT FALSE;
ALTER TABLE channels ADD COLUMN weight_override_reason TEXT;
ALTER TABLE channels ADD COLUMN weight_updated_at TIMESTAMPTZ;
ALTER TABLE channels ADD COLUMN weight_updated_by BIGINT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE channels DROP COLUMN IF EXISTS weight_updated_by;
ALTER TABLE channels DROP COLUMN IF EXISTS weight_updated_at;
ALTER TABLE channels DROP COLUMN IF EXISTS weight_override_reason;
ALTER TABLE channels DROP COLUMN IF EXISTS weight_override;
ALTER TABLE channels DROP COLUMN IF EXISTS auto_weight_enabled;
ALTER TABLE channels DROP COLUMN IF EXISTS importance_weight;
-- +goose StatementEnd
