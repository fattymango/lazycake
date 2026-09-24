-- +goose Up
CREATE TABLE gateways (
  id           TEXT PRIMARY KEY,
  account_id   TEXT NOT NULL REFERENCES accounts(id),
  label        TEXT NOT NULL,
  -- Nullable: unknown at `lcctl gateway create` time, filled in the first
  -- time the gateway process actually connects and publishes its key
  -- (internal/tunnel/quic.Relay's GatewayRegistry).
  noise_pubkey BYTEA,
  services     JSONB NOT NULL DEFAULT '[]',  -- [{name, port}]
  connected    BOOLEAN NOT NULL DEFAULT false,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE gateways;
