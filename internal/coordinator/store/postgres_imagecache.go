package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) RecordCachedImages(ctx context.Context, nodeID string, images []CachedImage) error {
	if len(images) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, img := range images {
		batch.Queue(`
			INSERT INTO node_images (node_id, digest, size_bytes, last_used)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (node_id, digest) DO UPDATE SET
				size_bytes = EXCLUDED.size_bytes, last_used = EXCLUDED.last_used`,
			nodeID, img.Digest, img.SizeBytes, img.LastUsed)
	}
	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()
	for range images {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("recording cached images: %w", err)
		}
	}
	return nil
}

func (s *PostgresStore) RemoveCachedImages(ctx context.Context, nodeID string, digests []string) error {
	if len(digests) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`DELETE FROM node_images WHERE node_id = $1 AND digest = ANY($2)`, nodeID, digests)
	if err != nil {
		return fmt.Errorf("removing cached images: %w", err)
	}
	return nil
}

func (s *PostgresStore) ListCachedImages(ctx context.Context, nodeID string) ([]CachedImage, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT node_id, digest, size_bytes, last_used FROM node_images WHERE node_id = $1`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("listing cached images: %w", err)
	}
	defer rows.Close()

	var out []CachedImage
	for rows.Next() {
		var img CachedImage
		if err := rows.Scan(&img.NodeID, &img.Digest, &img.SizeBytes, &img.LastUsed); err != nil {
			return nil, fmt.Errorf("scanning cached image: %w", err)
		}
		out = append(out, img)
	}
	return out, rows.Err()
}

func (s *PostgresStore) NodesWithImage(ctx context.Context, digest string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT node_id FROM node_images WHERE digest = $1`, digest)
	if err != nil {
		return nil, fmt.Errorf("listing nodes with image: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning node id: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
