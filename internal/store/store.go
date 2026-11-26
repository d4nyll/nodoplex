// Package store runs the operations of the base specification against Neo4j
// and PostgreSQL, as the storage rules of the extended specification give
// (B1 to B12).
package store

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"log"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/d4nyll/nodoplex/internal/changefeed"
	"github.com/d4nyll/nodoplex/internal/graph"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
)

// Settings holds the settings of the server configuration (M1).
type Settings struct {
	ExposeChangefeed       bool
	ChangeEntryCountTarget int64
	ChangeEntrySizeTarget  int64
	DeleteOldChangeEntries bool
}

// DeletedProductRetention is the time for which the server keeps the
// Changefeed of a deleted Product (P7).
const DeletedProductRetention = 30 * 24 * time.Hour

// readBatch is the number of Change Entries that a poll or a subscription
// reads from PostgreSQL at one time.
const readBatch = 500

// readyTimeout is the time for which Ready waits for each query (L3).
const readyTimeout = 2 * time.Second

type Store struct {
	neo      neo4j.Driver
	pg       *pgxpool.Pool
	settings Settings
	hub      *changefeed.Hub
	// Now gives the time of the server's clock.
	Now func() time.Time

	// ponytail: the locks of S1 live in this process, so only one server
	// process may use the databases at a time.
	locksMu sync.Mutex
	locks   map[string]*sync.RWMutex

	// copyMu makes one copy of the outbox run at a time, so that the server
	// copies the Change Entries of each Changefeed in ascending order (B3).
	copyMu sync.Mutex
	kick   chan struct{}

	// started is true after Start has succeeded (L2).
	started atomic.Bool

	// count and size are the count and the total size of the Change Entries
	// (R1), in PostgreSQL and in the Neo4j outbox. A write adds its Change
	// Entry after its Neo4j transaction commits. totalsMu guards them,
	// and makes the deletions of R2 run one at a time. Lock order: the lock
	// of a slug, then totalsMu, then copyMu.
	// ponytail: like the locks of S1, the totals live in this process.
	totalsMu    sync.Mutex
	count, size int64
}

func New(neo neo4j.Driver, pg *pgxpool.Pool, settings Settings) *Store {
	return &Store{neo: neo, pg: pg, settings: settings, hub: changefeed.NewHub(), Now: time.Now,
		locks: map[string]*sync.RWMutex{}, kick: make(chan struct{}, 1)}
}

// lock returns the lock of a slug. A write holds it to run the writes of a
// slug one at a time (S1). A read holds it for reading, so that it reads one
// state of the Product (S5), also across Neo4j and PostgreSQL.
// ponytail: the map keeps a lock for each slug that a request ever named.
func (s *Store) lock(slug string) *sync.RWMutex {
	s.locksMu.Lock()
	defer s.locksMu.Unlock()
	l := s.locks[slug]
	if l == nil {
		l = &sync.RWMutex{}
		s.locks[slug] = l
	}
	return l
}

// Start creates the constraints, the indexes, and the tables, copies the
// outbox, and reads the count and the total size of the Change Entries. When
// Start returns nil, the server has finished starting (L2).
func (s *Store) Start(ctx context.Context) error {
	if err := migrateNeo4j(ctx, s.neo); err != nil {
		return err
	}
	if err := migratePostgres(ctx, s.pg); err != nil {
		return err
	}
	// With the outbox empty, PostgreSQL holds each Change Entry.
	if err := s.copyPending(ctx); err != nil {
		return err
	}
	if err := s.pg.QueryRow(ctx, "SELECT count, size FROM change_entry_totals").Scan(&s.count, &s.size); err != nil {
		return err
	}
	s.started.Store(true)
	return nil
}

// Started reports whether Start has succeeded.
func (s *Store) Started() bool { return s != nil && s.started.Load() }

// errNotStarted refuses an operation that runs before Start has succeeded
// with internal_error (E1).
var errNotStarted = errors.New("the server hasn't finished starting")

