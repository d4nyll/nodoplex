package store

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/d4nyll/nodoplex/internal/graph"
	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
)

// Neo4j holds these values (B1):
//
//   - (:Node {id, product, type, version, fields, watchers, archivedMark}):
//     fields is the JSON of the Fields, so that each number in a payload keeps
//     its exact text
//   - (tail:Node)-[:EDGE {id, kind, priority}]->(head:Node)
//   - (:Changefeed {slug, state, latestChangeId, latestTime, deletedTime}): one
//     for each slug that a Product ever had, so the slug of a deleted Product
//     stays taken (P3). Times are microseconds since the Unix epoch
//   - (:OutboxEntry {slug, changeId, time, json, edgeEnds}): a Change Entry
//     that the server hasn't yet copied to PostgreSQL (B3, B5)
//   - (:DeletedNode {id, product, watchers}): the watchers of a deleted Node,
//     which the `watcher` filter of the Changefeed checks (Q4)

const (
	feedLive    = "live"    // the Product exists
	feedDeleted = "deleted" // the Product is deleted, and its Changefeed is kept (P7)
	feedPurged  = "purged"  // the Changefeed is deleted (P8)
)

var neo4jSchema = []string{
	"CREATE CONSTRAINT node_id_unique IF NOT EXISTS FOR (n:Node) REQUIRE n.id IS UNIQUE",
	"CREATE INDEX node_product IF NOT EXISTS FOR (n:Node) ON (n.product)",
	"CREATE INDEX edge_id IF NOT EXISTS FOR ()-[e:EDGE]-() ON (e.id)",
	"CREATE CONSTRAINT changefeed_slug_unique IF NOT EXISTS FOR (c:Changefeed) REQUIRE c.slug IS UNIQUE",
	"CREATE INDEX outbox_entry IF NOT EXISTS FOR (e:OutboxEntry) ON (e.slug, e.changeId)",
	"CREATE CONSTRAINT deleted_node_id_unique IF NOT EXISTS FOR (d:DeletedNode) REQUIRE d.id IS UNIQUE",
	"CREATE INDEX deleted_node_product IF NOT EXISTS FOR (d:DeletedNode) ON (d.product)",
}

// migrateNeo4j creates the constraints and the indexes.
func migrateNeo4j(ctx context.Context, driver neo4j.Driver) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)
	for _, q := range neo4jSchema {
		// ExecuteWrite retries transient errors, such as the deadlock of 2
		// processes that create the same constraint at once.
		if _, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
			return run(ctx, tx, q, nil)
		}); err != nil {
			return fmt.Errorf("neo4j: %s: %w", q, err)
		}
	}
	return nil
}

func run(ctx context.Context, tx neo4j.ManagedTransaction, cypher string, params map[string]any) ([]*neo4j.Record, error) {
	res, err := tx.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	return res.Collect(ctx)
}

type feedRecord struct {
	state       string
	feed        graph.Feed
	deletedTime time.Time // the time of the deletion of the Product, if state is feedDeleted
}

// loadFeed returns the Changefeed of the slug, or nil if no Product ever had
// the slug.
func loadFeed(ctx context.Context, tx neo4j.ManagedTransaction, slug string) (*feedRecord, error) {
	recs, err := run(ctx, tx, "MATCH (c:Changefeed {slug: $slug}) RETURN c.state, c.latestChangeId, c.latestTime, c.deletedTime",
		map[string]any{"slug": slug})
	if err != nil || len(recs) == 0 {
		return nil, err
	}
	v := recs[0].Values
	rec := &feedRecord{state: v[0].(string), feed: graph.Feed{LatestChangeID: v[1].(int64), LatestTime: fromMicros(v[2].(int64))}}
	if us, ok := v[3].(int64); ok {
		rec.deletedTime = fromMicros(us)
	}
	return rec, nil
}

func fromMicros(us int64) time.Time {
	if us == 0 {
		return time.Time{}
	}
	return time.UnixMicro(us).UTC()
}

func toMicros(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMicro()
}

