// Package changefeed holds the filter groups of a poll or a subscription
// (Q1 to Q6), and the signals that wake each open subscription (B9).
package changefeed

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/d4nyll/nodoplex/internal/graph"
	"github.com/google/uuid"
)

// StateFilter is a StateFilter of T: which Node the server checks, and the
// values for the Node before and after the change. If the input gives
// `values`, Before and After both hold them.
type StateFilter[T any] struct {
	Check  string // before, after, both, or either
	Before []T
	After  []T
}

// Group is one filter group. A nil member is a filter that the group doesn't
// give. In Watchers and Assignee, a nil pointer is no value.
type Group struct {
	Types    []graph.NodeType
	Watchers []*string
	Archived *StateFilter[bool]
	Status   *StateFilter[graph.Status]
	Assignee *StateFilter[*string]
}

// defaultArchived is the default `archived` filter (Q2).
var defaultArchived = StateFilter[bool]{Check: "either", Before: []bool{false}, After: []bool{false}}

// NodeState holds the values of a Node that the filters check.
type NodeState struct {
	Type     graph.NodeType
	Status   graph.Status
	Assignee *string
	Archived bool
}

// End is a Node at one end of an Edge, with the values that Q5 keeps. A nil
// pointer means that the Node didn't exist at that time.
type End struct {
	ID             uuid.UUID
	Type           graph.NodeType
	ArchivedBefore *bool
	ArchivedAfter  *bool
}

// Change is one change of a Change Entry, as the filters see it.
type Change struct {
	Edge bool
	// For a change to a Node: its ID, and its values before and after the
	// change, or nil if the Node didn't exist at that time.
	NodeID        uuid.UUID
	Before, After *NodeState
	// For a change to an Edge: its Tail Node and its Head Node.
	Tail, Head End
}

// Watchers gives the watchers that a Node has when the server checks the
// filter (Q4).
type Watchers func(nodeID uuid.UUID) []string

// UsesWatchers reports whether a filter group has a `watcher` filter, so the
// caller knows whether to read the watchers.
func UsesWatchers(groups []Group) bool {
	return slices.ContainsFunc(groups, func(g Group) bool { return g.Watchers != nil })
}

// NodeIDs returns the ID of each Node that the changes name.
func NodeIDs(changes []Change) []uuid.UUID {
	var ids []uuid.UUID
	for _, c := range changes {
		if c.Edge {
			ids = append(ids, c.Tail.ID, c.Head.ID)
		} else {
			ids = append(ids, c.NodeID)
		}
	}
	return ids
}

// Matches reports whether at least one change matches at least one of the
// filter groups (Q1). No filter groups is one group that holds only the
// default `archived` filter (Q2).
func Matches(groups []Group, changes []Change, watchers Watchers) bool {
	if len(groups) == 0 {
		groups = []Group{{}}
	}
	for _, c := range changes {
		for _, g := range groups {
			if g.matches(c, watchers) {
				return true
			}
		}
	}
	return false
}

func (g Group) matches(c Change, watchers Watchers) bool {
	if g.Types != nil {
		if c.Edge && !slices.Contains(g.Types, c.Tail.Type) && !slices.Contains(g.Types, c.Head.Type) {
			return false
		}
		if !c.Edge && !slices.Contains(g.Types, c.node().Type) {
			return false
		}
	}
	if g.Watchers != nil {
		ids := []uuid.UUID{c.NodeID}
		if c.Edge {
			ids = []uuid.UUID{c.Tail.ID, c.Head.ID}
		}
		if !slices.ContainsFunc(ids, func(id uuid.UUID) bool {
			w := watchers(id)
			return slices.ContainsFunc(g.Watchers, func(u *string) bool { return graph.Watches(w, u) })
		}) {
			return false
		}
	}
	if g.Status != nil && (c.Edge || !check(*g.Status, nodeValue(c.Before, func(n *NodeState) graph.Status { return n.Status }),
		nodeValue(c.After, func(n *NodeState) graph.Status { return n.Status }), eq)) {
		return false
	}
	if g.Assignee != nil && (c.Edge || !check(*g.Assignee, nodeValue(c.Before, func(n *NodeState) *string { return n.Assignee }),
		nodeValue(c.After, func(n *NodeState) *string { return n.Assignee }), optEq)) {
		return false
	}
	archived := defaultArchived
	if g.Archived != nil {
		archived = *g.Archived
	}
	if c.Edge {
		return check(archived, c.Tail.ArchivedBefore, c.Tail.ArchivedAfter, eq) ||
			check(archived, c.Head.ArchivedBefore, c.Head.ArchivedAfter, eq)
	}
	return check(archived, nodeValue(c.Before, func(n *NodeState) bool { return n.Archived }),
		nodeValue(c.After, func(n *NodeState) bool { return n.Archived }), eq)
}

