package changelog

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Entry represents a single row in the changelog table.
type Entry struct {
	ChangeID  int64           `json:"changeId"`
	Namespace string          `json:"-"`
	Action    string          `json:"action"`
	NodeType  string          `json:"nodeType"`
	NodeID    string          `json:"nodeId"`
	Node      json.RawMessage `json:"node,omitempty"`
	Previous  json.RawMessage `json:"previous,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// Store provides access to the changelog table in PostgreSQL.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a new changelog Store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Append inserts a new changelog entry and returns the assigned change_id.
func (s *Store) Append(ctx context.Context, entry Entry) (int64, error) {
	var changeID int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO changelog (namespace, action, node_type, node_id, node, previous)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING change_id`,
		entry.Namespace, entry.Action, entry.NodeType, entry.NodeID, entry.Node, entry.Previous,
	).Scan(&changeID)
	if err != nil {
		return 0, fmt.Errorf("changelog append: %w", err)
	}
	return changeID, nil
}

// ListSince returns changelog entries in namespace ns with change_id > sinceID
// for the given node type, ordered by change_id ASC, limited to limit rows.
func (s *Store) ListSince(ctx context.Context, ns string, nodeType string, sinceID int64, limit int) ([]Entry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT change_id, namespace, action, node_type, node_id, node, previous, timestamp
		 FROM changelog
		 WHERE namespace = $1 AND node_type = $2 AND change_id > $3
		 ORDER BY change_id ASC
		 LIMIT $4`,
		ns, nodeType, sinceID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("changelog list: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ChangeID, &e.Namespace, &e.Action, &e.NodeType, &e.NodeID, &e.Node, &e.Previous, &e.Timestamp); err != nil {
			return nil, fmt.Errorf("changelog scan: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
