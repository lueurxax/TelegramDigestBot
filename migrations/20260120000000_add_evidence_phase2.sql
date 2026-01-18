-- +goose Up
-- +goose StatementBegin

-- Evidence sources: cached evidence documents from external providers
CREATE TABLE evidence_sources (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    url TEXT NOT NULL,
    url_hash TEXT NOT NULL,
    domain TEXT NOT NULL,
    title TEXT,
    description TEXT,
    content TEXT,
    author TEXT,
    published_at TIMESTAMPTZ,
    language TEXT,
    provider TEXT NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX evidence_sources_url_hash_idx ON evidence_sources (url_hash);
CREATE INDEX evidence_sources_domain_idx ON evidence_sources (domain);
CREATE INDEX evidence_sources_expires_at_idx ON evidence_sources (expires_at);

-- Evidence claims: extracted claims from evidence sources
CREATE TABLE evidence_claims (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    evidence_id UUID NOT NULL REFERENCES evidence_sources(id) ON DELETE CASCADE,
    claim_text TEXT NOT NULL,
    entities_json JSONB,
    embedding vector(1536),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX evidence_claims_evidence_id_idx ON evidence_claims (evidence_id);

-- Item evidence: links items to evidence with agreement scores
CREATE TABLE item_evidence (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    item_id UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    evidence_id UUID NOT NULL REFERENCES evidence_sources(id) ON DELETE CASCADE,
    agreement_score REAL NOT NULL DEFAULT 0,
    is_contradiction BOOLEAN NOT NULL DEFAULT FALSE,
    matched_claims_json JSONB,
    matched_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX item_evidence_item_evidence_idx ON item_evidence (item_id, evidence_id);
CREATE INDEX item_evidence_item_id_idx ON item_evidence (item_id);
CREATE INDEX item_evidence_agreement_idx ON item_evidence (agreement_score DESC);

-- Enrichment queue: work queue for enrichment processing (mirrors fact_check_queue)
CREATE TABLE enrichment_queue (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    item_id UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    summary TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempt_count INT NOT NULL DEFAULT 0,
    error_message TEXT,
    next_retry_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX enrichment_queue_item_id_idx ON enrichment_queue (item_id);
CREATE INDEX enrichment_queue_pending_idx ON enrichment_queue (status, created_at) WHERE status = 'pending';
CREATE INDEX enrichment_queue_next_retry_idx ON enrichment_queue (status, next_retry_at) WHERE status = 'pending';

-- Add fact check fields to items table
ALTER TABLE items ADD COLUMN IF NOT EXISTS fact_check_score REAL;
ALTER TABLE items ADD COLUMN IF NOT EXISTS fact_check_tier TEXT;
ALTER TABLE items ADD COLUMN IF NOT EXISTS fact_check_notes TEXT;

CREATE INDEX items_fact_check_tier_idx ON items (fact_check_tier) WHERE fact_check_tier IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS items_fact_check_tier_idx;
ALTER TABLE items DROP COLUMN IF EXISTS fact_check_notes;
ALTER TABLE items DROP COLUMN IF EXISTS fact_check_tier;
ALTER TABLE items DROP COLUMN IF EXISTS fact_check_score;
DROP TABLE IF EXISTS enrichment_queue;
DROP TABLE IF EXISTS item_evidence;
DROP TABLE IF EXISTS evidence_claims;
DROP TABLE IF EXISTS evidence_sources;
-- +goose StatementEnd
