package graph

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// A partial Graph holds part of the graph of a Product, so that an operation
// reads only what its rules read. It holds each Edge between 2 of its Nodes,
// and each Parent of each of its Nodes, so each ancestor and the value of
// `archived` of each of its Nodes. It holds the Children, the Prerequisites,
// or the Dependents of a Node only if a loader has added them (Rel).
//
// When an operation reads a Node, an Edge, or a relation that a partial Graph
// doesn't hold, the Graph records it as missed, and the read gives what the
// Graph holds. Run then discards the result, adds what was missed, and runs
// the operation again, until a run misses nothing. A write also misses each
// relation that the rules read to find the changes that the write makes to
// Nodes that it doesn't name (see needAffected). This relies on 2 things that
// each write keeps true: each Node with Children has its derived status, and
// no `done` leaf is blocked.

// Rel is a set of relations of a Node.
type Rel uint8

const (
	RelChildren Rel = 1 << iota
	RelPrereqs
	RelDependents
)

// Misses holds what the runs of an operation read from a partial Graph that
// the Graph doesn't hold.
type Misses struct {
	Nodes   map[uuid.UUID]bool // Nodes to look up by id
	Edges   map[uuid.UUID]bool // Edges to look up by id
	Rels    map[uuid.UUID]Rel  // relations of Nodes that the Graph holds
	Product bool               // the Product Node
	Scan    *Scan              // more Nodes in the order of a Scan
}

// empty reports whether nothing was missed.
func (m *Misses) empty() bool {
	return len(m.Nodes) == 0 && len(m.Edges) == 0 && len(m.Rels) == 0 && !m.Product && m.Scan == nil
}

// Scan asks for the Nodes of the Product in the order of the sort
// lastUpdated of ListNodes, after the start key and id if After is true,
// until the Graph has scanned at least Count of them.
type Scan struct {
	Descending bool
	After      bool
	StartKey   int64
	StartID    uuid.UUID
	Count      int
}

type partial struct {
	rels        map[uuid.UUID]Rel
	absentNodes map[uuid.UUID]bool
	absentEdges map[uuid.UUID]bool
	scanned     []uuid.UUID
	scanDone    bool
	miss        Misses
}

// NewPartialGraph returns a partial Graph that holds nothing.
func NewPartialGraph(slug string) *Graph {
	g := NewGraph(slug)
	g.part = &partial{rels: map[uuid.UUID]Rel{}, absentNodes: map[uuid.UUID]bool{}, absentEdges: map[uuid.UUID]bool{}}
	g.part.resetMisses()
	return g
}

func (p *partial) resetMisses() {
	p.miss = Misses{Nodes: map[uuid.UUID]bool{}, Edges: map[uuid.UUID]bool{}, Rels: map[uuid.UUID]Rel{}}
}

// Add adds the Nodes that g doesn't hold, and then each Edge whose ends g
// holds. The loader gives each Edge that points to or from each Node that it
// adds, and adds each Parent of each Node that it adds.
func (g *Graph) Add(nodes []*Node, edges []*Edge) {
	for _, n := range nodes {
		if g.Nodes[n.ID] == nil {
			g.Nodes[n.ID] = n
		}
	}
	for _, e := range edges {
		if g.Edges[e.ID] == nil && g.Nodes[e.TailID] != nil && g.Nodes[e.HeadID] != nil {
			g.Edges[e.ID] = e
		}
	}
}

// SetRels records that g holds the relations r of the Node.
func (g *Graph) SetRels(id uuid.UUID, r Rel) { g.part.rels[id] |= r }

// SetAbsentNode records that the Product has no Node with the id.
func (g *Graph) SetAbsentNode(id uuid.UUID) { g.part.absentNodes[id] = true }

// SetAbsentEdge records that the Product has no Edge with the id.
func (g *Graph) SetAbsentEdge(id uuid.UUID) { g.part.absentEdges[id] = true }