// loader returns the function that adds to a partial graph of the Product
// what an operation missed (graph.Misses), from the Neo4j transaction tx. It
// does what graphtest.MemoryLoader does.
func loader(ctx context.Context, tx neo4j.ManagedTransaction, slug string) func(*graph.Graph, graph.Misses) error {
	return func(g *graph.Graph, m graph.Misses) error {
		query := func(cypher string, params map[string]any) ([]*neo4j.Record, error) {
			params["slug"] = slug
			return run(ctx, tx, cypher, params)
		}
		// add holds the ids of the Nodes to add, with their ancestors.
		var add []string
		if len(m.Nodes) > 0 {
			recs, err := query(`UNWIND $ids AS id OPTIONAL MATCH (n:Node {id: id, product: $slug})
				RETURN id, n IS NOT NULL`, map[string]any{"ids": idStrings(m.Nodes)})
			if err != nil {
				return err
			}
			for _, r := range recs {
				if id := r.Values[0].(string); r.Values[1].(bool) {
					add = append(add, id)
				} else {
					g.SetAbsentNode(uuid.MustParse(id))
				}
			}
		}
		if len(m.Edges) > 0 {
			recs, err := query(`UNWIND $ids AS id OPTIONAL MATCH (t:Node {product: $slug})-[:EDGE {id: id}]->(h:Node)
				RETURN id, t.id, h.id`, map[string]any{"ids": idStrings(m.Edges)})
			if err != nil {
				return err
			}
			for _, r := range recs {
				if tail, ok := r.Values[1].(string); ok {
					add = append(add, tail, r.Values[2].(string))
				} else {
					g.SetAbsentEdge(uuid.MustParse(r.Values[0].(string)))
				}
			}
		}
		if len(m.Rels) > 0 {
			var rows []map[string]any
			for id, r := range m.Rels {
				rows = append(rows, map[string]any{"id": id.String(), "children": r&graph.RelChildren != 0,
					"prereqs": r&graph.RelPrereqs != 0, "dependents": r&graph.RelDependents != 0})
			}
			recs, err := query(`UNWIND $rows AS r MATCH (x:Node {id: r.id})-[e:EDGE]-(y:Node)
				WHERE CASE WHEN e.kind = 'hierarchical' THEN r.children AND endNode(e) = x
				           WHEN startNode(e) = x THEN r.prereqs ELSE r.dependents END
				RETURN DISTINCT y.id`, map[string]any{"rows": rows})
			if err != nil {
				return err
			}
			for _, r := range recs {
				add = append(add, r.Values[0].(string))
			}
		}
		if m.Product {
			recs, err := query("MATCH (n:Node {product: $slug, type: 'product'}) RETURN n.id", map[string]any{})
			if err != nil {
				return err
			}
			for _, r := range recs {
				add = append(add, r.Values[0].(string))
			}
		}
		var scanned []uuid.UUID
		done := false
		if s := m.Scan; s != nil {
			after, key, id := s.After, s.StartKey, s.StartID
			if prev := g.Scanned(); len(prev) > 0 {
				last := prev[len(prev)-1]
				after, key, id = true, g.Nodes[last].Version, last
			}
			cmp, dir := "<", "DESC"
			if !s.Descending {
				cmp, dir = ">", "ASC"
			}
			// 1 more than the scan asks for tells whether more come after.
			want := s.Count - len(g.Scanned())
			recs, err := query(`MATCH (n:Node {product: $slug})
				WHERE NOT $after OR n.version `+cmp+` $key OR (n.version = $key AND n.id > $id)
				RETURN n.id ORDER BY n.version `+dir+`, n.id LIMIT $limit`,
				map[string]any{"after": after, "key": key, "id": id.String(), "limit": want + 1})
			if err != nil {
				return err
			}
			done = len(recs) <= want
			for _, r := range recs[:min(len(recs), want)] {
				scanned = append(scanned, uuid.MustParse(r.Values[0].(string)))
				add = append(add, r.Values[0].(string))
			}
		}
		if err := addWithAncestors(ctx, tx, g, add); err != nil {
			return err
		}
		if m.Scan != nil {
			g.AddScanned(scanned, done)
		}
		for id, r := range m.Rels {
			g.SetRels(id, r)
		}
		return nil
	}
}

