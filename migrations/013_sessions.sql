-- +goose Up
-- IMPLEMENTATION.md task 7.1: a portal login session, looked up by the
-- hash of a random ID handed to the browser as an httpOnly cookie - same
-- pattern api_tokens already uses (PLAN.md §2), so a stolen database
-- backup never yields a usable session any more than it yields a usable
-- bearer token.
CREATE TABLE sessions (
  id_hash     BYTEA PRIMARY KEY,
  account_id  TEXT NOT NULL REFERENCES accounts(id),
  role        TEXT NOT NULL CHECK (role IN ('customer','provider')),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at  TIMESTAMPTZ NOT NULL,
  revoked_at  TIMESTAMPTZ
);
CREATE INDEX sessions_account_id_idx ON sessions(account_id);

-- +goose Down
DROP TABLE sessions;