func (c Change) node() *NodeState {
	if c.After != nil {
		return c.After
	}
	return c.Before
}

func nodeValue[T any](n *NodeState, get func(*NodeState) T) *T {
	if n == nil {
		return nil
	}
	v := get(n)
	return &v
}

// check applies Q6 to a Node: before and after are its values before and
// after the change, or nil if it didn't exist at that time.
func check[T any](f StateFilter[T], before, after *T, same func(a, b T) bool) bool {
	in := func(values []T, v *T) bool {
		return v != nil && slices.ContainsFunc(values, func(x T) bool { return same(x, *v) })
	}
	matchesBefore, matchesAfter := in(f.Before, before), in(f.After, after)
	switch f.Check {
	case "before":
		return matchesBefore
	case "after":
		return matchesAfter
	case "both":
		return matchesBefore && matchesAfter
	}
	return matchesBefore || matchesAfter
}

func eq[T comparable](a, b T) bool { return a == b }

func optEq(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// ── Reading a Change Entry ──────────────────────────────────────────────────

// ParseEntry reads the changes of a Change Entry from its JSON text and the
// values of Q5 that the server keeps with it.
func ParseEntry(text, edgeEnds []byte) ([]Change, error) {
	type item struct {
		ID       uuid.UUID      `json:"id"`
		Type     graph.NodeType `json:"type"`
		Archived bool           `json:"archived"`
		Fields   struct {
			Status   graph.Status `json:"status"`
			Assignee *string      `json:"assignee"`
		} `json:"fields"`
		TailID uuid.UUID `json:"tailId"`
		HeadID uuid.UUID `json:"headId"`
	}
	var entry struct {
		Changes []struct {
			Entity string `json:"entity"`
			Before *item  `json:"before"`
			After  *item  `json:"after"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(text, &entry); err != nil {
		return nil, fmt.Errorf("parse change entry: %w", err)
	}
	var ends []*graph.EdgeEnds
	if err := json.Unmarshal(edgeEnds, &ends); err != nil || len(ends) != len(entry.Changes) {
		return nil, fmt.Errorf("parse edge ends: %v", err)
	}
	state := func(it *item) *NodeState {
		if it == nil {
			return nil
		}
		return &NodeState{Type: it.Type, Status: it.Fields.Status, Assignee: it.Fields.Assignee, Archived: it.Archived}
	}
	changes := make([]Change, len(entry.Changes))
	for i, c := range entry.Changes {
		it := c.After
		if it == nil {
			it = c.Before
		}
		if c.Entity == "node" {
			changes[i] = Change{NodeID: it.ID, Before: state(c.Before), After: state(c.After)}
			continue
		}
		e, x := it, ends[i]
		changes[i] = Change{
			Edge: true,
			Tail: End{ID: e.TailID, Type: x.TailType, ArchivedBefore: x.TailArchivedBefore, ArchivedAfter: x.TailArchivedAfter},
			Head: End{ID: e.HeadID, Type: x.HeadType, ArchivedBefore: x.HeadArchivedBefore, ArchivedAfter: x.HeadArchivedAfter},
		}
	}
	return changes, nil
}
