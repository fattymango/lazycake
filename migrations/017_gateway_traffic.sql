-- +goose Up
-- What each gateway moved for each task, from the gateway's own byte reports. Three tables
-- because they live for different lengths of time:
--   gateway_traffic       5-minute buckets, for charts and "busiest tasks"; pruned after 90 days
--   gateway_totals        a gateway's lifetime totals; permanent and tiny
--   task_gateway_totals   what one task moved through one gateway's service; permanent and tiny
-- bytes_to_local is what the gateway sent to the customer's own service (data coming from the
-- task); bytes_to_task is what came back from the service to the task.
CREATE TABLE gateway_traffic (
  gateway_id     TEXT        NOT NULL REFERENCES gateways(id) ON DELETE CASCADE,
  task_id        TEXT        NOT NULL,
  service        TEXT        NOT NULL,
  bucket         TIMESTAMPTZ NOT NULL,
  bytes_to_local BIGINT      NOT NULL DEFAULT 0,
  bytes_to_task  BIGINT      NOT NULL DEFAULT 0,
  connections    INTEGER     NOT NULL DEFAULT 0,
  PRIMARY KEY (gateway_id, task_id, service, bucket)
);
CREATE INDEX gateway_traffic_bucket_idx ON gateway_traffic (gateway_id, bucket);

CREATE TABLE gateway_totals (
  gateway_id     TEXT PRIMARY KEY REFERENCES gateways(id) ON DELETE CASCADE,
  bytes_to_local BIGINT      NOT NULL DEFAULT 0,
  bytes_to_task  BIGINT      NOT NULL DEFAULT 0,
  connections    BIGINT      NOT NULL DEFAULT 0,
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE task_gateway_totals (
  task_id        TEXT        NOT NULL,
  gateway_id     TEXT        NOT NULL REFERENCES gateways(id) ON DELETE CASCADE,
  service        TEXT        NOT NULL,
  bytes_to_local BIGINT      NOT NULL DEFAULT 0,
  bytes_to_task  BIGINT      NOT NULL DEFAULT 0,
  connections    BIGINT      NOT NULL DEFAULT 0,
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (task_id, gateway_id, service)
);

-- +goose Down
DROP TABLE task_gateway_totals;
DROP TABLE gateway_totals;
DROP TABLE gateway_traffic;
