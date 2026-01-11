-- +goose Up
-- +goose StatementBegin
DROP INDEX IF EXISTS channels_peer_id_uq;
CREATE UNIQUE INDEX channels_peer_id_uq ON channels (tg_peer_id) WHERE tg_peer_id != 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS channels_peer_id_uq;
CREATE UNIQUE INDEX channels_peer_id_uq ON channels (tg_peer_id);
-- +goose StatementEnd
