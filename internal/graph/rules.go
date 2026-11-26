package graph

import (
	"slices"

	"github.com/google/uuid"
)

// View holds the relations of a Graph and the value of `archived` of each of
// its Nodes (A2). Build a new View after a change to the Edges or to the
// archived marks. In a partial Graph, the methods that read a relation record
// what the Graph doesn't hold as missed.
type View struct {
	G          *Graph
	parents    map[uuid.UUID][]*Edge // Hierarchical Edges that point from the Node
	children   map[uuid.UUID][]*Edge // Hierarchical Edges that point to the Node
	prereqs    map[uuid.UUID][]*Edge // Dependency Edges that point from the Node
	dependents map[uuid.UUID][]*Edge // Dependency Edges that point to the Node
	archived   map[uuid.UUID]bool
}

func NewView(g *Graph) *View {
	v := &View{
		G:          g,
		parents:    map[uuid.UUID][]*Edge{},
		children:   map[uuid.UUID][]*Edge{},
		prereqs:    map[uuid.UUID][]*Edge{},
		dependents: map[uuid.UUID][]*Edge{},
	}
	for _, e := range g.Edges {
		if e.Kind == Hierarchical {
			v.parents[e.TailID] = append(v.parents[e.TailID], e)
			v.children[e.HeadID] = append(v.children[e.HeadID], e)
		} else {
			v.prereqs[e.TailID] = append(v.prereqs[e.TailID], e)
			v.dependents[e.HeadID] = append(v.dependents[e.HeadID], e)
		}
	}
	v.archived = v.computeArchived()
	return v
}

// Parents returns the Hierarchical Edges that point from the Node.
func (v *View) Parents(id uuid.UUID) []*Edge {
	v.G.need(id, 0)
	return v.parents[id]
}

// Children returns the Hierarchical Edges that point to the Node.
func (v *View) Children(id uuid.UUID) []*Edge {
	v.G.need(id, RelChildren)
	return v.children[id]
}

// Prereqs returns the Dependency Edges that point from the Node.
func (v *View) Prereqs(id uuid.UUID) []*Edge {
	v.G.need(id, RelPrereqs)
	return v.prereqs[id]
}

// Dependents returns the Dependency Edges that point to the Node.
func (v *View) Dependents(id uuid.UUID) []*Edge {
	v.G.need(id, RelDependents)
	return v.dependents[id]
}

// Archived reports whether the Node is archived (A2).
func (v *View) Archived(id uuid.UUID) bool {
	v.G.need(id, 0)
	return v.archived[id]
}

// computeArchived gives the value of A2 for each Node: a Node is archived if
// it carries the archived mark, or if it has at least one Parent and each of
// its Parents is archived.
func (v *View) computeArchived() map[uuid.UUID]bool {
	archived := map[uuid.UUID]bool{}
	seen := map[uuid.UUID]bool{}
	var visit func(id uuid.UUID) bool
	visit = func(id uuid.UUID) bool {
		if seen[id] {
			return archived[id]
		}
		seen[id] = true
		a := v.G.Nodes[id].ArchivedMark
		if !a {
			parents := v.parents[id]
			a = len(parents) > 0
			for _, e := range parents {
				if !visit(e.HeadID) {
					a = false
					break
				}
			}
		}
		archived[id] = a
		return a
	}
	for id := range v.G.Nodes {
		visit(id)
	}
	return archived
}

// Leaf reports whether the Node has no Children (T1).
func (v *View) Leaf(id uuid.UUID) bool { return len(v.Children(id)) == 0 }

// Blocked reports whether at least one Prerequisite of the Node is neither
// `done` nor archived (T2).
func (v *View) Blocked(id uuid.UUID) bool {
	for _, e := range v.Prereqs(id) {
		if v.G.Nodes[e.HeadID].Fields.Status != Done && !v.archived[e.HeadID] {
			return true
		}
	}
	return false
}

// Available reports whether the Node is available (W1).
func (v *View) Available(id uuid.UUID) bool {
	n := v.G.Nodes[id]
	return v.Leaf(id) && n.Fields.Status == NotStarted && !v.archived[id] && n.Fields.Assignee == nil && !v.Blocked(id)
}

