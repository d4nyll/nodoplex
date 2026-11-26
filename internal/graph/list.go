package graph

import (
	"cmp"
	"slices"
	"strings"

	"github.com/google/uuid"
)

// ListInput is the input of ListNodes. A nil slice is a filter that the input
// doesn't give. In Assignees and Watchers, a nil pointer is no value.
type ListInput struct {
	Types           []NodeType
	Statuses        []Status
	Assignees       []*string
	Watchers        []*string
	Archived        []bool
	Blocked         []bool
	ChildrenOf      []uuid.UUID
	DescendantsOf   []uuid.UUID
	ParentsOf       []uuid.UUID
	AncestorsOf     []uuid.UUID
	DependentsOf    []uuid.UUID
	PrerequisitesOf []uuid.UUID
	Search          *string // matches a title or description that contains it, ignoring case
	SortPriority    bool    // the sort `priority`; otherwise `lastUpdated`
	Direction       string  // "", "descending", or "ascending"
	StartAfterKey   *int64
	StartAfterID    *uuid.UUID
	Limit           int // 0 means the default, 50
}

type NodePage struct {
	Nodes   []NodeJSON `json:"nodes"`
	LastKey *int64     `json:"lastKey,omitempty"`
}

// ListNodes runs the steps of ListNodes.
func ListNodes(v *View, in ListInput) (NodePage, error) {
	if in.SortPriority && len(in.ChildrenOf) != 1 {
		return NodePage{}, invalid("the sort priority needs exactly 1 value of childrenOf")
	}
	if in.SortPriority && in.Direction != "" {
		return NodePage{}, invalid("direction is only for the sort lastUpdated")
	}
	if (in.StartAfterKey == nil) != (in.StartAfterID == nil) {
		return NodePage{}, invalid("give startAfterKey and startAfterId together")
	}
	archived := in.Archived
	if archived == nil {
		archived = []bool{false}
	}
	type relation struct {
		values []uuid.UUID
		nodes  func(id uuid.UUID) []uuid.UUID
	}
	relations := []relation{
		{in.ChildrenOf, func(id uuid.UUID) []uuid.UUID { return tails(v.Children(id)) }},
		{in.DescendantsOf, func(id uuid.UUID) []uuid.UUID {
			return v.closure(id, func(x uuid.UUID) []uuid.UUID { return tails(v.Children(x)) })
		}},
		{in.ParentsOf, func(id uuid.UUID) []uuid.UUID { return heads(v.Parents(id)) }},
		{in.AncestorsOf, func(id uuid.UUID) []uuid.UUID {
			return v.closure(id, func(x uuid.UUID) []uuid.UUID { return heads(v.Parents(x)) })
		}},
		{in.DependentsOf, func(id uuid.UUID) []uuid.UUID { return tails(v.Dependents(id)) }},
		{in.PrerequisitesOf, func(id uuid.UUID) []uuid.UUID { return heads(v.Prereqs(id)) }},
	}
	var relationSets []map[uuid.UUID]bool
	for _, r := range relations {
		if r.values == nil {
			continue
		}
		set := map[uuid.UUID]bool{}
		for _, id := range r.values {
			for _, n := range r.nodes(id) {
				set[n] = true
			}
		}
		relationSets = append(relationSets, set)
	}

	descending := !in.SortPriority && in.Direction != "ascending"
	limit := in.Limit
	if limit == 0 {
		limit = 50
	}
	// The candidates are the Nodes in each relation set, or else each Node
	// of the Product. In a partial Graph, they are the Nodes that come first
	// after the start, and more is true if more may come after them.
	var candidates []uuid.UUID
	more := false
	var scan Scan
	if len(relationSets) > 0 {
		for id := range relationSets[0] {
			if !slices.ContainsFunc(relationSets[1:], func(set map[uuid.UUID]bool) bool { return !set[id] }) {
				candidates = append(candidates, id)
			}
		}
	} else {
		scan = Scan{Descending: descending, After: in.StartAfterKey != nil, Count: limit}
		if scan.After {
			scan.StartKey, scan.StartID = *in.StartAfterKey, *in.StartAfterID
		}
		candidates, more = v.G.scan(scan)
	}

	type keyed struct {
		id  uuid.UUID
		key int64
	}
	sorted := make([]keyed, 0, len(candidates))
	for _, id := range candidates {
		k := keyed{id: id, key: v.G.Nodes[id].Version}
		if in.SortPriority {
			for _, e := range v.parents[id] {
				if e.HeadID == in.ChildrenOf[0] {
					k.key = e.Priority
				}
			}
		}
		sorted = append(sorted, k)
	}
	order := func(a, b keyed) int {
		c := cmp.Compare(a.key, b.key)
		if descending {
			c = -c
		}
		if c != 0 {
			return c
		}
		return idCmp(a.id, b.id)
	}
	slices.SortFunc(sorted, order)
	if in.StartAfterKey != nil {
		start := keyed{id: *in.StartAfterID, key: *in.StartAfterKey}
		i, _ := slices.BinarySearchFunc(sorted, start, order)
		for i < len(sorted) && order(sorted[i], start) <= 0 {
			i++
		}
		sorted = sorted[i:]
	}
	// The filters come after the sort, so that a page reads only the Nodes
	// up to its last one.
	var selected []keyed
	for _, k := range sorted {
		if len(selected) == limit {
			break
		}
		n := v.G.Nodes[k.id]
		if (in.Types != nil && !slices.Contains(in.Types, n.Type)) ||
			(in.Statuses != nil && !slices.Contains(in.Statuses, n.Fields.Status)) ||
			(in.Assignees != nil && !slices.ContainsFunc(in.Assignees, func(s *string) bool { return optSame(s, n.Fields.Assignee) })) ||
			(in.Watchers != nil && !slices.ContainsFunc(in.Watchers, func(s *string) bool { return Watches(n.Watchers, s) })) ||
			!slices.Contains(archived, v.archived[k.id]) ||
			(in.Blocked != nil && !slices.Contains(in.Blocked, v.Blocked(k.id))) ||
			(in.Search != nil && !containsFold(n.Fields.Title, *in.Search) && !containsFold(deref(n.Fields.Description), *in.Search)) {
			continue
		}
		selected = append(selected, k)
	}
	if more && len(selected) < limit {
		v.G.scanMore(scan)
	}
	page := NodePage{Nodes: []NodeJSON{}}
	for _, k := range selected {
		page.Nodes = append(page.Nodes, v.NodeJSON(k.id))
	}
	if len(selected) > 0 {
		page.LastKey = &selected[len(selected)-1].key
	}
	return page, nil
}

