// Package graphtest generates graphs and operations for tests, and runs each
// operation 2 ways: on the whole graph of the Product, as the server did
// before it read partial graphs, and on a partial graph that a loader fills.
package graphtest

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/d4nyll/nodoplex/internal/graph"
	"github.com/google/uuid"
)

// Now is the time of each write.
var Now = time.Date(2026, 11, 23, 8, 0, 0, 0, time.UTC)

// IDs returns a source of ids that gives the same ids for the same seed, so
// that both ways of running a write create the same ids.
func IDs(seed uint64) func() uuid.UUID {
	var key [32]byte
	binary.LittleEndian.PutUint64(key[:], seed)
	r := rand.NewChaCha8(key)
	return func() uuid.UUID { return uuid.Must(uuid.NewRandomFromReader(r)) }
}

var levels = []graph.NodeType{graph.TypeProduct, graph.TypeVision, graph.TypeGoal, graph.TypeFeature,
	graph.TypeRequirement, graph.TypeTask, graph.TypeSubtask}

var statuses = []graph.Status{graph.NotStarted, graph.InProgress, graph.Done}

// Generate returns a graph of a Product with n Nodes, made by writes, and
// the Changefeed values after them. Each Node other than a vision has 1 to 3
// Parents, leaves are at each level, Dependency Edges join random Nodes and
// form a few long chains, and the versions spread over many writes.
func Generate(rng *rand.Rand, slug string, n int) (*graph.Graph, graph.Feed) {
	g := graph.NewGraph(slug)
	var feed graph.Feed
	tryWrite := func(steps func(tx *graph.Tx) error) error {
		tx := graph.NewTx(g, feed, "gen", Now)
		tx.NewID = IDs(rng.Uint64())
		err := steps(tx)
		if err == nil {
			err = tx.Complete()
		}
		if err == nil {
			g, feed = tx.G, tx.Feed
		}
		return err
	}
	write := func(steps func(tx *graph.Tx)) {
		if err := tryWrite(func(tx *graph.Tx) error { steps(tx); return nil }); err != nil {
			panic(err)
		}
	}
	byLevel := make([][]uuid.UUID, len(levels))
	write(func(tx *graph.Tx) {
		id, err := graph.CreateProduct(tx, graph.FieldsInput{"title": "Product"}, false)
		if err != nil {
			panic(err)
		}
		byLevel[0] = []uuid.UUID{id}
	})
	// The weight of each level as the level of a new Node's Parent.
	weights := []int{1, 3, 8, 15, 25, 48}
	const batches = 6
	for b := range batches {
		write(func(tx *graph.Tx) {
			for range (n - 1 + b) / batches {
				total := 0
				for l, w := range weights {
					if len(byLevel[l]) > 0 {
						total += w
					}
				}
				r, l := rng.IntN(total), 0
				for ; len(byLevel[l]) == 0 || r >= weights[l]; l++ {
					if len(byLevel[l]) > 0 {
						r -= weights[l]
					}
				}
				candidates := byLevel[l]
				parents := []graph.ParentRef{{ID: candidates[rng.IntN(len(candidates))]}}
				for range []int{0, 0, 0, 1, 1, 2}[rng.IntN(6)] {
					if p := candidates[rng.IntN(len(candidates))]; levels[l+1] != graph.TypeVision &&
						!slices.ContainsFunc(parents, func(q graph.ParentRef) bool { return q.ID == p }) {
						parents = append(parents, graph.ParentRef{ID: p})
					}
				}
				if rng.IntN(5) == 0 {
					p := rng.Int64N(20)
					parents[0].Priority = &p
				}
				fields := graph.FieldsInput{"title": fmt.Sprintf("Node %d", rng.IntN(1000)),
					"status": string(statuses[rng.IntN(3)])}
				if rng.IntN(4) == 0 {
					fields["assignee"] = []string{"bob", "carol"}[rng.IntN(2)]
				}
				id, err := graph.CreateNode(tx, levels[l+1], parents, fields)
				if err != nil {
					panic(err)
				}
				byLevel[l+1] = append(byLevel[l+1], id)
			}
		})
	}
	// Dependency Edges. A `done` leaf depends only on `done` leaves, so that
	// no `done` leaf is blocked.
	write(func(tx *graph.Tx) {
		ids := sortedIDs(tx.G.Nodes)
		hasChildren := map[uuid.UUID]bool{}
		for _, e := range tx.G.Edges {
			hasChildren[e.HeadID] = true
		}
		doneLeaf := func(id uuid.UUID) bool { return !hasChildren[id] && tx.G.Nodes[id].Fields.Status == graph.Done }
		link := func(tail, head uuid.UUID) {
			if !doneLeaf(tail) || doneLeaf(head) {
				graph.CreateEdge(tx, graph.CreateEdgeInput{Kind: graph.Dependency, TailID: tail, HeadID: head,
					Version: tx.G.Nodes[tail].Version})
			}
		}
		for range n / 4 {
			link(ids[rng.IntN(len(ids))], ids[rng.IntN(len(ids))])
		}
		for range 3 {
			prev := ids[rng.IntN(len(ids))]
			for range 5 + rng.IntN(20) {
				next := ids[rng.IntN(len(ids))]
				link(next, prev)
				prev = next
			}
		}
	})
	ids := sortedIDs(g.Nodes)
	for range 10 {
		write(func(tx *graph.Tx) {
			for range 1 + n/400 {
				if id := ids[rng.IntN(len(ids))]; tx.G.Nodes[id].Type != graph.TypeProduct {
					graph.SetArchivedMark(tx, id, tx.G.Nodes[id].Version, true)
				}
			}
			for range 1 + n/50 {
				graph.SetWatcher(tx, ids[rng.IntN(len(ids))], []string{"alice", "bob"}[rng.IntN(2)], true)
			}
		})
	}
	// Status changes of leaves, each in its own write; a refused one changes
	// nothing.
	hasChildren := map[uuid.UUID]bool{}
	for _, e := range g.Edges {
		hasChildren[e.HeadID] = e.Kind == graph.Hierarchical || hasChildren[e.HeadID]
	}
	var leaves []uuid.UUID
	for _, id := range ids {
		if !hasChildren[id] {
			leaves = append(leaves, id)
		}
	}
	for range 100 {
		id := leaves[rng.IntN(len(leaves))]
		tryWrite(func(tx *graph.Tx) error {
			v := tx.G.Nodes[id].Version
			return graph.UpdateNode(tx, id, &v, graph.FieldsInput{"status": string(statuses[rng.IntN(3)])})
		})
	}
	return g, feed
}

