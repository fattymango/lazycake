-- +goose Up
-- IMPLEMENTATION.md task 4.5: "Deduct a hold at dispatch, settle at
-- completion." A hold reserves part of a customer's balance against one
-- in-flight task without moving any money - available balance is
-- balance_micros minus the sum of that account's active holds. Settling
-- (task 4.4's Store.SettleTask) or abandoning/fencing/cancelling a task
-- always releases its hold, so a task holds at most once, ever.
CREATE TABLE task_holds (
  task_id       TEXT PRIMARY KEY REFERENCES tasks(id),
  account_id    TEXT NOT NULL REFERENCES accounts(id),
  amount_micros BIGINT NOT NULL CHECK (amount_micros >= 0),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX task_holds_account ON task_holds (account_id);

-- +goose Down
DROP TABLE task_holds;
