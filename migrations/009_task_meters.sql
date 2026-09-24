-- +goose Up
-- One row per task, populated purely from the coordinator's own receive
-- clock (IMPLEMENTATION.md task 4.2) - never from agent-reported
-- timestamps, which a dishonest or clock-skewed host could inflate or
-- deflate to change what it gets billed or credited.
CREATE TABLE task_meters (
  task_id      TEXT PRIMARY KEY REFERENCES tasks(id),
  node_id      TEXT NOT NULL REFERENCES nodes(id),
  started_at   TIMESTAMPTZ NOT NULL,
  finished_at  TIMESTAMPTZ,
  duration_s   NUMERIC(12,3),
  normalised_s NUMERIC(12,3)
);

-- +goose Down
DROP TABLE task_meters;
