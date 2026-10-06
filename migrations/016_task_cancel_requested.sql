-- +goose Up
-- When a customer asked for a task to be stopped. Distinguishes a customer's
-- stop (the host did real work until then and is paid for it) from a
-- coordinator-issued cancellation (never billed), and lets the scheduler
-- re-send the cancel if it arrives while the node is still pulling the image
-- and cancel rather than retry a task whose node died.
ALTER TABLE tasks ADD COLUMN cancel_requested_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE tasks DROP COLUMN cancel_requested_at;
