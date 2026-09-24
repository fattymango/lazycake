-- +goose Up
CREATE TABLE task_logs (
  task_id   TEXT NOT NULL REFERENCES tasks(id),
  seq       BIGINT NOT NULL,
  stream    TEXT NOT NULL CHECK (stream IN ('stdout','stderr')),
  at        TIMESTAMPTZ NOT NULL,
  line      TEXT NOT NULL,
  PRIMARY KEY (task_id, seq)
);

-- +goose Down
DROP TABLE task_logs;
