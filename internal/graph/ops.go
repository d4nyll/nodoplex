package graph

import (
	"math"
	"slices"

	"github.com/google/uuid"
)

// This file holds the steps of the operations of the base specification that
// read or change the graph of one Product. The caller makes the checks before
// step 1, runs "complete a write" (Tx.Complete) after the steps of a write,
// and runs each write atomically.

// ── Products ────────────────────────────────────────────────────────────────

// CreateProduct runs steps 1 to 3.1 of CreateProduct. slugTaken is true if a
// Product that exists, or a deleted Product, has the slug.
func CreateProduct(tx *Tx, fields FieldsInput, slugTaken bool) (uuid.UUID, error) {
	if err := checkFields(TypeProduct, fields, true); err != nil {
		return uuid.Nil, err
	}
	if slugTaken {
		return uuid.Nil, Refuse(CodeSlugTaken, "the slug %q is taken", tx.G.Slug)
	}
	n := newNode(tx, TypeProduct, fields)
	slug := tx.G.Slug
	n.Fields.Slug = &slug
	tx.G.Nodes[n.ID] = n
	return n.ID, nil
}

func newNode(tx *Tx, t NodeType, fields FieldsInput) *Node {
	n := &Node{ID: tx.NewID(), Type: t, Watchers: []string{}}
	n.Fields = Fields{Product: tx.G.Slug, Outputs: []Output{}, Status: NotStarted}
	applyFields(&n.Fields, fields)
	return n
}

// ── Nodes ───────────────────────────────────────────────────────────────────

type ParentRef struct {
	ID       uuid.UUID
	Priority *int64
}

// CreateNode runs steps 1 to 6.2 of CreateNode.
func CreateNode(tx *Tx, t NodeType, parents []ParentRef, fields FieldsInput) (uuid.UUID, error) {
	if t == TypeProduct {
		return uuid.Nil, invalid("CreateNode can't create a product; use CreateProduct")
	}
	for i, p := range parents {
		if slices.ContainsFunc(parents[:i], func(q ParentRef) bool { return q.ID == p.ID }) {
			return uuid.Nil, invalid("parents names %s more than one time", p.ID)
		}
	}
	if t == TypeVision && len(parents) > 1 {
		return uuid.Nil, invalid("a vision has exactly one Parent")
	}
	if err := checkFields(t, fields, true); err != nil {
		return uuid.Nil, err
	}
	for _, p := range parents {
		parent := tx.G.node(p.ID)
		if parent == nil {
			return uuid.Nil, Refuse(CodeInvalidReference, "no Node in the Product has the id %s", p.ID)
		}
		if parent.Type != parentType[t] {
			return uuid.Nil, Refuse(CodeInvalidReference, "the Parent of a %s must be a %s, not a %s", t, parentType[t], parent.Type)
		}
	}
	n := newNode(tx, t, fields)
	tx.G.Nodes[n.ID] = n
	tx.G.setAll(n.ID)
	for _, p := range parents {
		if _, err := addHierarchicalEdge(tx, n.ID, p.ID, p.Priority); err != nil {
			return uuid.Nil, err
		}
	}
	return n.ID, nil
}

// addHierarchicalEdge adds a Hierarchical Edge with the given priority, or
// with the priority that G7 gives if priority is nil.
func addHierarchicalEdge(tx *Tx, child, parent uuid.UUID, priority *int64) (*Edge, error) {
	e := &Edge{ID: tx.NewID(), Kind: Hierarchical, TailID: child, HeadID: parent}
	if priority != nil {
		e.Priority = *priority
	} else {
		p, err := nextPriority(tx.G, parent)
		if err != nil {
			return nil, err
		}
		e.Priority = p
	}
	tx.G.Edges[e.ID] = e
	return e, nil
}

// nextPriority gives the priority of G7: 1 greater than the largest priority
// of the Hierarchical Edges that point to the Parent, or 10 if there are none.
func nextPriority(g *Graph, parent uuid.UUID) (int64, error) {
	g.need(parent, RelChildren)
	found := false
	var largest int64
	for _, e := range g.Edges {
		if e.Kind == Hierarchical && e.HeadID == parent && (!found || e.Priority > largest) {
			found, largest = true, e.Priority
		}
	}
	if !found {
		return 10, nil
	}
	if largest == math.MaxInt64 {
		return 0, Refuse(CodePriorityOutOfRange, "the largest priority of the Children of %s is 2^63 - 1, so the server can't give a larger one", parent)
	}
	return largest + 1, nil
}

