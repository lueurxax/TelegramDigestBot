-- +goose Up
-- +goose StatementBegin
ALTER TABLE evidence_sources ADD COLUMN extraction_failed BOOLEAN NOT NULL DEFAULT FALSE;
CREATE INDEX evidence_sources_extraction_failed_idx ON evidence_sources (extraction_failed) WHERE extraction_failed = TRUE;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS evidence_sources_extraction_failed_idx;
ALTER TABLE evidence_sources DROP COLUMN IF EXISTS extraction_failed;
-- +goose StatementEnd
