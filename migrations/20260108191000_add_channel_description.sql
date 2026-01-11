-- +goose Up
-- +goose StatementBegin
ALTER TABLE channels ADD COLUMN description TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE channels DROP COLUMN description;
-- +goose StatementEnd