// Ready returns an error if the server hasn't finished starting, or if a
// query to Neo4j or to PostgreSQL fails (L3).
func (s *Store) Ready(ctx context.Context) error {
	if !s.Started() {
		return errNotStarted
	}
	neoCtx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()
	session := s.neo.NewSession(neoCtx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(neoCtx)
	if _, err := neo4j.ExecuteRead(neoCtx, session, func(tx neo4j.ManagedTransaction) ([]*neo4j.Record, error) {
		return run(neoCtx, tx, "RETURN 1", nil)
	}); err != nil {
		return err
	}
	pgCtx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()
	_, err := s.pg.Exec(pgCtx, "SELECT 1")
	return err
}

func productNotFound(slug string) error {
	return graph.Refuse(graph.CodeNotFound, "no Product has the slug %q", slug)
}

// ── Writes and reads of the graph ───────────────────────────────────────────

// Write runs a write to the Product that has the slug. steps runs the steps
// of the operation on the Tx of a partial graph. Write then completes the
// write, and writes its changes and its Change Entry in one Neo4j
// transaction (B2). steps runs again each time the write reads more than the
// graph holds (graph.Run).
func (s *Store) Write(ctx context.Context, slug, userID string, steps func(tx *graph.Tx) error) (*graph.Tx, error) {
	return s.write(ctx, slug, userID, false, func(tx *graph.Tx, _ bool) error { return steps(tx) })
}

// CreateProduct runs the steps of CreateProduct.
func (s *Store) CreateProduct(ctx context.Context, slug, userID string, fields graph.FieldsInput) (graph.NodeJSON, error) {
	var id uuid.UUID
	tx, err := s.write(ctx, slug, userID, true, func(tx *graph.Tx, slugTaken bool) (err error) {
		id, err = graph.CreateProduct(tx, fields, slugTaken)
		return err
	})
	if err != nil {
		return graph.NodeJSON{}, err
	}
	return tx.View.NodeJSON(id), nil
}

func (s *Store) write(ctx context.Context, slug, userID string, creating bool, steps func(tx *graph.Tx, feedExists bool) error) (*graph.Tx, error) {
	if !s.Started() {
		return nil, errNotStarted
	}
	l := s.lock(slug)
	l.Lock()
	defer l.Unlock()
	session := s.neo.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)
	var done *graph.Tx
	_, err := session.ExecuteWrite(ctx, func(ntx neo4j.ManagedTransaction) (any, error) {
		now := s.Now()
		rec, err := loadFeed(ctx, ntx, slug)
		if err != nil {
			return nil, err
		}
		var tx *graph.Tx
		runSteps := func(g *graph.Graph, feed graph.Feed) error {
			tx = graph.NewTx(g, feed, userID, now)
			if err := steps(tx, rec != nil); err != nil {
				return err
			}
			return tx.Complete()
		}
		if creating {
			err = runSteps(graph.NewGraph(slug), graph.Feed{})
		} else if rec == nil || rec.state != feedLive {
			err = productNotFound(slug)
		} else {
			g := graph.NewPartialGraph(slug)
			err = graph.Run(g, loader(ctx, ntx, slug), func() error { return runSteps(g, rec.feed) })
		}
		if err != nil {
			return nil, err
		}
		if creating {
			if _, err := run(ctx, ntx, "CREATE (:Changefeed {slug: $slug, state: $state, latestChangeId: 0, latestTime: 0})",
				map[string]any{"slug": slug, "state": feedLive}); err != nil {
				return nil, err
			}
		}
		if err := persist(ctx, ntx, tx); err != nil {
			return nil, err
		}
		done = tx
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	if done.Entry != nil {
		s.totalsMu.Lock()
		s.count, s.size = s.count+1, s.size+int64(len(done.Entry.JSON))
		s.totalsMu.Unlock()
		s.Kick()
	}
	return done, nil
}

// errNothingToDelete means that PostgreSQL holds no Change Entry that
// deleteDownTo can delete.
var errNothingToDelete = errors.New("PostgreSQL holds too few Change Entries to get within the targets")

// deleteDownTo copies the outbox, and then deletes the oldest Change Entries
// from PostgreSQL until the count is at most countLimit and the total size at
// most sizeLimit (R2). It deletes only Change Entries that it has copied
// (B6), and it copies first so that the oldest ones are in PostgreSQL. The
// caller holds totalsMu.
func (s *Store) deleteDownTo(ctx context.Context, countLimit, sizeLimit int64) error {
	s.copyMu.Lock()
	defer s.copyMu.Unlock()
	if err := s.copyOutbox(ctx); err != nil {
		return err
	}
	for s.count > countLimit || s.size > sizeLimit {
		deleted, count, size, err := deleteOldest(ctx, s.pg, s.count-countLimit, s.size-sizeLimit)
		if err != nil {
			return err
		}
		if count == 0 {
			return errNothingToDelete
		}
		s.count, s.size = s.count-count, s.size-size
		for slug := range deleted {
			s.hub.Signal(slug)
		}
	}
	return nil
}

// Read runs fn on one state of the graph of the Product that has the slug. fn
// gets a View of a partial graph, and runs again each time it reads more than
// the graph holds (graph.Run).
func (s *Store) Read(ctx context.Context, slug string, fn func(v *graph.View) error) error {
	if !s.Started() {
		return errNotStarted
	}
	l := s.lock(slug)
	l.RLock()
	defer l.RUnlock()
	session := s.neo.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)
	_, err := session.ExecuteRead(ctx, func(ntx neo4j.ManagedTransaction) (any, error) {
		rec, err := loadFeed(ctx, ntx, slug)
		if err != nil {
			return nil, err
		}
		if rec == nil || rec.state != feedLive {
			return nil, productNotFound(slug)
		}
		g := graph.NewPartialGraph(slug)
		return nil, graph.Run(g, loader(ctx, ntx, slug), func() error { return fn(graph.NewView(g)) })
	})
	return err
}

// ListProducts runs the steps of ListProducts.
func (s *Store) ListProducts(ctx context.Context, archived []bool) ([]graph.NodeJSON, error) {
	if !s.Started() {
		return nil, errNotStarted
	}
	session := s.neo.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)
	recs, err := neo4j.ExecuteRead(ctx, session, func(tx neo4j.ManagedTransaction) ([]*neo4j.Record, error) {
		return run(ctx, tx, `MATCH (c:Changefeed {state: $state}) MATCH (n:Node {product: c.slug, type: 'product'})
			RETURN n.id, n.type, n.version, n.fields, n.watchers, n.archivedMark`, map[string]any{"state": feedLive})
	})
	if err != nil {
		return nil, err
	}
	products := []graph.NodeJSON{}
	for _, r := range recs {
		n, err := recordToNode(r.Values)
		if err != nil {
			return nil, err
		}
		// A Product Node has no Parent, so it is archived only if it carries
		// the archived mark (A2).
		if slices.Contains(archived, n.ArchivedMark) {
			g := graph.NewGraph(n.Fields.Product)
			g.Nodes[n.ID] = n
			products = append(products, graph.NewView(g).NodeJSON(n.ID))
		}
	}
	slices.SortFunc(products, func(a, b graph.NodeJSON) int { return cmp.Compare(*a.Fields.Slug, *b.Fields.Slug) })
	return products, nil
}

