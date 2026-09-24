-- +goose Up
ALTER TABLE tasks ADD COLUMN tunnel_targets JSONB NOT NULL DEFAULT '[]';
-- [{gateway_id, hostname, port}] - the structured form of gateway_ids,
-- which stays around as a quick-query convenience list.

-- +goose Down
ALTER TABLE tasks DROP COLUMN tunnel_targets;