func sortedIDs[T any](m map[uuid.UUID]T) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	return ids
}

// ── Operations ──────────────────────────────────────────────────────────────

// Op is an operation, as the API runs it: a read, or the steps of a write
// and the response after Complete.
type Op struct {
	Name    string
	Read    func(v *graph.View) (any, error)
	Write   func(tx *graph.Tx) error
	Respond func(tx *graph.Tx) any
}

func nodeResponse(id *uuid.UUID) func(tx *graph.Tx) any {
	return func(tx *graph.Tx) any { return tx.View.NodeJSON(*id) }
}

func edgeResponse(id *uuid.UUID) func(tx *graph.Tx) any {
	return func(tx *graph.Tx) any { return *tx.G.Edges[*id] }
}

func randomID(rng *rand.Rand) uuid.UUID {
	var u uuid.UUID
	binary.LittleEndian.PutUint64(u[:8], rng.Uint64())
	binary.LittleEndian.PutUint64(u[8:], rng.Uint64())
	return u
}

// RandomOp returns a random operation on g, the whole graph of the Product.
// It names Nodes and Edges of g, and at times ids that g doesn't have, and
// gives inputs that the server refuses.
func RandomOp(rng *rand.Rand, g *graph.Graph) Op {
	nodes, edges := sortedIDs(g.Nodes), sortedIDs(g.Edges)
	hasChildren, hasIncoming := map[uuid.UUID]bool{}, map[uuid.UUID]bool{}
	for _, e := range g.Edges {
		hasIncoming[e.HeadID] = true
		if e.Kind == graph.Hierarchical {
			hasChildren[e.HeadID] = true
		}
	}
	chance := func(n int) bool { return rng.IntN(n) == 0 }
	pick := func(ids []uuid.UUID, ok func(uuid.UUID) bool) uuid.UUID {
		if len(ids) == 0 || chance(25) {
			return randomID(rng)
		}
		for range 20 {
			if id := ids[rng.IntN(len(ids))]; ok == nil || ok(id) {
				return id
			}
		}
		return ids[rng.IntN(len(ids))]
	}
	node := func() uuid.UUID { return pick(nodes, nil) }
	leaf := func() uuid.UUID { return pick(nodes, func(id uuid.UUID) bool { return !hasChildren[id] }) }
	edge := func() uuid.UUID { return pick(edges, nil) }
	version := func(id uuid.UUID) int64 {
		v := int64(0)
		if n := g.Nodes[id]; n != nil {
			v = n.Version
		}
		switch rng.IntN(12) {
		case 0:
			return v - 1
		case 1:
			return v + 2
		}
		return v
	}
	tailVersion := func(edgeID uuid.UUID) int64 {
		if e := g.Edges[edgeID]; e != nil {
			return version(e.TailID)
		}
		return version(uuid.Nil)
	}
	include := chance(2)
	typeOf := func(id uuid.UUID) graph.NodeType {
		if n := g.Nodes[id]; n != nil {
			return n.Type
		}
		return graph.TypeTask
	}
	childType := func(t graph.NodeType) graph.NodeType {
		if i := slices.Index(levels, t); i >= 0 && i+1 < len(levels) {
			return levels[i+1]
		}
		return graph.TypeSubtask
	}
	ofType := func(t graph.NodeType) uuid.UUID {
		return pick(nodes, func(id uuid.UUID) bool { return g.Nodes[id].Type == t })
	}

	switch r := rng.IntN(100); {
	case r < 6:
		id := node()
		return Op{Name: "GetNode", Read: func(v *graph.View) (any, error) { return graph.GetNode(v, id, include) }}
	case r < 9:
		id := edge()
		return Op{Name: "GetEdge", Read: func(v *graph.View) (any, error) {
			e, err := graph.GetEdge(v, id, include)
			if err != nil {
				return nil, err
			}
			return *e, nil
		}}
	case r < 14:
		id, from := node(), chance(2)
		return Op{Name: "GetEdges", Read: func(v *graph.View) (any, error) { return graph.GetEdges(v, id, include, from) }}
	case r < 16:
		id, head := edge(), chance(2)
		return Op{Name: "GetEdgeEnd", Read: func(v *graph.View) (any, error) {
			e, err := graph.GetEdge(v, id, include)
			if err != nil {
				return nil, err
			}
			end := e.TailID
			if head {
				end = e.HeadID
			}
			return v.NodeJSON(end), nil
		}}
	case r < 27:
		in := randomListInput(rng, g, node)
		return Op{Name: "ListNodes", Read: func(v *graph.View) (any, error) { return graph.ListNodes(v, in) }}
	case r < 32:
		return Op{Name: "GetNextWork", Read: func(v *graph.View) (any, error) { return graph.NextWork(v), nil }}
	case r < 42:
		parent := pick(nodes, func(id uuid.UUID) bool { return g.Nodes[id].Type != graph.TypeSubtask })
		t := childType(typeOf(parent))
		if chance(15) {
			t = levels[rng.IntN(len(levels))]
		}
		parents := []graph.ParentRef{{ID: parent}}
		for range rng.IntN(3) {
			parents = append(parents, graph.ParentRef{ID: ofType(typeOf(parent))})
		}
		if chance(4) {
			p := rng.Int64N(30) - 5
			if chance(4) {
				p = math.MaxInt64
			}
			parents[rng.IntN(len(parents))].Priority = &p
		}
		fields := graph.FieldsInput{"title": "New"}
		if chance(3) {
			fields["status"] = string(statuses[rng.IntN(3)])
		}
		if chance(20) {
			delete(fields, "title")
		}
		var id uuid.UUID
		return Op{Name: "CreateNode", Write: func(tx *graph.Tx) (err error) {
			id, err = graph.CreateNode(tx, t, parents, fields)
			return err
		}, Respond: nodeResponse(&id)}
	case r < 62:
		id := leaf()
		if chance(6) {
			id = node()
		}
		v := version(id)
		vp := &v
		if chance(30) {
			vp = nil
		}
		fields := graph.FieldsInput{}
		switch rng.IntN(6) {
		case 0:
			fields["title"] = "Renamed"
		case 1:
			fields["assignee"] = []any{nil, "bob"}[rng.IntN(2)]
		case 2:
			fields["status"] = "unknown"
		default:
			fields["status"] = string(statuses[rng.IntN(3)])
		}
		return Op{Name: "UpdateNode", Write: func(tx *graph.Tx) error { return graph.UpdateNode(tx, id, vp, fields) },
			Respond: nodeResponse(&id)}
	case r < 72:
		id, mark := node(), chance(2)
		if n := g.Nodes[id]; n != nil && !chance(4) {
			mark = !n.ArchivedMark
		}
		v := version(id)
		return Op{Name: "SetArchivedMark", Write: func(tx *graph.Tx) error { return graph.SetArchivedMark(tx, id, v, mark) },
			Respond: nodeResponse(&id)}
	case r < 76:
		id := pick(nodes, func(id uuid.UUID) bool { return !hasIncoming[id] || chance(3) })
		v := version(id)
		return Op{Name: "DeleteNode", Write: func(tx *graph.Tx) error { return graph.DeleteNode(tx, id, v) }}
	case r < 78:
		id, user, watch := node(), []string{"alice", "bob"}[rng.IntN(2)], chance(2)
		return Op{Name: "SetWatcher", Write: func(tx *graph.Tx) error { return graph.SetWatcher(tx, id, user, watch) },
			Respond: nodeResponse(&id)}
	case r < 92:
		in := graph.CreateEdgeInput{Kind: graph.Dependency, TailID: node()}
		in.HeadID = node()
		if chance(3) {
			in.Kind = graph.Hierarchical
			if i := slices.Index(levels, typeOf(in.TailID)); i > 0 && !chance(6) {
				in.HeadID = ofType(levels[i-1])
			}
		}
		if chance(4) {
			p := rng.Int64N(30)
			in.Priority = &p
		}
		in.Version = version(in.TailID)
		var id uuid.UUID
		return Op{Name: "CreateEdge", Write: func(tx *graph.Tx) (err error) {
			id, err = graph.CreateEdge(tx, in)
			return err
		}, Respond: edgeResponse(&id)}
	case r < 95:
		id, p := edge(), rng.Int64N(30)
		v := tailVersion(id)
		return Op{Name: "UpdateEdge", Write: func(tx *graph.Tx) error { return graph.UpdateEdge(tx, id, p, v) },
			Respond: edgeResponse(&id)}
	default:
		id := edge()
		v := tailVersion(id)
		return Op{Name: "DeleteEdge", Write: func(tx *graph.Tx) error { return graph.DeleteEdge(tx, id, v) }}
	}
}