// Watches reports whether the watchers include the user ID, or, if the user
// ID is nil, whether there are no watchers.
func Watches(watchers []string, userID *string) bool {
	if userID == nil {
		return len(watchers) == 0
	}
	return slices.Contains(watchers, *userID)
}

// closure returns each Node reachable from id through next, other than id.
func (v *View) closure(id uuid.UUID, next func(uuid.UUID) []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	var out []uuid.UUID
	queue := next(id)
	for len(queue) > 0 {
		x := queue[0]
		queue = queue[1:]
		if seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
		queue = append(queue, next(x)...)
	}
	return out
}

// NextWork returns the next available leaf of the Product (W2), or nil.
func NextWork(v *View) *NodeJSON {
	product := v.G.ProductNode()
	if product == nil {
		return nil
	}
	// The result of a visit doesn't depend on the path to the Node, so each
	// Node is visited at most one time. In a partial Graph, the walk stops at
	// the first Node whose relations the Graph doesn't hold, because the
	// rest of the walk depends on the result of that Node.
	memo := map[uuid.UUID]*uuid.UUID{}
	var visit func(id uuid.UUID) *uuid.UUID
	visit = func(id uuid.UUID) *uuid.UUID {
		if r, ok := memo[id]; ok || v.G.missed() {
			return r
		}
		var result *uuid.UUID
		switch {
		// Each Child of a `done` Node is `done` or carries the archived mark
		// (T3), so no available leaf is below it.
		case v.G.Nodes[id].Fields.Status == Done || v.archived[id] || v.Blocked(id):
		case v.Available(id):
			result = &id
		default:
			children := slices.Clone(v.Children(id))
			slices.SortFunc(children, func(a, b *Edge) int {
				if c := cmp.Compare(a.Priority, b.Priority); c != 0 {
					return c
				}
				return idCmp(a.TailID, b.TailID)
			})
			for _, e := range children {
				if result = visit(e.TailID); result != nil {
					break
				}
			}
		}
		memo[id] = result
		return result
	}
	id := visit(product.ID)
	if id == nil {
		return nil
	}
	n := v.NodeJSON(*id)
	return &n
}

// containsFold reports whether s contains substr, ignoring case: both are
// mapped to lowercase with the simple case mappings of Unicode (O1).
func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
