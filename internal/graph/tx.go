package graph

import (
	"cmp"
	"slices"
	"time"

	"github.com/google/uuid"
)

// Feed holds the values of a Changefeed that a write reads and changes: the
// latest Change ID (C2), and the `time` of the Change Entry that has it (C7).
type Feed struct {
	LatestChangeID int64
	LatestTime     time.Time
}

// Tx is one write to the graph of a Product. The steps of the operation
// change G. Complete then finishes the write, and compares G with Before to
// find the changes.
type Tx struct {
	Before *Graph
	G      *Graph
	UserID string
	Feed   Feed
	Now    time.Time // the time of the server's clock
	// NewID gives the id of each Node and each Edge that the write creates.
	NewID func() uuid.UUID

	// ProductDeleted is true if the write deleted the Product Node (P7).
	ProductDeleted bool
	// Entry is the Change Entry that Complete recorded, or nil.
	Entry *Entry
	// View is the View of G that Complete computed.
	View *View
}

func NewTx(before *Graph, feed Feed, userID string, now time.Time) *Tx {
	return &Tx{Before: before, G: before.Clone(), Feed: feed, UserID: userID, Now: now, NewID: uuid.New}
}

// Entry is a Change Entry, with its JSON text (J7, B10) and the values that
// Q5 gives (B12).
type Entry struct {
	ChangeID int64
	Time     time.Time
	JSON     []byte
	// EdgeEnds holds, for each change in the order of `changes`, the values of
	// Q5 for a change to an Edge, or null for a change to a Node.
	EdgeEnds []byte
}

// EdgeEnds holds the values of Q5 for a change to an Edge. A nil pointer
// means that the Node didn't exist at that time.
type EdgeEnds struct {
	TailType           NodeType `json:"tailType"`
	HeadType           NodeType `json:"headType"`
	TailArchivedBefore *bool    `json:"tailArchivedBefore"`
	TailArchivedAfter  *bool    `json:"tailArchivedAfter"`
	HeadArchivedBefore *bool    `json:"headArchivedBefore"`
	HeadArchivedAfter  *bool    `json:"headArchivedAfter"`
}

type changeJSON struct {
	Kind   string `json:"kind"`
	Entity string `json:"entity"`
	Before any    `json:"before,omitempty"`
	After  any    `json:"after,omitempty"`
}

type entryJSON struct {
	ChangeID int64        `json:"changeId"`
	Time     string       `json:"time"`
	UserID   string       `json:"userId"`
	Changes  []changeJSON `json:"changes"`
}

// TimeFormat is the JSON form of a timestamp (J4).
const TimeFormat = "2006-01-02T15:04:05.000000Z"

// Complete runs the steps of "complete a write" of the base specification on
// G. It returns a refusal, or sets tx.Entry to the Change Entry that the write
// records, or leaves it nil if the write records none (C4).
func (tx *Tx) Complete() error {
	if tx.G.missed() {
		return errMissed
	}
	after := NewView(tx.G)
	after.deriveStatuses()
	tx.View = after
	before := NewView(tx.Before)
	if needAffected(tx, before, after); tx.G.missed() {
		return errMissed
	}
	for _, id := range sortedNodeIDs(tx.G.Nodes) {
		// In a partial Graph, a Node whose Children or Prerequisites the
		// Graph doesn't hold is neither a leaf that the write makes `done`
		// nor one that it makes blocked (see needAffected).
		if tx.G.has(id, RelChildren|RelPrereqs) && after.Leaf(id) && tx.G.Nodes[id].Fields.Status == Done && after.Blocked(id) {
			return Refuse(CodeDoneLeafBlocked, "after the write, the done leaf %s would be blocked", id)
		}
	}

	var nodeChanges, edgeChanges []uuid.UUID
	for _, id := range unionIDs(tx.Before.Nodes, tx.G.Nodes) {
		if b, a := tx.Before.Nodes[id], tx.G.Nodes[id]; b == nil || a == nil || nodeStateChanged(before, after, id) {
			nodeChanges = append(nodeChanges, id)
		}
	}
	for _, id := range unionIDs(tx.Before.Edges, tx.G.Edges) {
		if b, a := tx.Before.Edges[id], tx.G.Edges[id]; b == nil || a == nil || b.Priority != a.Priority {
			edgeChanges = append(edgeChanges, id)
		}
	}
	if len(nodeChanges) == 0 && len(edgeChanges) == 0 {
		return nil
	}

	changeID := tx.Feed.LatestChangeID + 1
	t := tx.Now.UTC().Truncate(time.Microsecond)
	if t.Before(tx.Feed.LatestTime) {
		t = tx.Feed.LatestTime
	}
	entry := entryJSON{ChangeID: changeID, Time: t.Format(TimeFormat), UserID: tx.UserID}
	var ends []*EdgeEnds
	for _, id := range nodeChanges {
		c := changeJSON{Entity: "node"}
		if b := tx.Before.Nodes[id]; b != nil {
			c.Before = before.NodeJSON(id)
		}
		if a := tx.G.Nodes[id]; a != nil {
			a.Version = changeID
			c.After = after.NodeJSON(id)
		}
		c.Kind = changeKind(c.Before != nil, c.After != nil)
		entry.Changes = append(entry.Changes, c)
		ends = append(ends, nil)
	}
	for _, id := range edgeChanges {
		c := changeJSON{Entity: "edge"}
		e := tx.Before.Edges[id]
		if e != nil {
			c.Before = *e
		}
		if a := tx.G.Edges[id]; a != nil {
			c.After = *a
			e = a
		}
		c.Kind = changeKind(c.Before != nil, c.After != nil)
		entry.Changes = append(entry.Changes, c)
		ends = append(ends, &EdgeEnds{
			TailType:           nodeType(tx, e.TailID),
			HeadType:           nodeType(tx, e.HeadID),
			TailArchivedBefore: archivedAt(before, e.TailID),
			TailArchivedAfter:  archivedAt(after, e.TailID),
			HeadArchivedBefore: archivedAt(before, e.HeadID),
			HeadArchivedAfter:  archivedAt(after, e.HeadID),
		})
	}

	text, err := Marshal(entry)
	if err != nil {
		return err
	}
	endsText, err := Marshal(ends)
	if err != nil {
		return err
	}
	tx.Feed = Feed{LatestChangeID: changeID, LatestTime: t}
	tx.Entry = &Entry{ChangeID: changeID, Time: t, JSON: text, EdgeEnds: endsText}
	return nil
}