func (v *View) NodeJSON(id uuid.UUID) NodeJSON { return nodeJSON(v.G.Nodes[id], v.archived[id]) }

// deriveStatuses sets the status of each Node that isn't a leaf to its derived
// status (T3, T4), and leaves the status of each leaf as it is (T5). The
// status of a Node depends on the statuses of its Children and its
// Prerequisites, and Y2 forbids a cycle of these relations, so a visit in
// depth-first order reaches each Node after the Nodes that it depends on. In
// a partial Graph, a Node whose Children or Prerequisites the Graph doesn't
// hold keeps its status; needAffected makes the Graph hold them for each Node
// whose derived status can change.
func (v *View) deriveStatuses() {
	seen := map[uuid.UUID]bool{}
	var visit func(id uuid.UUID) Status
	visit = func(id uuid.UUID) Status {
		n := v.G.Nodes[id]
		if seen[id] || !v.G.has(id, RelChildren|RelPrereqs) {
			return n.Fields.Status
		}
		seen[id] = true
		for _, e := range v.prereqs[id] {
			visit(e.HeadID)
		}
		if len(v.children[id]) == 0 {
			return n.Fields.Status
		}
		var statuses []Status
		for _, e := range v.children[id] {
			s := visit(e.TailID)
			if !v.G.Nodes[e.TailID].ArchivedMark {
				statuses = append(statuses, s)
			}
		}
		n.Fields.Status = derivedStatus(statuses, v.Blocked(id))
		return n.Fields.Status
	}
	for _, id := range sortedNodeIDs(v.G.Nodes) {
		visit(id)
	}
}

// derivedStatus applies the rules of step 2 of T3 to the statuses of the
// Children that step 1 leaves.
func derivedStatus(children []Status, blocked bool) Status {
	all := func(s Status) bool {
		return !slices.ContainsFunc(children, func(c Status) bool { return c != s })
	}
	switch {
	case len(children) == 0 && !blocked:
		return Done
	case len(children) == 0:
		return InProgress
	case all(Done) && !blocked:
		return Done
	case all(NotStarted):
		return NotStarted
	}
	return InProgress
}

// formsCycle reports whether a new Edge of kind from tail to head would form a
// cycle of either kind (Y1, Y2). It counts archived Nodes (Y3).
func (v *View) formsCycle(kind EdgeKind, tail, head uuid.UUID) bool {
	// Y1: the Tail Node is reachable from the Head Node through Parents and
	// Prerequisites.
	parentsAndPrereqs := func(id uuid.UUID) []uuid.UUID {
		return append(heads(v.parents[id]), heads(v.Prereqs(id))...)
	}
	if reachable(head, tail, parentsAndPrereqs) {
		return true
	}
	// Y2: through Children and Prerequisites, the Parent is reachable from the
	// Child, or the Dependent is reachable from the Prerequisite.
	childrenAndPrereqs := func(id uuid.UUID) []uuid.UUID {
		return append(tails(v.Children(id)), heads(v.Prereqs(id))...)
	}
	if kind == Hierarchical {
		return reachable(tail, head, childrenAndPrereqs)
	}
	return reachable(head, tail, childrenAndPrereqs)
}

// reachable reports whether target is start, or is reachable from start
// through next.
func reachable(start, target uuid.UUID, next func(uuid.UUID) []uuid.UUID) bool {
	seen := map[uuid.UUID]bool{start: true}
	queue := []uuid.UUID{start}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if id == target {
			return true
		}
		for _, n := range next(id) {
			if !seen[n] {
				seen[n] = true
				queue = append(queue, n)
			}
		}
	}
	return false
}

func heads(edges []*Edge) []uuid.UUID {
	ids := make([]uuid.UUID, len(edges))
	for i, e := range edges {
		ids[i] = e.HeadID
	}
	return ids
}

func tails(edges []*Edge) []uuid.UUID {
	ids := make([]uuid.UUID, len(edges))
	for i, e := range edges {
		ids[i] = e.TailID
	}
	return ids
}
