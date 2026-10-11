-- +goose Up
-- Which release each agent and gateway runs, as it last reported (empty = never reported: it predates version reporting).
ALTER TABLE nodes ADD COLUMN agent_version TEXT NOT NULL DEFAULT '';
ALTER TABLE gateways ADD COLUMN version TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE gateways DROP COLUMN version;
ALTER TABLE nodes DROP COLUMN agent_version;