// ── The Changefeed ──────────────────────────────────────────────────────────

// changefeedOf returns the Changefeed of a Product that exists, or of a
// deleted Product whose Changefeed the server still keeps.
func (s *Store) changefeedOf(ctx context.Context, slug string) (*feedRecord, error) {
	session := s.neo.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)
	rec, err := neo4j.ExecuteRead(ctx, session, func(tx neo4j.ManagedTransaction) (*feedRecord, error) {
		return loadFeed(ctx, tx, slug)
	})
	if err != nil {
		return nil, err
	}
	// P8: when the time of P7 ends, the server answers as if the Product
	// doesn't exist, even before PurgeExpired deletes the Changefeed.
	expired := rec != nil && rec.state == feedDeleted && !s.Now().Before(rec.deletedTime.Add(DeletedProductRetention))
	if rec == nil || rec.state == feedPurged || expired {
		return nil, productNotFound(slug)
	}
	return rec, nil
}

// LatestChangeID runs the steps of GetLatestChangeId.
func (s *Store) LatestChangeID(ctx context.Context, slug string) (int64, error) {
	if !s.Started() {
		return 0, errNotStarted
	}
	l := s.lock(slug)
	l.RLock()
	defer l.RUnlock()
	rec, err := s.changefeedOf(ctx, slug)
	if err != nil {
		return 0, err
	}
	return rec.feed.LatestChangeID, nil
}

