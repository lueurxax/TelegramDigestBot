-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS filters (
  id      UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  type    TEXT NOT NULL, -- allow|deny
  pattern TEXT NOT NULL,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS filters_active_idx ON filters (is_active);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS filters;
-- +goose StatementEnd