func randomListInput(rng *rand.Rand, g *graph.Graph, node func() uuid.UUID) graph.ListInput {
	chance := func(n int) bool { return rng.IntN(n) == 0 }
	var in graph.ListInput
	if chance(5) {
		in.Types = []graph.NodeType{levels[rng.IntN(len(levels))], levels[rng.IntN(len(levels))]}
	}
	if chance(5) {
		in.Statuses = []graph.Status{statuses[rng.IntN(3)]}
	}
	if chance(10) {
		bob := "bob"
		in.Assignees = [][]*string{{nil}, {&bob}, {nil, &bob}}[rng.IntN(3)]
	}
	if chance(10) {
		alice := "alice"
		in.Watchers = [][]*string{{nil}, {&alice}}[rng.IntN(2)]
	}
	in.Archived = [][]bool{nil, nil, {false}, {true}, {true, false}}[rng.IntN(5)]
	if chance(6) {
		in.Blocked = [][]bool{{true}, {false}, {true, false}}[rng.IntN(3)]
	}
	ids := func() []uuid.UUID {
		out := []uuid.UUID{node()}
		if chance(3) {
			out = append(out, node())
		}
		return out
	}
	if chance(2) {
		for range 1 + rng.IntN(2) {
			switch rng.IntN(6) {
			case 0:
				in.ChildrenOf = ids()
			case 1:
				in.DescendantsOf = ids()
			case 2:
				in.ParentsOf = ids()
			case 3:
				in.AncestorsOf = ids()
			case 4:
				in.DependentsOf = ids()
			case 5:
				in.PrerequisitesOf = ids()
			}
		}
	}
	if chance(4) {
		in.SortPriority = true
		if !chance(10) {
			in.ChildrenOf = []uuid.UUID{node()}
		}
	}
	if (!in.SortPriority || chance(10)) && chance(2) {
		in.Direction = []string{"descending", "ascending"}[rng.IntN(2)]
	}
	if chance(3) {
		k, id := rng.Int64N(10), node()
		if n := g.Nodes[id]; n != nil && !chance(4) {
			k = n.Version
		}
		in.StartAfterKey, in.StartAfterID = &k, &id
		if chance(15) {
			in.StartAfterID = nil
		}
	}
	in.Limit = rng.IntN(20)
	return in
}