// needAffected records as missed each relation that the rules read to find
// the Nodes whose derived values the write changes, if the partial Graph
// doesn't hold it. In a whole Graph, it does nothing. A Node that the
// write doesn't name changes only through a Node or an Edge next to it:
//
//   - the derived status of a Parent reads the status and the archived mark
//     of each of its Children (T3)
//   - whether a Dependent is blocked reads the status and `archived` of each
//     of its Prerequisites (T2)
//   - `archived` of a Child reads `archived` of each of its Parents (A2)
//   - done_leaf_blocked reads whether a Node whose status changes is a
//     blocked leaf
//
// So for each change, the Graph must hold the Children and the Prerequisites
// of each Node that the change can reach, and needAffected asks for them. A
// later run then finds the changes to those Nodes, until a run finds no
// change whose Nodes the Graph doesn't hold.
func needAffected(tx *Tx, before, after *View) {
	g := tx.G
	derive := RelChildren | RelPrereqs
	for _, id := range unionIDs(tx.Before.Nodes, g.Nodes) {
		b, a := tx.Before.Nodes[id], g.Nodes[id]
		if b == nil || a == nil {
			// The Edges of a Node that the write creates or deletes are
			// changes to Edges, below.
			continue
		}
		status := b.Fields.Status != a.Fields.Status
		archived := before.archived[id] != after.archived[id]
		if status {
			g.need(id, derive)
		}
		if status || b.ArchivedMark != a.ArchivedMark {
			for _, e := range after.parents[id] {
				g.need(e.HeadID, derive)
			}
		}
		if status || archived {
			for _, e := range after.Dependents(id) {
				g.need(e.TailID, derive)
			}
		}
		if archived {
			after.Children(id)
		}
	}
	for _, id := range unionIDs(tx.Before.Edges, g.Edges) {
		b, a := tx.Before.Edges[id], g.Edges[id]
		if b != nil && a != nil {
			continue // a priority changes no derived value
		}
		e := cmp.Or(a, b)
		// A new or a deleted Edge changes the Children of its Head Node, or
		// the Prerequisites of its Tail Node.
		end := e.HeadID
		if e.Kind == Dependency {
			end = e.TailID
		}
		if g.Nodes[end] != nil {
			g.need(end, derive)
		}
	}
}

func changeKind(hasBefore, hasAfter bool) string {
	switch {
	case !hasBefore:
		return "creation"
	case !hasAfter:
		return "deletion"
	}
	return "update"
}

// nodeStateChanged reports whether the state of a Node (C3) differs between 2
// Views: its tracked Fields, its archived mark, whether it is archived, or
// the Edges that point from it, with their priorities. Each Field is tracked.
func nodeStateChanged(before, after *View, id uuid.UUID) bool {
	b, a := before.G.Nodes[id], after.G.Nodes[id]
	if !fieldsSame(b.Fields, a.Fields) || b.ArchivedMark != a.ArchivedMark || before.archived[id] != after.archived[id] {
		return true
	}
	out := func(v *View) map[uuid.UUID]int64 {
		m := map[uuid.UUID]int64{}
		for _, e := range append(slices.Clone(v.parents[id]), v.prereqs[id]...) {
			m[e.ID] = e.Priority
		}
		return m
	}
	ob, oa := out(before), out(after)
	if len(ob) != len(oa) {
		return true
	}
	for eid, p := range ob {
		if q, ok := oa[eid]; !ok || p != q {
			return true
		}
	}
	return false
}

func nodeType(tx *Tx, id uuid.UUID) NodeType {
	if n := tx.G.Nodes[id]; n != nil {
		return n.Type
	}
	return tx.Before.Nodes[id].Type
}

func archivedAt(v *View, id uuid.UUID) *bool {
	if _, ok := v.G.Nodes[id]; !ok {
		return nil
	}
	a := v.archived[id]
	return &a
}

func unionIDs[T any](a, b map[uuid.UUID]T) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	var ids []uuid.UUID
	for _, m := range []map[uuid.UUID]T{a, b} {
		for id := range m {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	slices.SortFunc(ids, idCmp)
	return ids
}
