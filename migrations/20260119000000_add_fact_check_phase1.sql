CREATE TABLE fact_check_queue (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    item_id UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    claim TEXT NOT NULL,
    normalized_claim TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempt_count INT NOT NULL DEFAULT 0,
    error_message TEXT,
    next_retry_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX fact_check_queue_item_id_idx ON fact_check_queue (item_id);
CREATE INDEX fact_check_queue_pending_idx ON fact_check_queue (status, created_at) WHERE status = 'pending';
CREATE INDEX fact_check_queue_next_retry_idx ON fact_check_queue (status, next_retry_at) WHERE status = 'pending';

CREATE TABLE fact_check_cache (
    normalized_claim TEXT PRIMARY KEY,
    result_json JSONB NOT NULL,
    cached_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE item_fact_checks (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    item_id UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    claim TEXT NOT NULL,
    url TEXT NOT NULL,
    publisher TEXT,
    rating TEXT,
    matched_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX item_fact_checks_item_url_idx ON item_fact_checks (item_id, url);
CREATE INDEX item_fact_checks_item_id_idx ON item_fact_checks (item_id);