// findNode returns the Node with the id, or not_found.
func findNode(g *Graph, id uuid.UUID) (*Node, error) {
	n := g.node(id)
	if n == nil {
		return nil, Refuse(CodeNotFound, "no Node in the Product has the id %s", id)
	}
	return n, nil
}

func checkVersion(n *Node, version int64) error {
	if n.Version != version {
		return Refuse(CodeVersionConflict, "the version of Node %s is %d, not %d", n.ID, n.Version, version)
	}
	return nil
}

// GetNode runs the steps of GetNode.
func GetNode(v *View, id uuid.UUID, includeArchived bool) (NodeJSON, error) {
	if _, err := findNode(v.G, id); err != nil {
		return NodeJSON{}, err
	}
	if v.Archived(id) && !includeArchived {
		return NodeJSON{}, Refuse(CodeNotFound, "the Node %s is archived", id)
	}
	return v.NodeJSON(id), nil
}

// UpdateNode runs steps 1 to 6.1 of UpdateNode.
func UpdateNode(tx *Tx, id uuid.UUID, version *int64, fields FieldsInput) error {
	n, err := findNode(tx.G, id)
	if err != nil {
		return err
	}
	if err := checkFields(n.Type, fields, false); err != nil {
		return err
	}
	if version == nil {
		// Each Field is tracked, so each UpdateNode names a tracked Field.
		return invalid("version is required, because fields names a tracked Field")
	}
	if err := checkVersion(n, *version); err != nil {
		return err
	}
	if _, ok := fields["status"]; ok && !NewView(tx.G).Leaf(id) {
		return Refuse(CodeStatusIsDerived, "the Node %s has Children, so its status is derived", id)
	}
	applyFields(&n.Fields, fields)
	return nil
}

// SetArchivedMark runs steps 1 to 4.1 of ArchiveNode (mark true) or of
// UnarchiveNode (mark false).
func SetArchivedMark(tx *Tx, id uuid.UUID, version int64, mark bool) error {
	n, err := findNode(tx.G, id)
	if err != nil {
		return err
	}
	if err := checkVersion(n, version); err != nil {
		return err
	}
	n.ArchivedMark = mark
	return nil
}

// DeleteNode runs steps 1 to 4.2 of DeleteNode.
func DeleteNode(tx *Tx, id uuid.UUID, version int64) error {
	n, err := findNode(tx.G, id)
	if err != nil {
		return err
	}
	if err := checkVersion(n, version); err != nil {
		return err
	}
	tx.G.need(id, RelChildren|RelDependents|RelPrereqs)
	for _, e := range tx.G.Edges {
		if e.HeadID == id {
			return Refuse(CodeHasIncomingEdges, "an Edge points to the Node %s", id)
		}
	}
	for eid, e := range tx.G.Edges {
		if e.TailID == id {
			delete(tx.G.Edges, eid)
		}
	}
	delete(tx.G.Nodes, id)
	if n.Type == TypeProduct {
		tx.ProductDeleted = true
	}
	return nil
}

// SetWatcher runs the steps of WatchNode (watch true) or of UnwatchNode
// (watch false).
func SetWatcher(tx *Tx, id uuid.UUID, userID string, watch bool) error {
	n, err := findNode(tx.G, id)
	if err != nil {
		return err
	}
	i, found := slices.BinarySearch(n.Watchers, userID)
	switch {
	case watch && !found:
		n.Watchers = slices.Insert(n.Watchers, i, userID)
	case !watch && found:
		n.Watchers = slices.Delete(n.Watchers, i, i+1)
	}
	return nil
}

// ── Edges ───────────────────────────────────────────────────────────────────

type CreateEdgeInput struct {
	Kind     EdgeKind
	TailID   uuid.UUID
	HeadID   uuid.UUID
	Priority *int64
	Version  int64
}

