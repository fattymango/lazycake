-- +goose Up
-- Same fix as 014, for the one other table left with a blocking FK to
-- nodes(id): a task_meters row (duration_s/normalised_s, used for
-- billing) is meaningful long after the node that ran it is deleted -
-- losing just the FK link (node_id -> NULL) is correct, losing the
-- meter row itself is not. Caught live: deleting a node with any task
-- history failed with "violates foreign key constraint
-- task_meters_node_id_fkey".
ALTER TABLE task_meters ALTER COLUMN node_id DROP NOT NULL;
ALTER TABLE task_meters DROP CONSTRAINT task_meters_node_id_fkey;
ALTER TABLE task_meters ADD CONSTRAINT task_meters_node_id_fkey
  FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE task_meters DROP CONSTRAINT task_meters_node_id_fkey;
ALTER TABLE task_meters ADD CONSTRAINT task_meters_node_id_fkey
  FOREIGN KEY (node_id) REFERENCES nodes(id);
ALTER TABLE task_meters ALTER COLUMN node_id SET NOT NULL;
