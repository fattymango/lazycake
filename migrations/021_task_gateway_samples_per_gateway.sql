-- +goose Up
-- Which gateway moved the bytes, so a task's chart can show each gateway's own consumption.
-- Rows written before this migration have no gateway ('') and show as an unnamed gateway.
ALTER TABLE task_gateway_samples ADD COLUMN gateway_id TEXT NOT NULL DEFAULT '';
ALTER TABLE task_gateway_samples DROP CONSTRAINT task_gateway_samples_pkey;
ALTER TABLE task_gateway_samples ADD PRIMARY KEY (task_id, gateway_id, at);

-- +goose Down
DELETE FROM task_gateway_samples a USING task_gateway_samples b
  WHERE a.task_id = b.task_id AND a.at = b.at AND a.gateway_id > b.gateway_id;
ALTER TABLE task_gateway_samples DROP CONSTRAINT task_gateway_samples_pkey;
ALTER TABLE task_gateway_samples DROP COLUMN gateway_id;
ALTER TABLE task_gateway_samples ADD PRIMARY KEY (task_id, at);
