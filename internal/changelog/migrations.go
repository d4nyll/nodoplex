package changelog

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RunMigrations creates the changelog table and indexes if they don't exist.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS changelog (
			change_id   BIGSERIAL PRIMARY KEY,
			action      TEXT NOT NULL,
			node_type   TEXT NOT NULL,
			node_id     TEXT NOT NULL,
			node        JSONB,
			previous    JSONB,
			timestamp   TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_changelog_type_id ON changelog (node_type, change_id)`,
	}

	for _, q := range queries {
		if _, err := pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("changelog migration: %w", err)
		}
	}
	return nil
}
