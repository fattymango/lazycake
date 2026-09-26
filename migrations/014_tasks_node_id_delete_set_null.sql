-- +goose Up
-- A node the provider portal deletes (see internal/coordinator/portalapi's
-- node-delete endpoint) must not be blocked by its own task history - a
-- task's own fields (image, exit_code, limits, etc.) are immutable and
-- meaningful long after the node that ran it is gone, so losing just the
-- FK link (node_id -> NULL) is correct; losing the whole task row is not.
ALTER TABLE tasks DROP CONSTRAINT tasks_node_id_fkey;
ALTER TABLE tasks ADD CONSTRAINT tasks_node_id_fkey
  FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE tasks DROP CONSTRAINT tasks_node_id_fkey;
ALTER TABLE tasks ADD CONSTRAINT tasks_node_id_fkey
  FOREIGN KEY (node_id) REFERENCES nodes(id);
