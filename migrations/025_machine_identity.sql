-- +goose Up
-- One machine entry per physical machine: the agent keeps a stable id in the host directory it mounts, so a restart, an update or a
-- recreated container is the same machine (and keeps its trust) instead of a new row each time. NULL for agents that predate it.
ALTER TABLE nodes ADD COLUMN machine_id TEXT;
CREATE UNIQUE INDEX nodes_account_machine ON nodes (account_id, machine_id) WHERE machine_id IS NOT NULL;

-- +goose Down
DROP INDEX nodes_account_machine;
ALTER TABLE nodes DROP COLUMN machine_id;