// ── Running operations ──────────────────────────────────────────────────────

// Outcome is what an operation gives: its response or its refusal, and the
// Change Entry that it records.
type Outcome struct {
	Response string
	Err      string
	Entry    string
	EdgeEnds string
}

func outcome(response any, err error, tx *graph.Tx) Outcome {
	var o Outcome
	if err != nil {
		var refusal *graph.Error
		if errors.As(err, &refusal) {
			o.Err = refusal.Code + ": " + refusal.Message
		} else {
			o.Err = "internal: " + err.Error()
		}
		return o
	}
	text, merr := graph.Marshal(response)
	if merr != nil {
		panic(merr)
	}
	o.Response = string(text)
	if tx != nil && tx.Entry != nil {
		o.Entry, o.EdgeEnds = string(tx.Entry.JSON), string(tx.Entry.EdgeEnds)
	}
	return o
}

func respond(op Op, tx *graph.Tx) any {
	if op.Respond == nil {
		return nil
	}
	return op.Respond(tx)
}

// RunWhole runs op on the whole graph g. It returns the graph and the
// Changefeed values after op.
func RunWhole(g *graph.Graph, feed graph.Feed, op Op, seed uint64) (Outcome, *graph.Graph, graph.Feed) {
	if op.Read != nil {
		resp, err := op.Read(graph.NewView(g))
		return outcome(resp, err, nil), g, feed
	}
	tx := graph.NewTx(g, feed, "alice", Now)
	tx.NewID = IDs(seed)
	err := op.Write(tx)
	if err == nil {
		err = tx.Complete()
	}
	if err != nil {
		return outcome(nil, err, nil), g, feed
	}
	return outcome(respond(op, tx), nil, tx), tx.G, tx.Feed
}

