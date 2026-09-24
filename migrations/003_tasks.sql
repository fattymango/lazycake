-- +goose Up
CREATE TYPE task_state AS ENUM (
  'queued','reserved','dispatched','running',
  'succeeded','failed','fenced','abandoned','cancelled'
);

CREATE TABLE tasks (
  id                TEXT PRIMARY KEY,
  account_id        TEXT NOT NULL REFERENCES accounts(id),
  idempotency_key   TEXT,
  state             task_state NOT NULL DEFAULT 'queued',

  image             TEXT NOT NULL,           -- must contain '@sha256:'
  entrypoint        TEXT[],
  args              TEXT[],
  env               JSONB NOT NULL DEFAULT '{}',
  workdir           TEXT,

  cpu_cores         NUMERIC(6,2) NOT NULL,
  memory_mb         INT NOT NULL,
  disk_mb           INT NOT NULL,
  tmpfs_mb          INT NOT NULL DEFAULT 0,
  pids_limit        INT NOT NULL DEFAULT 256,
  wall_timeout_s    INT NOT NULL,
  no_output_timeout_s INT NOT NULL DEFAULT 0,
  egress_mb         INT NOT NULL DEFAULT 0,

  req_arch          TEXT NOT NULL DEFAULT 'amd64',
  req_cpu_flags     TEXT[] NOT NULL DEFAULT '{}',
  req_isolation     TEXT NOT NULL DEFAULT 'podman',
  req_confidentiality TEXT NOT NULL DEFAULT 'none',

  gateway_ids       TEXT[] NOT NULL DEFAULT '{}',
  delivery          TEXT NOT NULL DEFAULT 'at_most_once',
  max_attempts      INT NOT NULL DEFAULT 1,
  attempt           INT NOT NULL DEFAULT 0,

  node_id           TEXT REFERENCES nodes(id),
  lease_expires_at  TIMESTAMPTZ,
  requeue_after     TIMESTAMPTZ,

  exit_code         INT,
  exit_reason       TEXT,     -- 'exited','oom','disk','wall_timeout','no_output','fenced','abandoned'
  started_at        TIMESTAMPTZ,
  finished_at       TIMESTAMPTZ,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX tasks_idem ON tasks (account_id, idempotency_key)
  WHERE idempotency_key IS NOT NULL;
CREATE INDEX tasks_queue ON tasks (state, created_at) WHERE state = 'queued';
CREATE INDEX tasks_reclaim ON tasks (requeue_after)
  WHERE state IN ('reserved','dispatched','running');

-- +goose Down
DROP TABLE tasks;
DROP TYPE task_state;
