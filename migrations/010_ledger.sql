-- +goose Up
-- IMPLEMENTATION.md task 4.4: every completed, billable task writes one
-- charge row (negative amount_micros) against the customer and one credit
-- row (positive amount_micros) against the host, in the same transaction
-- that updates both balances - so summing an account's own rows always
-- reconciles against its balance history.
CREATE TABLE ledger_entries (
  id            TEXT PRIMARY KEY,
  task_id       TEXT NOT NULL REFERENCES tasks(id),
  account_id    TEXT NOT NULL REFERENCES accounts(id),
  kind          TEXT NOT NULL CHECK (kind IN ('charge','credit')),
  amount_micros BIGINT NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ledger_entries_account ON ledger_entries (account_id);
CREATE INDEX ledger_entries_task ON ledger_entries (task_id);

-- +goose Down
DROP TABLE ledger_entries;