// RunPartial runs op on a partial graph that load fills, as the store runs
// it. It returns the partial graph, and for a write, the Tx of the last run.
func RunPartial(slug string, feed graph.Feed, load func(*graph.Graph, graph.Misses) error, op Op, seed uint64) (Outcome, *graph.Graph, *graph.Tx) {
	g := graph.NewPartialGraph(slug)
	var tx *graph.Tx
	var resp any
	err := graph.Run(g, load, func() (err error) {
		if op.Read != nil {
			resp, err = op.Read(graph.NewView(g))
			return err
		}
		tx = graph.NewTx(g, feed, "alice", Now)
		tx.NewID = IDs(seed)
		if err := op.Write(tx); err != nil {
			return err
		}
		if err := tx.Complete(); err != nil {
			return err
		}
		resp = respond(op, tx)
		return nil
	})
	if err != nil {
		return outcome(nil, err, nil), g, nil
	}
	return outcome(resp, nil, tx), g, tx
}

// ── The in-memory loader ────────────────────────────────────────────────────

// MemoryLoader returns a loader that reads from w, the whole graph of the
// Product. It does what the Neo4j loader of the store does.
func MemoryLoader(w *graph.Graph) func(*graph.Graph, graph.Misses) error {
	return func(g *graph.Graph, m graph.Misses) error {
		var add []uuid.UUID
		for id := range m.Nodes {
			if w.Nodes[id] != nil {
				add = append(add, id)
			} else {
				g.SetAbsentNode(id)
			}
		}
		for id := range m.Edges {
			if e := w.Edges[id]; e != nil {
				add = append(add, e.TailID, e.HeadID)
			} else {
				g.SetAbsentEdge(id)
			}
		}
		for id, r := range m.Rels {
			for _, e := range w.Edges {
				h := e.Kind == graph.Hierarchical
				switch {
				case r&graph.RelChildren != 0 && h && e.HeadID == id:
					add = append(add, e.TailID)
				case r&graph.RelPrereqs != 0 && !h && e.TailID == id:
					add = append(add, e.HeadID)
				case r&graph.RelDependents != 0 && !h && e.HeadID == id:
					add = append(add, e.TailID)
				}
			}
		}
		if m.Product {
			for id, n := range w.Nodes {
				if n.Type == graph.TypeProduct {
					add = append(add, id)
				}
			}
		}
		var scanned []uuid.UUID
		done := false
		if s := m.Scan; s != nil {
			scanned, done = scan(w, g, *s)
			add = append(add, scanned...)
		}
		addWithAncestors(g, w, add)
		if m.Scan != nil {
			g.AddScanned(scanned, done)
		}
		for id, r := range m.Rels {
			g.SetRels(id, r)
		}
		return nil
	}
}

