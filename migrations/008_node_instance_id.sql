-- +goose Up
ALTER TABLE nodes ADD COLUMN instance_id TEXT;

-- One row per (account, instance): lets a reconnecting agent process (same
-- instance_id for as long as it stays up) be recognised as the same node
-- rather than minting a fresh node_id - and with it a fresh, empty set of
-- assigned tasks - on every reconnect (IMPLEMENTATION.md task 3.3).
CREATE UNIQUE INDEX nodes_account_instance ON nodes (account_id, instance_id) WHERE instance_id IS NOT NULL;

-- +goose Down
DROP INDEX nodes_account_instance;
ALTER TABLE nodes DROP COLUMN instance_id;
