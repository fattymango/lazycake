-- +goose Up
-- Network over time for machines and tasks (follow-up to 8.15).
--   node_task_usage.tunnel_out / tunnel_in   bytes the task moved through its tunnel during the bucket (deltas of the agent's cumulative counters)
--   task_usage_samples                       the task's own readings at heartbeat resolution (~15 s), for the task page's chart; pruned after 30 days
--   gateway_traffic (task_id, bucket) index  so a task's gateway-side traffic can be charted
ALTER TABLE node_task_usage
  ADD COLUMN tunnel_out BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN tunnel_in  BIGINT NOT NULL DEFAULT 0;

CREATE TABLE task_usage_samples (
  task_id      TEXT        NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  at           TIMESTAMPTZ NOT NULL,
  cpu_cores    DOUBLE PRECISION NOT NULL DEFAULT 0,
  memory_bytes BIGINT      NOT NULL DEFAULT 0,
  tunnel_out   BIGINT      NOT NULL DEFAULT 0,  -- bytes since the previous sample
  tunnel_in    BIGINT      NOT NULL DEFAULT 0,
  PRIMARY KEY (task_id, at)
);
CREATE INDEX task_usage_samples_at_idx ON task_usage_samples (at);

CREATE INDEX gateway_traffic_task_idx ON gateway_traffic (task_id, bucket);

-- +goose Down
DROP INDEX gateway_traffic_task_idx;
DROP TABLE task_usage_samples;
ALTER TABLE node_task_usage DROP COLUMN tunnel_in, DROP COLUMN tunnel_out;
