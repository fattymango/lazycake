-- +goose Up
CREATE TABLE node_images (
  node_id    TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  digest     TEXT NOT NULL,
  size_bytes BIGINT NOT NULL,
  last_used  TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (node_id, digest)
);

-- +goose Down
DROP TABLE node_images;
