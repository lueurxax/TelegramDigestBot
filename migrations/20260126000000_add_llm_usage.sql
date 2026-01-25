-- +goose Up
-- +goose StatementBegin

-- LLM usage tracking for cost monitoring and budget controls
CREATE TABLE llm_usage (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    date DATE NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    task TEXT NOT NULL,
    prompt_tokens INT NOT NULL DEFAULT 0,
    completion_tokens INT NOT NULL DEFAULT 0,
    request_count INT NOT NULL DEFAULT 0,
    cost_usd NUMERIC(12, 6) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX llm_usage_date_provider_model_task_idx ON llm_usage (date, provider, model, task);
CREATE INDEX llm_usage_date_idx ON llm_usage (date);
CREATE INDEX llm_usage_provider_idx ON llm_usage (provider);
CREATE INDEX llm_usage_cost_idx ON llm_usage (cost_usd) WHERE cost_usd > 0;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS llm_usage;
-- +goose StatementEnd
