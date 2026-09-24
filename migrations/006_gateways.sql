-- +goose Up
CREATE TABLE gateways (
  id           TEXT PRIMARY KEY,
  account_id   TEXT NOT NULL REFERENCES accounts(id),
  label        TEXT NOT NULL,
  noise_pubkey BYTEA NOT NULL,
  services     JSONB NOT NULL DEFAULT '[]',  -- [{name, port}]
  connected    BOOLEAN NOT NULL DEFAULT false,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE gateways;
