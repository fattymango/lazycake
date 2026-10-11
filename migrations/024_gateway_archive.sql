-- +goose Up
-- A deleted gateway is archived, not dropped: its traffic history and the tasks that used it keep pointing at a real row.
ALTER TABLE gateways ADD COLUMN archived_at TIMESTAMPTZ;
-- The hash of the install token minted for this gateway, so deleting it can expire exactly that token (NULL for gateways created before this).
ALTER TABLE gateways ADD COLUMN token_hash BYTEA;

-- +goose Down
ALTER TABLE gateways DROP COLUMN token_hash;
ALTER TABLE gateways DROP COLUMN archived_at;
