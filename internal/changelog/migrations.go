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
		// Rows written before namespaces existed have a NULL namespace and are
		// never returned until AssignNamespace fills it in.
		`ALTER TABLE changelog ADD COLUMN IF NOT EXISTS namespace TEXT`,
		`CREATE INDEX IF NOT EXISTS idx_changelog_namespace_type_id ON changelog (namespace, node_type, change_id)`,
	}

	for _, q := range queries {
		if _, err := pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("changelog migration: %w", err)
		}
	}
	return nil
}

// AssignNamespace sets the namespace of every changelog row that has none to
// ns. Rows that already have a namespace are left alone, so running it again
// is a no-op. Returns the number of rows updated.
func AssignNamespace(ctx context.Context, pool *pgxpool.Pool, ns string) (int64, error) {
	tag, err := pool.Exec(ctx, `UPDATE changelog SET namespace = $1 WHERE namespace IS NULL`, ns)
	if err != nil {
		return 0, fmt.Errorf("changelog assign namespace: %w", err)
	}
	return tag.RowsAffected(), nil
}