// scan returns the Nodes of w that come next after those that g has scanned,
// in the order of s, and whether no Node comes after them.
func scan(w, g *graph.Graph, s graph.Scan) ([]uuid.UUID, bool) {
	type keyed struct {
		id  uuid.UUID
		key int64
	}
	order := func(a, b keyed) int {
		c := cmp.Compare(a.key, b.key)
		if s.Descending {
			c = -c
		}
		return cmp.Or(c, bytes.Compare(a.id[:], b.id[:]))
	}
	var all []keyed
	for id, n := range w.Nodes {
		all = append(all, keyed{id, n.Version})
	}
	slices.SortFunc(all, order)
	after, hasAfter := keyed{s.StartID, s.StartKey}, s.After
	if done := g.Scanned(); len(done) > 0 {
		last := done[len(done)-1]
		after, hasAfter = keyed{last, g.Nodes[last].Version}, true
	}
	var out []uuid.UUID
	want := s.Count - len(g.Scanned())
	for _, k := range all {
		if hasAfter && order(k, after) <= 0 {
			continue
		}
		if len(out) == want {
			return out, false
		}
		out = append(out, k.id)
	}
	return out, true
}

// addWithAncestors adds the Nodes of w with the ids, and their ancestors, to
// g, with each Edge of w that points to or from a Node that it adds.
func addWithAncestors(g, w *graph.Graph, ids []uuid.UUID) {
	parents := map[uuid.UUID][]uuid.UUID{}
	for _, e := range w.Edges {
		if e.Kind == graph.Hierarchical {
			parents[e.TailID] = append(parents[e.TailID], e.HeadID)
		}
	}
	added := map[uuid.UUID]bool{}
	var nodes []*graph.Node
	for len(ids) > 0 {
		id := ids[0]
		ids = ids[1:]
		if added[id] || g.Nodes[id] != nil {
			continue
		}
		added[id] = true
		n := *w.Nodes[id]
		n.Fields.Outputs = slices.Clone(n.Fields.Outputs)
		n.Watchers = slices.Clone(n.Watchers)
		nodes = append(nodes, &n)
		ids = append(ids, parents[id]...)
	}
	var edges []*graph.Edge
	for _, e := range w.Edges {
		if added[e.TailID] || added[e.HeadID] {
			c := *e
			edges = append(edges, &c)
		}
	}
	g.Add(nodes, edges)
}

// ── Comparing graphs ────────────────────────────────────────────────────────

// Apply returns w with the changes of the write tx, as the store writes them
// to Neo4j: deleting a Node deletes each Edge that points to or from it.
func Apply(w *graph.Graph, tx *graph.Tx) *graph.Graph {
	c := w.Clone()
	for id := range tx.Before.Edges {
		if tx.G.Edges[id] == nil {
			delete(c.Edges, id)
		}
	}
	for id := range tx.Before.Nodes {
		if tx.G.Nodes[id] == nil {
			delete(c.Nodes, id)
			for eid, e := range c.Edges {
				if e.TailID == id || e.HeadID == id {
					delete(c.Edges, eid)
				}
			}
		}
	}
	for id, n := range tx.G.Nodes {
		cn := *n
		c.Nodes[id] = &cn
	}
	for id, e := range tx.G.Edges {
		ce := *e
		c.Edges[id] = &ce
	}
	return c
}