func repeatableRead(ctx context.Context, pool *pgxpool.Pool, fn func(tx pgx.Tx) error) error {
	return pgx.BeginTxFunc(ctx, pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, fn)
}

func entriesDeleted() error {
	return graph.Refuse(graph.CodeChangeEntriesDeleted, "the server has deleted a Change Entry after the start Change ID")
}

// Poll runs the steps of Poll. It returns the JSON text of each Change Entry,
// and the latest Change ID.
func (s *Store) Poll(ctx context.Context, slug string, start int64, limit int, groups []changefeed.Group) ([]json.RawMessage, int64, error) {
	if !s.Started() {
		return nil, 0, errNotStarted
	}
	l := s.lock(slug)
	l.RLock()
	defer l.RUnlock()
	rec, err := s.changefeedOf(ctx, slug)
	if err != nil {
		return nil, 0, err
	}
	if !s.settings.ExposeChangefeed {
		return nil, 0, graph.Refuse(graph.CodeChangefeedDisabled, "the server doesn't expose the Changefeed")
	}
	latest := rec.feed.LatestChangeID
	if start > latest {
		return nil, 0, graph.Refuse(graph.CodeChangeOutOfRange, "the start Change ID %d is higher than the latest Change ID %d", start, latest)
	}
	// B8: wait until the server has copied each Change Entry up to the
	// latest Change ID.
	if err := s.copyPending(ctx); err != nil {
		return nil, 0, err
	}
	out := []json.RawMessage{}
	err = repeatableRead(ctx, s.pg, func(tx pgx.Tx) error {
		deleted, err := deletedThrough(ctx, tx, slug)
		if err != nil {
			return err
		}
		if start > 0 && deleted > start {
			return entriesDeleted()
		}
		position := start
		for len(out) < limit {
			batch, err := readEntries(ctx, tx, slug, position, latest, readBatch)
			if err != nil || len(batch) == 0 {
				return err
			}
			matched, err := s.filter(ctx, slug, batch, groups)
			if err != nil {
				return err
			}
			for _, e := range matched[:min(len(matched), limit-len(out))] {
				out = append(out, e.JSON)
			}
			position = batch[len(batch)-1].changeID
		}
		return nil
	})
	return out, latest, err
}

// Entry is a Change Entry that a subscription pushes: its Change ID and its
// JSON text.
type Entry struct {
	ChangeID int64
	JSON     json.RawMessage
}

// filter returns each Change Entry that matches the filter groups.
func (s *Store) filter(ctx context.Context, slug string, entries []storedEntry, groups []changefeed.Group) ([]Entry, error) {
	parsed := make([][]changefeed.Change, len(entries))
	var ids []uuid.UUID
	for i, e := range entries {
		changes, err := changefeed.ParseEntry(e.json, e.edgeEnds)
		if err != nil {
			return nil, err
		}
		parsed[i] = changes
		ids = append(ids, changefeed.NodeIDs(changes)...)
	}
	watchers := map[uuid.UUID][]string{}
	if changefeed.UsesWatchers(groups) {
		session := s.neo.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
		defer session.Close(ctx)
		var err error
		watchers, err = neo4j.ExecuteRead(ctx, session, func(tx neo4j.ManagedTransaction) (map[uuid.UUID][]string, error) {
			return loadWatchers(ctx, tx, slug, ids)
		})
		if err != nil {
			return nil, err
		}
	}
	var out []Entry
	for i, e := range entries {
		if changefeed.Matches(groups, parsed[i], func(id uuid.UUID) []string { return watchers[id] }) {
			out = append(out, Entry{ChangeID: e.changeID, JSON: e.json})
		}
	}
	return out, nil
}