// addWithAncestors adds the Nodes with the ids, and their ancestors, to g,
// with each Edge that points to or from a Node that it adds.
func addWithAncestors(ctx context.Context, tx neo4j.ManagedTransaction, g *graph.Graph, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	recs, err := run(ctx, tx, `UNWIND $ids AS id MATCH (:Node {id: id})-[:EDGE*0.. {kind: 'hierarchical'}]->(a:Node)
		WITH DISTINCT a WHERE NOT a.id IN $have
		RETURN a.id, a.type, a.version, a.fields, a.watchers, a.archivedMark`,
		map[string]any{"ids": ids, "have": idStrings(g.Nodes)})
	if err != nil || len(recs) == 0 {
		return err
	}
	nodes := make([]*graph.Node, len(recs))
	added := make([]string, len(recs))
	for i, r := range recs {
		if nodes[i], err = recordToNode(r.Values); err != nil {
			return err
		}
		added[i] = r.Values[0].(string)
	}
	recs, err = run(ctx, tx, `UNWIND $ids AS id MATCH (:Node {id: id})-[e:EDGE]-(:Node)
		RETURN DISTINCT e.id, e.kind, startNode(e).id, endNode(e).id, e.priority`, map[string]any{"ids": added})
	if err != nil {
		return err
	}
	edges := make([]*graph.Edge, len(recs))
	for i, r := range recs {
		edges[i] = recordToEdge(r.Values)
	}
	g.Add(nodes, edges)
	return nil
}

func idStrings[T any](m map[uuid.UUID]T) []string {
	out := make([]string, 0, len(m))
	for id := range m {
		out = append(out, id.String())
	}
	return out
}

// recordToEdge reads the values id, kind, tail id, head id, and priority of
// an Edge.
func recordToEdge(v []any) *graph.Edge {
	e := &graph.Edge{ID: uuid.MustParse(v[0].(string)), Kind: graph.EdgeKind(v[1].(string)),
		TailID: uuid.MustParse(v[2].(string)), HeadID: uuid.MustParse(v[3].(string))}
	if p, ok := v[4].(int64); ok {
		e.Priority = p
	}
	return e
}

// recordToNode reads the values id, type, version, fields, watchers, and
// archivedMark of a Node.
func recordToNode(v []any) (*graph.Node, error) {
	fields, err := graph.DecodeFields([]byte(v[3].(string)))
	if err != nil {
		return nil, err
	}
	watchers := []string{}
	for _, w := range v[4].([]any) {
		watchers = append(watchers, w.(string))
	}
	return &graph.Node{
		ID: uuid.MustParse(v[0].(string)), Type: graph.NodeType(v[1].(string)), Version: v[2].(int64),
		Fields: fields, Watchers: watchers, ArchivedMark: v[5].(bool),
	}, nil
}

