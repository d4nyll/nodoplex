package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgreSQL holds these values:
//
//   - change_entry: each Change Entry that the server has copied from Neo4j
//     (B3), with its JSON text (B10), its size (B11), and the values of Q5
//     (B12) in edge_ends
//   - changefeed: for each Changefeed, the highest Change ID that R2 has
//     deleted. R2 deletes the Change Entries of a Changefeed in ascending
//     order of Change ID, so a Change Entry whose Change ID is higher than
//     the start Change ID is deleted only if deleted_through is higher.
//   - change_entry_totals: the number and the total size of the Change
//     Entries in change_entry, so that the server doesn't count them when it
//     starts
var postgresSchema = []string{
	`CREATE TABLE IF NOT EXISTS change_entry (
		slug       TEXT        NOT NULL,
		change_id  BIGINT      NOT NULL,
		time       TIMESTAMPTZ NOT NULL,
		json       TEXT        NOT NULL,
		edge_ends  TEXT        NOT NULL,
		size       BIGINT      NOT NULL,
		PRIMARY KEY (slug, change_id)
	)`,
	`CREATE INDEX IF NOT EXISTS change_entry_age ON change_entry (time, slug, change_id)`,
	`CREATE TABLE IF NOT EXISTS changefeed (
		slug            TEXT   PRIMARY KEY,
		deleted_through BIGINT NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE IF NOT EXISTS change_entry_totals (
		id    BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
		count BIGINT  NOT NULL,
		size  BIGINT  NOT NULL
	)`,
	`INSERT INTO change_entry_totals (id, count, size) VALUES (TRUE, 0, 0) ON CONFLICT DO NOTHING`,
}

// migratePostgres creates the tables.
func migratePostgres(ctx context.Context, pool *pgxpool.Pool) error {
	for _, q := range postgresSchema {
		if _, err := pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("postgres: %w", err)
		}
	}
	return nil
}

// insertEntries copies Change Entries to PostgreSQL in one transaction. It
// skips a Change Entry that PostgreSQL already holds (B4), e.g. after a
// restart between the commit of a copy and the deletion from Neo4j.
func insertEntries(ctx context.Context, pool *pgxpool.Pool, entries []outboxEntry) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var count, size int64
		for _, e := range entries {
			if _, err := tx.Exec(ctx, "INSERT INTO changefeed (slug) VALUES ($1) ON CONFLICT DO NOTHING", e.slug); err != nil {
				return err
			}
			tag, err := tx.Exec(ctx, `INSERT INTO change_entry (slug, change_id, time, json, edge_ends, size)
				VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT DO NOTHING`,
				e.slug, e.changeID, e.time, e.json, e.edgeEnds, len(e.json))
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 1 {
				count++
				size += int64(len(e.json))
			}
		}
		_, err := tx.Exec(ctx, "UPDATE change_entry_totals SET count = count + $1, size = size + $2", count, size)
		return err
	})
}

type storedEntry struct {
	changeID int64
	json     []byte
	edgeEnds []byte
}

// readEntries returns at most limit Change Entries of the slug whose Change
// ID is higher than after and at most upTo, in ascending order of Change ID.
func readEntries(ctx context.Context, tx pgx.Tx, slug string, after, upTo int64, limit int) ([]storedEntry, error) {
	rows, err := tx.Query(ctx, `SELECT change_id, json, edge_ends FROM change_entry
		WHERE slug = $1 AND change_id > $2 AND change_id <= $3 ORDER BY change_id LIMIT $4`, slug, after, upTo, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (storedEntry, error) {
		var e storedEntry
		var text, ends string
		err := row.Scan(&e.changeID, &text, &ends)
		e.json, e.edgeEnds = []byte(text), []byte(ends)
		return e, err
	})
}

// deletedThrough returns the highest Change ID of the slug that R2 deleted.
func deletedThrough(ctx context.Context, tx pgx.Tx, slug string) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, "SELECT deleted_through FROM changefeed WHERE slug = $1", slug).Scan(&id)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	return id, err
}

// deleteOldest deletes the oldest Change Entries (R2), at most 1000. It stops
// when it has deleted at least count of them, of at least size bytes
// together. It returns the highest Change ID that it deleted for each slug, and the
// number and the total size of the Change Entries that it deleted.
func deleteOldest(ctx context.Context, pool *pgxpool.Pool, count, size int64) (map[string]int64, int64, int64, error) {
	deleted := map[string]int64{}
	var n, bytes int64
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		// Ascending time across Changefeeds, and ascending Change ID in each
		// Changefeed, because the time of a Changefeed never decreases (C7).
		rows, err := tx.Query(ctx, "SELECT slug, change_id, size FROM change_entry ORDER BY time, slug, change_id LIMIT 1000")
		if err != nil {
			return err
		}
		type row struct {
			slug     string
			changeID int64
			size     int64
		}
		oldest, err := pgx.CollectRows(rows, func(cr pgx.CollectableRow) (row, error) {
			var r row
			return r, cr.Scan(&r.slug, &r.changeID, &r.size)
		})
		if err != nil {
			return err
		}
		var slugs []string
		var ids []int64
		for _, r := range oldest {
			if n >= count && bytes >= size {
				break
			}
			slugs, ids = append(slugs, r.slug), append(ids, r.changeID)
			n, bytes = n+1, bytes+r.size
			deleted[r.slug] = max(deleted[r.slug], r.changeID)
		}
		if n == 0 {
			return nil
		}
		if _, err := tx.Exec(ctx, `DELETE FROM change_entry e USING unnest($1::text[], $2::bigint[]) AS d(slug, change_id)
			WHERE e.slug = d.slug AND e.change_id = d.change_id`, slugs, ids); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE change_entry_totals SET count = count - $1, size = size - $2", n, bytes); err != nil {
			return err
		}
		for slug, id := range deleted {
			if _, err := tx.Exec(ctx, "UPDATE changefeed SET deleted_through = GREATEST(deleted_through, $2) WHERE slug = $1", slug, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, 0, 0, err
	}
	return deleted, n, bytes, nil
}

// deleteChangefeed deletes each Change Entry of the slug, for P8. It returns
// their number and their total size.
func deleteChangefeed(ctx context.Context, pool *pgxpool.Pool, slug string) (count, size int64, err error) {
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, "SELECT count(*), coalesce(sum(size), 0) FROM change_entry WHERE slug = $1", slug).Scan(&count, &size); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM change_entry WHERE slug = $1", slug); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM changefeed WHERE slug = $1", slug); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "UPDATE change_entry_totals SET count = count - $1, size = size - $2", count, size)
		return err
	})
	return count, size, err
}
