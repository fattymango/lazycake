-- +goose Up
-- The tunnel bandwidth a machine offers, in Mbit/s (0 = no limit was set). Enforced by the agent as a
-- rate limit on all its tunnel traffic; shown on the machine page.
ALTER TABLE nodes ADD COLUMN offer_network_mbps INT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE nodes DROP COLUMN offer_network_mbps;
