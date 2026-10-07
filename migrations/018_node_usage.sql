-- +goose Up
-- What each machine and its tasks are using, from the agent's heartbeat (display only: the
-- host controls the agent, so none of this feeds billing or trust). Samples arrive every ~15 s
-- and are folded straight into 5-minute buckets as running sums plus a sample count, so an
-- average is sum / samples and the table stays small without a separate rollup job.
--   node_usage          machine totals per bucket; pruned after 30 days
--   node_task_usage     each task's share of a machine per bucket (for the stacked hover); pruned after 30 days
--   node_usage_latest   the newest reading per machine, for the live gauges
--   task_usage_totals   permanent per-task summary, so a task's numbers outlive the buckets
CREATE TABLE node_usage (
  node_id        TEXT        NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  bucket         TIMESTAMPTZ NOT NULL,
  samples        INTEGER     NOT NULL DEFAULT 0,
  cpu_busy_sum   DOUBLE PRECISION NOT NULL DEFAULT 0,  -- host CPU busy fraction, summed
  cpu_count      INTEGER     NOT NULL DEFAULT 0,
  mem_used_sum   DOUBLE PRECISION NOT NULL DEFAULT 0,
  mem_total      BIGINT      NOT NULL DEFAULT 0,
  disk_used_sum  DOUBLE PRECISION NOT NULL DEFAULT 0,
  disk_total     BIGINT      NOT NULL DEFAULT 0,
  tasks_cpu_sum  DOUBLE PRECISION NOT NULL DEFAULT 0,  -- cores used by tasks, summed
  tasks_mem_sum  DOUBLE PRECISION NOT NULL DEFAULT 0,
  PRIMARY KEY (node_id, bucket)
);

CREATE TABLE node_task_usage (
  node_id  TEXT        NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  task_id  TEXT        NOT NULL,
  bucket   TIMESTAMPTZ NOT NULL,
  cpu_sum  DOUBLE PRECISION NOT NULL DEFAULT 0,
  mem_sum  DOUBLE PRECISION NOT NULL DEFAULT 0,
  PRIMARY KEY (node_id, task_id, bucket)
);
CREATE INDEX node_task_usage_bucket_idx ON node_task_usage (node_id, bucket);

CREATE TABLE node_usage_latest (
  node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
  at      TIMESTAMPTZ NOT NULL,
  sample  JSONB       NOT NULL
);

CREATE TABLE task_usage_totals (
  task_id           TEXT PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
  node_id           TEXT        NOT NULL,
  core_seconds      DOUBLE PRECISION NOT NULL DEFAULT 0,
  peak_memory_bytes BIGINT      NOT NULL DEFAULT 0,
  tunnel_to_gateway BIGINT      NOT NULL DEFAULT 0,
  tunnel_to_task    BIGINT      NOT NULL DEFAULT 0,
  samples           INTEGER     NOT NULL DEFAULT 0,
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE task_usage_totals;
DROP TABLE node_usage_latest;
DROP TABLE node_task_usage;
DROP TABLE node_usage;
