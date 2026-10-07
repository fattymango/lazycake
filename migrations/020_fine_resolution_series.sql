-- +goose Up
-- Short tasks and fresh machines need finer detail than 5-minute buckets, or their charts are a few dots.
--   task_gateway_samples   what one task moved through gateways, per 10-second bin; pruned after 30 days
--   node_usage_samples     one row per heartbeat (about every 15 s) per machine, with its tasks; pruned after 48 hours
CREATE TABLE task_gateway_samples (
  task_id        TEXT        NOT NULL,  -- no foreign key, like gateway_traffic: removed by the retention job
  at             TIMESTAMPTZ NOT NULL,
  bytes_to_local BIGINT      NOT NULL DEFAULT 0,
  bytes_to_task  BIGINT      NOT NULL DEFAULT 0,
  PRIMARY KEY (task_id, at)
);
CREATE INDEX task_gateway_samples_at_idx ON task_gateway_samples (at);

CREATE TABLE node_usage_samples (
  node_id       TEXT        NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  at            TIMESTAMPTZ NOT NULL,
  host_cpu_busy DOUBLE PRECISION NOT NULL DEFAULT 0,
  cpu_count     INTEGER     NOT NULL DEFAULT 0,
  mem_used      BIGINT      NOT NULL DEFAULT 0,
  mem_total     BIGINT      NOT NULL DEFAULT 0,
  disk_used     BIGINT      NOT NULL DEFAULT 0,
  disk_total    BIGINT      NOT NULL DEFAULT 0,
  tasks_cpu     DOUBLE PRECISION NOT NULL DEFAULT 0,
  tasks_mem     BIGINT      NOT NULL DEFAULT 0,
  tasks         JSONB       NOT NULL DEFAULT '[]',  -- [{"id","cpu","mem","out","in"}]
  PRIMARY KEY (node_id, at)
);
CREATE INDEX node_usage_samples_at_idx ON node_usage_samples (at);

-- +goose Down
DROP TABLE node_usage_samples;
DROP TABLE task_gateway_samples;