// CheckSubscribe runs steps 1 to 4 of Subscribe, and returns the start Change
// ID. start is nil if the input gives no startChangeId.
func (s *Store) CheckSubscribe(ctx context.Context, slug string, start *int64) (int64, error) {
	if !s.Started() {
		return 0, errNotStarted
	}
	l := s.lock(slug)
	l.RLock()
	defer l.RUnlock()
	rec, err := s.changefeedOf(ctx, slug)
	if err != nil {
		return 0, err
	}
	if !s.settings.ExposeChangefeed {
		return 0, graph.Refuse(graph.CodeChangefeedDisabled, "the server doesn't expose the Changefeed")
	}
	latest := rec.feed.LatestChangeID
	position := latest
	if start != nil {
		position = *start
	}
	if position > latest {
		return 0, graph.Refuse(graph.CodeChangeOutOfRange, "the start Change ID %d is higher than the latest Change ID %d", position, latest)
	}
	if position > 0 {
		err := repeatableRead(ctx, s.pg, func(tx pgx.Tx) error {
			deleted, err := deletedThrough(ctx, tx, slug)
			if err == nil && deleted > position {
				return entriesDeleted()
			}
			return err
		})
		if err != nil {
			return 0, err
		}
	}
	return position, nil
}

// Subscription is an open subscription (step 5 of Subscribe).
type Subscription struct {
	s        *Store
	slug     string
	groups   []changefeed.Group
	position int64
	// Signals receives a signal when the server has copied Change Entries of
	// the Product, or has deleted some (B9).
	Signals <-chan struct{}
	Close   func()
}

// Subscribe opens a subscription at the position that CheckSubscribe gave.
func (s *Store) Subscribe(slug string, position int64, groups []changefeed.Group) *Subscription {
	signals, cancel := s.hub.Subscribe(slug)
	return &Subscription{s: s, slug: slug, groups: groups, position: position, Signals: signals, Close: cancel}
}

// Next runs step 6 of Subscribe on at most readBatch Change Entries. It
// returns the JSON text of each one that matches, and more is true if
// Change Entries may be left before the latest Change ID. A refusal ends the
// subscription with its code.
func (sub *Subscription) Next(ctx context.Context) (entries []Entry, more bool, err error) {
	s := sub.s
	l := s.lock(sub.slug)
	l.RLock()
	defer l.RUnlock()
	rec, err := s.changefeedOf(ctx, sub.slug)
	if err != nil {
		return nil, false, err
	}
	if err := s.copyPending(ctx); err != nil {
		return nil, false, err
	}
	err = repeatableRead(ctx, s.pg, func(tx pgx.Tx) error {
		deleted, err := deletedThrough(ctx, tx, sub.slug)
		if err != nil {
			return err
		}
		if deleted > sub.position {
			return graph.Refuse(graph.CodeChangeEntriesDeleted, "the server has deleted a Change Entry after the position of the subscription")
		}
		batch, err := readEntries(ctx, tx, sub.slug, sub.position, rec.feed.LatestChangeID, readBatch)
		if err != nil || len(batch) == 0 {
			return err
		}
		if entries, err = s.filter(ctx, sub.slug, batch, sub.groups); err != nil {
			return err
		}
		sub.position = batch[len(batch)-1].changeID
		more = len(batch) == readBatch
		return nil
	})
	return entries, more, err
}

// ── The outbox ──────────────────────────────────────────────────────────────

// Kick asks the background loop to copy the outbox.
func (s *Store) Kick() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// copyPending copies each Change Entry in the Neo4j outbox to PostgreSQL (B3),
// deletes it from Neo4j (B5), and signals the subscriptions (B9). When it
// returns nil, PostgreSQL holds each Change Entry that Neo4j held when it
// started.
func (s *Store) copyPending(ctx context.Context) error {
	s.copyMu.Lock()
	defer s.copyMu.Unlock()
	return s.copyOutbox(ctx)
}

// copyOutbox runs copyPending. The caller holds copyMu.
func (s *Store) copyOutbox(ctx context.Context) error {
	session := s.neo.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)
	for {
		entries, err := neo4j.ExecuteRead(ctx, session, func(tx neo4j.ManagedTransaction) ([]outboxEntry, error) {
			recs, err := run(ctx, tx, `MATCH (e:OutboxEntry) RETURN e.slug, e.changeId, e.time, e.json, e.edgeEnds
				ORDER BY e.slug, e.changeId LIMIT 500`, nil)
			var out []outboxEntry
			for _, r := range recs {
				v := r.Values
				out = append(out, outboxEntry{slug: v[0].(string), changeID: v[1].(int64), time: fromMicros(v[2].(int64)),
					json: v[3].(string), edgeEnds: v[4].(string)})
			}
			return out, err
		})
		if err != nil || len(entries) == 0 {
			return err
		}
		if err := insertEntries(ctx, s.pg, entries); err != nil {
			return err
		}
		keys := make([]map[string]any, len(entries))
		slugs := map[string]bool{}
		for i, e := range entries {
			keys[i] = map[string]any{"slug": e.slug, "changeId": e.changeID}
			slugs[e.slug] = true
		}
		if _, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
			return run(ctx, tx, "UNWIND $keys AS k MATCH (e:OutboxEntry {slug: k.slug, changeId: k.changeId}) DELETE e",
				map[string]any{"keys": keys})
		}); err != nil {
			return err
		}
		for slug := range slugs {
			s.hub.Signal(slug)
		}
	}
}

