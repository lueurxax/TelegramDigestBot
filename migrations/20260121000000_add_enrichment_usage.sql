-- +goose Up
-- +goose StatementBegin

-- Enrichment usage tracking for budget controls
CREATE TABLE enrichment_usage (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    date DATE NOT NULL,
    provider TEXT NOT NULL,
    request_count INT NOT NULL DEFAULT 0,
    embedding_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX enrichment_usage_date_provider_idx ON enrichment_usage (date, provider);
CREATE INDEX enrichment_usage_date_idx ON enrichment_usage (date);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS enrichment_usage;
-- +goose StatementEnd
