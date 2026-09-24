-- +goose Up
CREATE TABLE nodes (
  id                TEXT PRIMARY KEY,
  account_id        TEXT NOT NULL REFERENCES accounts(id),
  hostname          TEXT NOT NULL,
  arch              TEXT NOT NULL,
  cpu_flags         TEXT[] NOT NULL DEFAULT '{}',
  capabilities      JSONB NOT NULL DEFAULT '{}',   -- probe results
  offer_cores       NUMERIC(6,2) NOT NULL DEFAULT 0,
  offer_memory_mb   INT NOT NULL DEFAULT 0,
  offer_disk_mb     INT NOT NULL DEFAULT 0,
  bench_score       NUMERIC(8,3),                  -- relative to reference = 1.000
  trust_score       NUMERIC(4,3) NOT NULL DEFAULT 0.500,
  connected         BOOLEAN NOT NULL DEFAULT false,
  last_heartbeat_at TIMESTAMPTZ,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX nodes_available ON nodes (connected) WHERE connected;

-- +goose Down
DROP TABLE nodes;
