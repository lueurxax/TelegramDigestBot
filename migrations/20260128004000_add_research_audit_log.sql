-- +goose Up
-- +goose StatementBegin
CREATE TABLE research_audit_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id BIGINT NOT NULL,
    route TEXT NOT NULL,
    status_code INT NOT NULL,
    ip_address TEXT,
    query_hash TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX research_audit_log_created_idx ON research_audit_log (created_at DESC);
CREATE INDEX research_audit_log_user_idx ON research_audit_log (user_id, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS research_audit_log_user_idx;
DROP INDEX IF EXISTS research_audit_log_created_idx;
DROP TABLE IF EXISTS research_audit_log;
-- +goose StatementEnd