// Diff describes the first difference between 2 graphs, or returns "".
func Diff(a, b *graph.Graph) string {
	for _, id := range sortedIDs(union(a.Nodes, b.Nodes)) {
		x, y := a.Nodes[id], b.Nodes[id]
		if x == nil || y == nil {
			return fmt.Sprintf("node %s: %v != %v", id, x, y)
		}
		fx, _ := graph.Marshal(x.Fields)
		fy, _ := graph.Marshal(y.Fields)
		if x.Type != y.Type || x.Version != y.Version || x.ArchivedMark != y.ArchivedMark ||
			!slices.Equal(x.Watchers, y.Watchers) || !bytes.Equal(fx, fy) {
			return fmt.Sprintf("node %s: %+v %s != %+v %s", id, *x, fx, *y, fy)
		}
	}
	for _, id := range sortedIDs(union(a.Edges, b.Edges)) {
		x, y := a.Edges[id], b.Edges[id]
		if x == nil || y == nil || *x != *y {
			return fmt.Sprintf("edge %s: %v != %v", id, x, y)
		}
	}
	return ""
}

func union[T any](a, b map[uuid.UUID]T) map[uuid.UUID]bool {
	out := map[uuid.UUID]bool{}
	for id := range a {
		out[id] = true
	}
	for id := range b {
		out[id] = true
	}
	return out
}

// NamedOp is an operation with a name that says what it does.
type NamedOp struct {
	Name string
	Op   Op
}

