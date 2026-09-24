-- +goose Up
CREATE TABLE accounts (
  id              TEXT PRIMARY KEY,
  name            TEXT NOT NULL,
  balance_micros  BIGINT NOT NULL DEFAULT 0,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE api_tokens (
  token_hash  BYTEA PRIMARY KEY,
  account_id  TEXT NOT NULL REFERENCES accounts(id),
  kind        TEXT NOT NULL CHECK (kind IN ('customer','agent','gateway')),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  revoked_at  TIMESTAMPTZ
);

-- +goose Down
DROP TABLE api_tokens;
DROP TABLE accounts;