// persist writes the changes of a write, its Change Entry, and the new values
// of the Changefeed, in the Neo4j transaction tx (B2).
func persist(ctx context.Context, tx neo4j.ManagedTransaction, w *graph.Tx) error {
	slug := w.G.Slug
	before, after := w.Before, w.G
	exec := func(cypher string, params map[string]any) error {
		params["slug"] = slug
		_, err := run(ctx, tx, cypher, params)
		return err
	}

	var deletedEdges []string
	for id := range before.Edges {
		if after.Edges[id] == nil {
			deletedEdges = append(deletedEdges, id.String())
		}
	}
	if len(deletedEdges) > 0 {
		if err := exec("UNWIND $ids AS id MATCH ()-[e:EDGE {id: id}]->() DELETE e", map[string]any{"ids": deletedEdges}); err != nil {
			return err
		}
	}

	var deletedNodes []map[string]any
	for id, n := range before.Nodes {
		if after.Nodes[id] == nil {
			deletedNodes = append(deletedNodes, map[string]any{"id": id.String(), "watchers": n.Watchers})
		}
	}
	if len(deletedNodes) > 0 {
		if err := exec(`UNWIND $rows AS r MATCH (n:Node {id: r.id}) DETACH DELETE n
			WITH r CREATE (:DeletedNode {id: r.id, product: $slug, watchers: r.watchers})`, map[string]any{"rows": deletedNodes}); err != nil {
			return err
		}
	}

	var nodes []map[string]any
	for id, n := range after.Nodes {
		fields, err := graph.Marshal(n.Fields)
		if err != nil {
			return err
		}
		if old := before.Nodes[id]; old != nil {
			oldFields, err := graph.Marshal(old.Fields)
			if err != nil {
				return err
			}
			if old.Version == n.Version && old.ArchivedMark == n.ArchivedMark && slices.Equal(old.Watchers, n.Watchers) && bytes.Equal(oldFields, fields) {
				continue
			}
		}
		nodes = append(nodes, map[string]any{"id": id.String(), "type": string(n.Type), "version": n.Version,
			"fields": string(fields), "watchers": n.Watchers, "archivedMark": n.ArchivedMark})
	}
	if len(nodes) > 0 {
		if err := exec(`UNWIND $rows AS r MERGE (n:Node {id: r.id})
			SET n.product = $slug, n.type = r.type, n.version = r.version, n.fields = r.fields,
			    n.watchers = r.watchers, n.archivedMark = r.archivedMark`, map[string]any{"rows": nodes}); err != nil {
			return err
		}
	}

	var newEdges, changedEdges []map[string]any
	for id, e := range after.Edges {
		row := map[string]any{"id": id.String(), "kind": string(e.Kind), "tailId": e.TailID.String(), "headId": e.HeadID.String(), "priority": nil}
		if e.Kind == graph.Hierarchical {
			row["priority"] = e.Priority
		}
		if old := before.Edges[id]; old == nil {
			newEdges = append(newEdges, row)
		} else if old.Priority != e.Priority {
			changedEdges = append(changedEdges, row)
		}
	}
	if len(newEdges) > 0 {
		if err := exec(`UNWIND $rows AS r MATCH (t:Node {id: r.tailId}) MATCH (h:Node {id: r.headId})
			CREATE (t)-[:EDGE {id: r.id, kind: r.kind, priority: r.priority}]->(h)`, map[string]any{"rows": newEdges}); err != nil {
			return err
		}
	}
	if len(changedEdges) > 0 {
		if err := exec("UNWIND $rows AS r MATCH ()-[e:EDGE {id: r.id}]->() SET e.priority = r.priority", map[string]any{"rows": changedEdges}); err != nil {
			return err
		}
	}

	if e := w.Entry; e != nil {
		state := feedLive
		if w.ProductDeleted {
			state = feedDeleted
		}
		if err := exec(`MATCH (c:Changefeed {slug: $slug})
			SET c.latestChangeId = $id, c.latestTime = $time, c.state = $state,
			    c.deletedTime = CASE WHEN $state = 'deleted' THEN $time END`,
			map[string]any{"id": e.ChangeID, "time": toMicros(e.Time), "state": state}); err != nil {
			return err
		}
		if err := exec(`CREATE (:OutboxEntry {slug: $slug, changeId: $id, time: $time, json: $json, edgeEnds: $edgeEnds})`,
			map[string]any{"id": e.ChangeID, "time": toMicros(e.Time), "json": string(e.JSON), "edgeEnds": string(e.EdgeEnds)}); err != nil {
			return err
		}
	}
	return nil
}

// loadWatchers returns the watchers that each Node has now, or that it had
// when a client deleted it (Q4).
func loadWatchers(ctx context.Context, tx neo4j.ManagedTransaction, slug string, ids []uuid.UUID) (map[uuid.UUID][]string, error) {
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = id.String()
	}
	recs, err := run(ctx, tx, `UNWIND $ids AS id
		OPTIONAL MATCH (n:Node {id: id}) WHERE n.product = $slug
		OPTIONAL MATCH (d:DeletedNode {id: id}) WHERE d.product = $slug
		RETURN id, coalesce(n.watchers, d.watchers, [])`, map[string]any{"ids": strs, "slug": slug})
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID][]string{}
	for _, r := range recs {
		var watchers []string
		for _, w := range r.Values[1].([]any) {
			watchers = append(watchers, w.(string))
		}
		out[uuid.MustParse(r.Values[0].(string))] = watchers
	}
	return out, nil
}

type outboxEntry struct {
	slug     string
	changeID int64
	time     time.Time
	json     string
	edgeEnds string
}