// CreateEdge runs steps 1 to 8.1 of CreateEdge.
func CreateEdge(tx *Tx, in CreateEdgeInput) (uuid.UUID, error) {
	if in.Kind == Dependency && in.Priority != nil {
		return uuid.Nil, invalid("a Dependency Edge has no priority")
	}
	tail, head := tx.G.node(in.TailID), tx.G.node(in.HeadID)
	if tail == nil {
		return uuid.Nil, Refuse(CodeInvalidReference, "no Node in the Product has the tailId %s", in.TailID)
	}
	if head == nil {
		return uuid.Nil, Refuse(CodeInvalidReference, "no Node in the Product has the headId %s", in.HeadID)
	}
	if err := checkVersion(tail, in.Version); err != nil {
		return uuid.Nil, err
	}
	if in.Kind == Hierarchical && head.Type != parentType[tail.Type] {
		return uuid.Nil, Refuse(CodeInvalidReference, "a %s can't be the Parent of a %s", head.Type, tail.Type)
	}
	for _, e := range tx.G.Edges {
		if (e.TailID == in.TailID && e.HeadID == in.HeadID) || (e.TailID == in.HeadID && e.HeadID == in.TailID) {
			return uuid.Nil, Refuse(CodeAlreadyConnected, "the Edge %s already connects the 2 Nodes", e.ID)
		}
	}
	if NewView(tx.G).formsCycle(in.Kind, in.TailID, in.HeadID) {
		return uuid.Nil, Refuse(CodeCycle, "the new Edge would form a cycle")
	}
	if in.Kind == Hierarchical {
		e, err := addHierarchicalEdge(tx, in.TailID, in.HeadID, in.Priority)
		if err != nil {
			return uuid.Nil, err
		}
		return e.ID, nil
	}
	e := &Edge{ID: tx.NewID(), Kind: Dependency, TailID: in.TailID, HeadID: in.HeadID}
	tx.G.Edges[e.ID] = e
	return e.ID, nil
}

// GetEdge runs steps 1 and 2 of GetEdge.
func GetEdge(v *View, id uuid.UUID, includeArchived bool) (*Edge, error) {
	e := v.G.edge(id)
	if e == nil {
		return nil, Refuse(CodeNotFound, "no Edge in the Product has the id %s", id)
	}
	if !includeArchived && (v.Archived(e.TailID) || v.Archived(e.HeadID)) {
		return nil, Refuse(CodeNotFound, "the Edge %s has an archived Node at an end", id)
	}
	return e, nil
}

// GetEdges runs the steps of GetEdgesFrom (from true) or of GetEdgesTo (from
// false). It returns the Edges in id order.
func GetEdges(v *View, id uuid.UUID, includeArchived, from bool) ([]Edge, error) {
	if _, err := GetNode(v, id, includeArchived); err != nil {
		return nil, err
	}
	if from {
		v.G.need(id, RelPrereqs)
	} else {
		v.G.need(id, RelChildren|RelDependents)
	}
	edges := []Edge{}
	for _, e := range v.G.Edges {
		end := e.TailID
		if from {
			end = e.HeadID
		}
		if (from && e.TailID == id || !from && e.HeadID == id) && (includeArchived || !v.archived[end]) {
			edges = append(edges, *e)
		}
	}
	slices.SortFunc(edges, func(a, b Edge) int { return idCmp(a.ID, b.ID) })
	return edges, nil
}

func findEdge(g *Graph, id uuid.UUID) (*Edge, error) {
	e := g.edge(id)
	if e == nil {
		return nil, Refuse(CodeNotFound, "no Edge in the Product has the id %s", id)
	}
	return e, nil
}

// UpdateEdge runs steps 1 to 5.1 of UpdateEdge.
func UpdateEdge(tx *Tx, id uuid.UUID, priority, version int64) error {
	e, err := findEdge(tx.G, id)
	if err != nil {
		return err
	}
	if err := checkVersion(tx.G.Nodes[e.TailID], version); err != nil {
		return err
	}
	if e.Kind == Dependency {
		return invalid("a Dependency Edge has no priority")
	}
	e.Priority = priority
	return nil
}

// DeleteEdge runs steps 1 to 4.1 of DeleteEdge.
func DeleteEdge(tx *Tx, id uuid.UUID, version int64) error {
	e, err := findEdge(tx.G, id)
	if err != nil {
		return err
	}
	if err := checkVersion(tx.G.Nodes[e.TailID], version); err != nil {
		return err
	}
	if e.Kind == Hierarchical && len(NewView(tx.G).parents[e.TailID]) == 1 {
		return Refuse(CodeParentRequired, "the Edge %s is the only Hierarchical Edge from its Child", id)
	}
	delete(tx.G.Edges, id)
	return nil
}