// Run copies the outbox after each write, deletes the oldest Change Entries
// while the count or the total size is above its target (R2, R3), and
// deletes the Changefeeds of deleted Products whose time has ended (P8),
// until ctx ends.
func (s *Store) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var lastPurge time.Time
	for {
		if err := s.copyPending(ctx); err != nil && ctx.Err() == nil {
			log.Printf("copy outbox: %v", err)
		}
		if s.settings.DeleteOldChangeEntries {
			if err := s.deleteOldEntries(ctx); err != nil && ctx.Err() == nil {
				log.Printf("delete old change entries: %v", err)
			}
		}
		if time.Since(lastPurge) > time.Minute {
			if err := s.PurgeExpired(ctx, s.Now()); err != nil && ctx.Err() == nil {
				log.Printf("delete changefeeds of deleted products: %v", err)
			}
			lastPurge = time.Now()
		}
		select {
		case <-ctx.Done():
			drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.copyPending(drain); err != nil {
				log.Printf("copy outbox: %v", err)
			}
			return
		case <-s.kick:
		case <-ticker.C:
		}
	}
}

// deleteOldEntries deletes the oldest Change Entries until the count and the
// total size are within the targets (R2). If the rest are in the outbox, a
// later run deletes them (R2).
func (s *Store) deleteOldEntries(ctx context.Context) error {
	s.totalsMu.Lock()
	defer s.totalsMu.Unlock()
	err := s.deleteDownTo(ctx, s.settings.ChangeEntryCountTarget, s.settings.ChangeEntrySizeTarget)
	if err == errNothingToDelete {
		return nil
	}
	return err
}

// PurgeExpired deletes the Changefeed of each deleted Product whose deletion
// is older than DeletedProductRetention at the time now (P8), and ends each
// open subscription to it.
func (s *Store) PurgeExpired(ctx context.Context, now time.Time) error {
	session := s.neo.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)
	recs, err := neo4j.ExecuteRead(ctx, session, func(tx neo4j.ManagedTransaction) ([]*neo4j.Record, error) {
		return run(ctx, tx, "MATCH (c:Changefeed {state: $state}) WHERE c.deletedTime <= $cutoff RETURN c.slug",
			map[string]any{"state": feedDeleted, "cutoff": toMicros(now.Add(-DeletedProductRetention))})
	})
	if err != nil {
		return err
	}
	for _, r := range recs {
		if err := s.purge(ctx, session, r.Values[0].(string)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) purge(ctx context.Context, session neo4j.Session, slug string) error {
	l := s.lock(slug)
	l.Lock()
	defer l.Unlock()
	s.totalsMu.Lock()
	defer s.totalsMu.Unlock()
	s.copyMu.Lock()
	defer s.copyMu.Unlock()
	// No write adds to the Changefeed of a deleted Product, so after this
	// copy, PostgreSQL holds each of its Change Entries.
	if err := s.copyOutbox(ctx); err != nil {
		return err
	}
	// PostgreSQL first: if the server stops between the 2 transactions, the
	// next run finds the Changefeed still deleted, and runs both again.
	count, size, err := deleteChangefeed(ctx, s.pg, slug)
	if err != nil {
		return err
	}
	s.count, s.size = s.count-count, s.size-size
	_, err = session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		params := map[string]any{"slug": slug, "state": feedPurged}
		for _, q := range []string{
			"MATCH (d:DeletedNode {product: $slug}) DELETE d",
			"MATCH (c:Changefeed {slug: $slug}) SET c.state = $state",
		} {
			if _, err := run(ctx, tx, q, params); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	if err == nil {
		s.hub.Signal(slug)
	}
	return err
}