// CommonOps returns common operations on g, the whole graph of a large
// Product: reads of single Nodes and Edges, pages of ListNodes, and writes
// whose changes reach other Nodes.
func CommonOps(rng *rand.Rand, g *graph.Graph) []NamedOp {
	v := graph.NewView(g)
	ids := sortedIDs(g.Nodes)
	find := func(ok func(n *graph.Node) bool) uuid.UUID {
		for _, i := range rng.Perm(len(ids)) {
			if n := g.Nodes[ids[i]]; ok(n) {
				return n.ID
			}
		}
		panic("no Node")
	}
	leaf := func(n *graph.Node) bool { return len(v.Children(n.ID)) == 0 && !v.Archived(n.ID) }
	ofType := func(t graph.NodeType) uuid.UUID {
		return find(func(n *graph.Node) bool { return n.Type == t && !v.Archived(n.ID) })
	}
	// The Prerequisite with the longest chain of Dependents.
	depth := map[uuid.UUID]int{}
	var chain func(id uuid.UUID) int
	chain = func(id uuid.UUID) int {
		if d, ok := depth[id]; ok {
			return d
		}
		d := 0
		for _, e := range v.Dependents(id) {
			d = max(d, 1+chain(e.TailID))
		}
		depth[id] = d
		return d
	}
	deepest := ids[0]
	for _, id := range ids {
		if leaf(g.Nodes[id]) && chain(id) > chain(deepest) {
			deepest = id
		}
	}
	var edgeIDs, depIDs []uuid.UUID
	for _, id := range sortedIDs(g.Edges) {
		if g.Edges[id].Kind == graph.Dependency {
			depIDs = append(depIDs, id)
		} else {
			edgeIDs = append(edgeIDs, id)
		}
	}
	read := func(name string, fn func(v *graph.View) (any, error)) NamedOp {
		return NamedOp{name, Op{Name: name, Read: fn}}
	}
	list := func(name string, in graph.ListInput) NamedOp {
		return read(name, func(v *graph.View) (any, error) { return graph.ListNodes(v, in) })
	}
	write := func(name string, id uuid.UUID, fn func(tx *graph.Tx) error) NamedOp {
		return NamedOp{name, Op{Name: name, Write: fn, Respond: nodeResponse(&id)}}
	}
	version := func(id uuid.UUID) *int64 { v := g.Nodes[id].Version; return &v }
	status := func(id uuid.UUID, s graph.Status) func(tx *graph.Tx) error {
		return func(tx *graph.Tx) error {
			return graph.UpdateNode(tx, id, version(id), graph.FieldsInput{"status": string(s)})
		}
	}
	subtask, task, req, feature, goal := ofType(graph.TypeSubtask), ofType(graph.TypeTask), ofType(graph.TypeRequirement),
		ofType(graph.TypeFeature), ofType(graph.TypeGoal)
	notStarted := find(func(n *graph.Node) bool { return leaf(n) && n.Fields.Status == graph.NotStarted })
	inProgress := find(func(n *graph.Node) bool { return leaf(n) && n.Fields.Status == graph.InProgress })
	marked := find(func(n *graph.Node) bool { return n.ArchivedMark })
	free := find(func(n *graph.Node) bool { return len(v.Children(n.ID))+len(v.Dependents(n.ID)) == 0 })
	depLeaf := find(func(n *graph.Node) bool { return leaf(n) && n.Fields.Status != graph.Done })
	edge, dep := edgeIDs[rng.IntN(len(edgeIDs))], depIDs[rng.IntN(len(depIDs))]
	var newID uuid.UUID
	ten := int64(10)
	return []NamedOp{
		read("GetNode of a subtask", func(v *graph.View) (any, error) { return graph.GetNode(v, subtask, false) }),
		read("GetEdge", func(v *graph.View) (any, error) { return graph.GetEdge(v, edge, true) }),
		read("GetEdgesFrom a task", func(v *graph.View) (any, error) { return graph.GetEdges(v, task, true, true) }),
		read("GetEdgesTo a feature", func(v *graph.View) (any, error) { return graph.GetEdges(v, feature, true, false) }),
		list("ListNodes, no filter", graph.ListInput{}),
		list("ListNodes, ascending, limit 200", graph.ListInput{Direction: "ascending", Limit: 200}),
		list("ListNodes, childrenOf a feature", graph.ListInput{ChildrenOf: []uuid.UUID{feature}}),
		list("ListNodes, descendantsOf a requirement", graph.ListInput{DescendantsOf: []uuid.UUID{req}}),
		list("ListNodes, sort priority, childrenOf a goal", graph.ListInput{ChildrenOf: []uuid.UUID{goal}, SortPriority: true}),
		list("ListNodes, type task, status in_progress", graph.ListInput{Types: []graph.NodeType{graph.TypeTask},
			Statuses: []graph.Status{graph.InProgress}}),
		list("ListNodes, blocked, limit 20", graph.ListInput{Blocked: []bool{true}, Limit: 20}),
		read("GetNextWork", func(v *graph.View) (any, error) { return graph.NextWork(v), nil }),
		{"CreateNode under a task", Op{Name: "CreateNode", Write: func(tx *graph.Tx) (err error) {
			newID, err = graph.CreateNode(tx, graph.TypeSubtask, []graph.ParentRef{{ID: task}}, graph.FieldsInput{"title": "New"})
			return err
		}, Respond: nodeResponse(&newID)}},
		write("UpdateNode title of a leaf", notStarted, func(tx *graph.Tx) error {
			return graph.UpdateNode(tx, notStarted, version(notStarted), graph.FieldsInput{"title": "Renamed"})
		}),
		write("UpdateNode not_started leaf to in_progress", notStarted, status(notStarted, graph.InProgress)),
		write("UpdateNode in_progress leaf to done", inProgress, status(inProgress, graph.Done)),
		write(fmt.Sprintf("UpdateNode, chain of %d Dependents, to done", chain(deepest)), deepest, status(deepest, graph.Done)),
		write("ArchiveNode a requirement", req, func(tx *graph.Tx) error { return graph.SetArchivedMark(tx, req, *version(req), true) }),
		write("UnarchiveNode a marked Node", marked, func(tx *graph.Tx) error {
			return graph.SetArchivedMark(tx, marked, *version(marked), false)
		}),
		write("WatchNode a subtask", subtask, func(tx *graph.Tx) error { return graph.SetWatcher(tx, subtask, "zoe", true) }),
		{"CreateEdge dependency between leaves", Op{Name: "CreateEdge", Write: func(tx *graph.Tx) (err error) {
			newID, err = graph.CreateEdge(tx, graph.CreateEdgeInput{Kind: graph.Dependency, TailID: depLeaf, HeadID: notStarted,
				Version: *version(depLeaf)})
			return err
		}, Respond: edgeResponse(&newID)}},
		{"UpdateEdge priority", Op{Name: "UpdateEdge", Write: func(tx *graph.Tx) error {
			return graph.UpdateEdge(tx, edge, ten, *version(g.Edges[edge].TailID))
		}, Respond: edgeResponse(&edge)}},
		{"DeleteEdge dependency", Op{Name: "DeleteEdge", Write: func(tx *graph.Tx) error {
			return graph.DeleteEdge(tx, dep, *version(g.Edges[dep].TailID))
		}}},
		{"DeleteNode with no incoming Edge", Op{Name: "DeleteNode", Write: func(tx *graph.Tx) error {
			return graph.DeleteNode(tx, free, *version(free))
		}}},
	}
}