// Scanned returns the Nodes that g has scanned, in the order of the Scan.
func (g *Graph) Scanned() []uuid.UUID { return g.part.scanned }

// AddScanned records that g holds the Nodes that come next in the order of
// the Scan. done is true if no Node comes after them.
func (g *Graph) AddScanned(ids []uuid.UUID, done bool) {
	g.part.scanned = append(g.part.scanned, ids...)
	g.part.scanDone = done
}

// size measures what g holds, so that Run can check that each load adds
// something.
func (g *Graph) size() int {
	p := g.part
	n := len(g.Nodes) + len(g.Edges) + len(p.absentNodes) + len(p.absentEdges) + len(p.scanned)
	for _, r := range p.rels {
		n += int(r)
	}
	if p.scanDone {
		n++
	}
	return n
}

// errMissed ends a step that has read something that the partial Graph
// doesn't hold. Run discards it.
var errMissed = errors.New("graph: the operation read a part of the graph that the partial graph doesn't hold")

// Run runs fn, which runs an operation on g, until a run misses nothing, and
// returns the result of that run. After each run that misses something, load
// adds what the run missed to g.
func Run(g *Graph, load func(g *Graph, m Misses) error, fn func() error) error {
	for {
		g.part.resetMisses()
		err := fn()
		m := g.part.miss
		if m.empty() {
			return err
		}
		before := g.size()
		if err := load(g, m); err != nil {
			return err
		}
		// Each load adds something, and g can't grow past the graph of the
		// Product, so the loop ends.
		if g.size() == before {
			return fmt.Errorf("graph: the loader added nothing for the misses %+v", m)
		}
	}
}

// node returns the Node with the id, or nil if the Product has none.
func (g *Graph) node(id uuid.UUID) *Node {
	n := g.Nodes[id]
	if n == nil && g.part != nil && !g.part.absentNodes[id] {
		g.part.miss.Nodes[id] = true
	}
	return n
}

// edge returns the Edge with the id, or nil if the Product has none.
func (g *Graph) edge(id uuid.UUID) *Edge {
	e := g.Edges[id]
	if e == nil && g.part != nil && !g.part.absentEdges[id] {
		g.part.miss.Edges[id] = true
	}
	return e
}

// has reports whether g holds the Node and its relations r.
func (g *Graph) has(id uuid.UUID, r Rel) bool {
	return g.part == nil || (g.Nodes[id] != nil && g.part.rels[id]&r == r)
}

// need reports whether g holds the Node and its relations r, and records what
// it doesn't hold as missed. It reports false for a Node that the Product
// doesn't have.
func (g *Graph) need(id uuid.UUID, r Rel) bool {
	if g.has(id, r) {
		return true
	}
	if g.node(id) != nil {
		g.part.miss.Rels[id] |= r &^ g.part.rels[id]
	}
	return false
}

// missed reports whether a run has missed something so far.
func (g *Graph) missed() bool { return g.part != nil && !g.part.miss.empty() }

// setAll records that g holds each relation of a Node that a write creates.
func (g *Graph) setAll(id uuid.UUID) {
	if g.part != nil {
		g.SetRels(id, RelChildren|RelPrereqs|RelDependents)
	}
}

// scan returns the Nodes to sort for ListNodes when no relation filter
// selects them: in a whole Graph, each Node; in a partial Graph, the Nodes
// that come first after the start, in the order of s. more is true if more
// Nodes may come after those.
func (g *Graph) scan(s Scan) (ids []uuid.UUID, more bool) {
	if g.part == nil {
		for id := range g.Nodes {
			ids = append(ids, id)
		}
		return ids, false
	}
	return g.part.scanned, !g.part.scanDone
}

// scanMore asks for more Nodes in the order of s than g has scanned: at
// least s.Count, and at least 2 times as many.
func (g *Graph) scanMore(s Scan) {
	s.Count = max(s.Count, 2*len(g.part.scanned))
	g.part.miss.Scan = &s
}
