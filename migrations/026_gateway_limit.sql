-- +goose Up
-- The bandwidth cap (whole Mbps; 0 = unlimited) a gateway says it enforces, as reported when it connects. NULL = never reported (a gateway that
-- predates it), shown as "not reported" rather than guessed.
ALTER TABLE gateways ADD COLUMN max_mbps INTEGER;

-- +goose Down
ALTER TABLE gateways DROP COLUMN max_mbps;
