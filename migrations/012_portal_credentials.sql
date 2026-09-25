-- +goose Up
-- IMPLEMENTATION.md task 7.1: a human's login for one of the two portals.
-- One account can hold at most one set of portal credentials, fixed to
-- whichever portal it registered on (PLAN.md §2: role is not a per-request
-- choice) - account_id is the primary key, not a separate unique column,
-- so that's enforced structurally rather than by convention.
CREATE TABLE portal_credentials (
  account_id     TEXT PRIMARY KEY REFERENCES accounts(id),
  username       TEXT NOT NULL UNIQUE,
  password_hash  TEXT NOT NULL,
  role           TEXT NOT NULL CHECK (role IN ('customer','provider')),
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE portal_credentials;
