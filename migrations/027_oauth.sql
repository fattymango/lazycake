-- +goose Up
-- OAuth 2.1 for the MCP server (see .claude/mcp.md): an AI client registers itself, a signed-in user approves it on the dashboard, and it gets
-- an access token (short) plus a refresh token (long, rotated on use). Tokens, like sessions and agent tokens, are stored only as hashes.
CREATE TABLE oauth_clients (
  client_id     TEXT PRIMARY KEY,
  client_name   TEXT NOT NULL,
  redirect_uris JSONB NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One row per approval that has not been exchanged yet; deleted when exchanged (single use) and short-lived.
CREATE TABLE oauth_codes (
  code_hash      BYTEA PRIMARY KEY,
  client_id      TEXT NOT NULL REFERENCES oauth_clients(client_id) ON DELETE CASCADE,
  account_id     TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  role           TEXT NOT NULL CHECK (role IN ('customer','provider')),
  redirect_uri   TEXT NOT NULL,
  code_challenge TEXT NOT NULL,
  expires_at     TIMESTAMPTZ NOT NULL
);

-- One row per connected app (a grant): the current access and refresh token hashes. Refreshing replaces both.
CREATE TABLE oauth_tokens (
  id                 TEXT PRIMARY KEY,
  access_hash        BYTEA NOT NULL UNIQUE,
  refresh_hash       BYTEA NOT NULL UNIQUE,
  client_id          TEXT NOT NULL REFERENCES oauth_clients(client_id) ON DELETE CASCADE,
  account_id         TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  role               TEXT NOT NULL CHECK (role IN ('customer','provider')),
  created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_used_at       TIMESTAMPTZ,
  access_expires_at  TIMESTAMPTZ NOT NULL,
  refresh_expires_at TIMESTAMPTZ NOT NULL,
  revoked_at         TIMESTAMPTZ
);
CREATE INDEX oauth_tokens_account ON oauth_tokens (account_id) WHERE revoked_at IS NULL;

-- +goose Down
DROP TABLE oauth_tokens;
DROP TABLE oauth_codes;
DROP TABLE oauth_clients;
