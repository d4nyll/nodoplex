package graph

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

// world holds the graph of one Product, and runs writes on it as the store
// does: the steps, then Complete, then the new state.
type world struct {
	t       *testing.T
	g       *Graph
	feed    Feed
	now     time.Time
	entries []decodedEntry
	product uuid.UUID
}

type decodedEntry struct {
	ChangeID int64  `json:"changeId"`
	Time     string `json:"time"`
	UserID   string `json:"userId"`
	Changes  []struct {
		Kind   string          `json:"kind"`
		Entity string          `json:"entity"`
		Before json.RawMessage `json:"before"`
		After  json.RawMessage `json:"after"`
	} `json:"changes"`
}

func newWorld(t *testing.T) *world {
	w := &world{t: t, g: NewGraph("com.example.app"), now: time.Date(2026, 11, 23, 8, 0, 0, 0, time.UTC)}
	w.mustWrite(func(tx *Tx) (err error) {
		w.product, err = CreateProduct(tx, FieldsInput{"title": "App"}, false)
		return err
	})
	return w
}

func (w *world) write(steps func(tx *Tx) error) (*Tx, error) {
	w.t.Helper()
	tx := NewTx(w.g, w.feed, "alice", w.now)
	if err := steps(tx); err != nil {
		return nil, err
	}
	if err := tx.Complete(); err != nil {
		return nil, err
	}
	w.g, w.feed = tx.G, tx.Feed
	if tx.Entry != nil {
		var e decodedEntry
		if err := json.Unmarshal(tx.Entry.JSON, &e); err != nil {
			w.t.Fatal(err)
		}
		w.entries = append(w.entries, e)
	}
	return tx, nil
}

func (w *world) mustWrite(steps func(tx *Tx) error) *Tx {
	w.t.Helper()
	tx, err := w.write(steps)
	if err != nil {
		w.t.Fatalf("write: %v", err)
	}
	return tx
}

func (w *world) node(t NodeType, title string, parents ...uuid.UUID) uuid.UUID {
	w.t.Helper()
	var id uuid.UUID
	refs := make([]ParentRef, len(parents))
	for i, p := range parents {
		refs[i] = ParentRef{ID: p}
	}
	w.mustWrite(func(tx *Tx) (err error) {
		id, err = CreateNode(tx, t, refs, FieldsInput{"title": title})
		return err
	})
	return id
}

func (w *world) version(id uuid.UUID) int64 { return w.g.Nodes[id].Version }

func (w *world) edge(kind EdgeKind, tail, head uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	_, err := w.write(func(tx *Tx) (err error) {
		id, err = CreateEdge(tx, CreateEdgeInput{Kind: kind, TailID: tail, HeadID: head, Version: w.version(tail)})
		return err
	})
	return id, err
}

func (w *world) mustEdge(kind EdgeKind, tail, head uuid.UUID) uuid.UUID {
	w.t.Helper()
	id, err := w.edge(kind, tail, head)
	if err != nil {
		w.t.Fatalf("create edge: %v", err)
	}
	return id
}

func (w *world) setStatus(id uuid.UUID, s Status) error {
	version := w.version(id)
	_, err := w.write(func(tx *Tx) error { return UpdateNode(tx, id, &version, FieldsInput{"status": string(s)}) })
	return err
}

func (w *world) mustStatus(id uuid.UUID, s Status) {
	w.t.Helper()
	if err := w.setStatus(id, s); err != nil {
		w.t.Fatalf("set status %s: %v", s, err)
	}
}

func (w *world) setMark(id uuid.UUID, mark bool) error {
	_, err := w.write(func(tx *Tx) error { return SetArchivedMark(tx, id, w.version(id), mark) })
	return err
}

func (w *world) status(id uuid.UUID) Status { return w.g.Nodes[id].Fields.Status }
func (w *world) archived(id uuid.UUID) bool { return NewView(w.g).Archived(id) }

// lastChanges returns the kind of each change to a Node in the last Change
// Entry, by Node ID.
func (w *world) lastNodeChanges() map[uuid.UUID]string {
	out := map[uuid.UUID]string{}
	e := w.entries[len(w.entries)-1]
	for _, c := range e.Changes {
		if c.Entity != "node" {
			continue
		}
		var n struct{ ID uuid.UUID }
		raw := c.After
		if raw == nil {
			raw = c.Before
		}
		json.Unmarshal(raw, &n)
		out[n.ID] = c.Kind
	}
	return out
}

func idLess(a, b uuid.UUID) bool { return idCmp(a, b) < 0 }

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var refusal *Error
	if !errors.As(err, &refusal) || refusal.Code != code {
		t.Fatalf("want refusal %s, got %v", code, err)
	}
}

// ── Archiving ───────────────────────────────────────────────────────────────

// The example of the mental model: Product P, Vision V, Goals G1 and G2 under
// V, Feature F under G1, Feature E under G1 and G2.
func TestArchivedFromMarksAndPaths(t *testing.T) {
	w := newWorld(t)
	v := w.node(TypeVision, "V", w.product)
	g1 := w.node(TypeGoal, "G1", v)
	g2 := w.node(TypeGoal, "G2", v)
	f := w.node(TypeFeature, "F", g1)
	e := w.node(TypeFeature, "E", g1, g2)
	r := w.node(TypeRequirement, "R under F", f)

	if err := w.setMark(g1, true); err != nil {
		t.Fatal(err)
	}
	if !w.archived(g1) || !w.archived(f) || !w.archived(r) || w.archived(e) || w.archived(g2) {
		t.Fatalf("after archiving G1: G1 %v, F %v, R %v, E %v, G2 %v", w.archived(g1), w.archived(f), w.archived(r), w.archived(e), w.archived(g2))
	}
	changed := w.lastNodeChanges()
	for _, id := range []uuid.UUID{g1, f, r} {
		if changed[id] != "update" {
			t.Errorf("the Change Entry must hold an update of %s, got %v", w.g.Nodes[id].Fields.Title, changed)
		}
	}
	if _, ok := changed[e]; ok {
		t.Error("the Change Entry must hold no update of E, because E doesn't become archived")
	}
	if !w.g.Nodes[g1].ArchivedMark || w.g.Nodes[f].ArchivedMark {
		t.Error("only G1 carries the archived mark")
	}

	// Deleting the Hierarchical Edge from E to G2 leaves E only under G1.
	var eToG2 uuid.UUID
	for id, edge := range w.g.Edges {
		if edge.TailID == e && edge.HeadID == g2 {
			eToG2 = id
		}
	}
	w.mustWrite(func(tx *Tx) error { return DeleteEdge(tx, eToG2, w.version(e)) })
	if !w.archived(e) || w.lastNodeChanges()[e] != "update" {
		t.Error("E becomes archived when its only path to the Product passes through G1")
	}

	// A new Hierarchical Edge from F to G2 makes F, and R under it, stop being
	// archived.
	w.mustEdge(Hierarchical, f, g2)
	if w.archived(f) || w.archived(r) {
		t.Error("F and R stop being archived when F gets a Parent that isn't archived")
	}
	if w.lastNodeChanges()[r] != "update" {
		t.Error("the Change Entry must hold an update of R, which stops being archived")
	}

	// Unarchiving G1 makes E stop being archived.
	if err := w.setMark(g1, false); err != nil {
		t.Fatal(err)
	}
	if w.archived(e) || w.lastNodeChanges()[e] != "update" {
		t.Error("E stops being archived when G1 loses its archived mark")
	}

	// The archived mark on the Product archives every Node.
	if err := w.setMark(w.product, true); err != nil {
		t.Fatal(err)
	}
	for id := range w.g.Nodes {
		if !w.archived(id) {
			t.Errorf("%s isn't archived after the Product is archived", w.g.Nodes[id].Fields.Title)
		}
	}
	// Archiving a Node that carries the mark records no Change Entry.
	n := len(w.entries)
	if err := w.setMark(w.product, true); err != nil || len(w.entries) != n {
		t.Errorf("archiving again: err %v, entries %d -> %d", err, n, len(w.entries))
	}
}

// ── Status ──────────────────────────────────────────────────────────────────

func TestDerivedStatusRules(t *testing.T) {
	cases := []struct {
		children []Status
		blocked  bool
		want     Status
	}{
		{nil, false, Done},
		{nil, true, InProgress},
		{[]Status{Done, Done}, false, Done},
		{[]Status{Done, Done}, true, InProgress},
		{[]Status{NotStarted, NotStarted}, false, NotStarted},
		{[]Status{NotStarted, NotStarted}, true, NotStarted},
		{[]Status{NotStarted, Done}, false, InProgress},
		{[]Status{InProgress}, false, InProgress},
	}
	for _, c := range cases {
		if got := derivedStatus(c.children, c.blocked); got != c.want {
			t.Errorf("derivedStatus(%v, blocked %v) = %s, want %s", c.children, c.blocked, got, c.want)
		}
	}
}

func TestDerivedStatusPropagates(t *testing.T) {
	w := newWorld(t)
	v := w.node(TypeVision, "V", w.product)
	g := w.node(TypeGoal, "G", v)
	f := w.node(TypeFeature, "F", g)
	r := w.node(TypeRequirement, "R", f)
	s := w.node(TypeRequirement, "S", f)
	t1 := w.node(TypeTask, "T1", r)
	t2 := w.node(TypeTask, "T2", r)
	ts := w.node(TypeTask, "TS", s)

	if w.status(r) != NotStarted || w.status(w.product) != NotStarted {
		t.Fatalf("a Node whose Children are not_started is not_started, got R %s, P %s", w.status(r), w.status(w.product))
	}
	w.mustStatus(t1, InProgress)
	if w.status(r) != InProgress || w.status(f) != InProgress || w.status(w.product) != InProgress {
		t.Fatalf("in_progress must reach each ancestor, got R %s, F %s, P %s", w.status(r), w.status(f), w.status(w.product))
	}
	if w.lastNodeChanges()[w.product] != "update" {
		t.Error("the Change Entry must hold an update of each Node whose derived status changes")
	}
	if w.g.Nodes[w.product].Version != w.feed.LatestChangeID {
		t.Error("a Node whose derived status changes gets a new version")
	}

	// A Child that carries the archived mark doesn't count.
	w.mustStatus(t1, Done)
	if err := w.setMark(t2, true); err != nil {
		t.Fatal(err)
	}
	if w.status(r) != Done {
		t.Fatalf("R must be done when its only counted Child is done, got %s", w.status(r))
	}

	// R depends on S. S isn't done, so R is blocked and isn't done.
	w.mustEdge(Dependency, r, s)
	if w.status(r) != InProgress {
		t.Fatalf("a blocked R with done Children is in_progress, got %s", w.status(r))
	}
	w.mustStatus(ts, Done)
	if w.status(s) != Done || w.status(r) != Done {
		t.Fatalf("R is done once S is done, got S %s, R %s", w.status(s), w.status(r))
	}
	// The server accepts a write that blocks a derived done Node, and derives
	// its status again in the same write.
	w.mustStatus(ts, InProgress)
	if w.status(s) != InProgress || w.status(r) != InProgress {
		t.Fatalf("R must stop being done with S, got S %s, R %s", w.status(s), w.status(r))
	}
	changed := w.lastNodeChanges()
	if changed[s] != "update" || changed[r] != "update" || changed[ts] != "update" {
		t.Errorf("the Change Entry must hold TS, S, and R, got %v", changed)
	}

	// A Child that is archived only through its ancestors still counts.
	w.mustStatus(ts, Done)
	if err := w.setMark(s, true); err != nil {
		t.Fatal(err)
	}
	if !w.archived(ts) || w.status(s) != Done {
		t.Fatal("TS is archived through S, and S is still derived from TS")
	}
	w.mustStatus(ts, InProgress)
	if w.status(s) != InProgress {
		t.Errorf("inside an archived subtree, S is still derived from its Children, got %s", w.status(s))
	}
	// An archived Prerequisite doesn't block.
	if w.status(r) != Done {
		t.Errorf("R isn't blocked by the archived S, so R is done, got %s", w.status(r))
	}
}

func TestLeafStatus(t *testing.T) {
	w := newWorld(t)
	v := w.node(TypeVision, "V", w.product)
	g := w.node(TypeGoal, "G", v)
	f := w.node(TypeFeature, "F", g)
	r := w.node(TypeRequirement, "R", f)
	task := w.node(TypeTask, "T", r)

	w.mustStatus(task, Done)
	// The first Child replaces the status that a client set.
	sub := w.node(TypeSubtask, "Z", task)
	if w.status(task) != NotStarted {
		t.Fatalf("a Task with a not_started SubTask is not_started, got %s", w.status(task))
	}
	wantCode(t, w.setStatus(task, Done), CodeStatusIsDerived)

	w.mustStatus(sub, InProgress)
	// When the Node becomes a leaf again, it keeps its last derived status.
	w.mustWrite(func(tx *Tx) error { return DeleteNode(tx, sub, w.version(sub)) })
	if w.status(task) != InProgress {
		t.Fatalf("the Task keeps its last derived status, got %s", w.status(task))
	}
	w.mustStatus(task, NotStarted)
}

func TestDoneLeafBlocked(t *testing.T) {
	w := newWorld(t)
	v := w.node(TypeVision, "V", w.product)
	g := w.node(TypeGoal, "G", v)
	f := w.node(TypeFeature, "F", g)
	r := w.node(TypeRequirement, "R", f)
	a := w.node(TypeTask, "A", r)
	b := w.node(TypeTask, "B", r)

	// B doesn't depend on A, B is done, A isn't: no Dependency Edge from B to A.
	w.mustStatus(b, Done)
	_, err := w.edge(Dependency, b, a)
	wantCode(t, err, CodeDoneLeafBlocked)
	w.mustStatus(b, NotStarted)

	w.mustEdge(Dependency, b, a)
	w.mustStatus(b, InProgress) // a blocked leaf can be in_progress
	wantCode(t, w.setStatus(b, Done), CodeDoneLeafBlocked)
	w.mustStatus(a, Done)
	w.mustStatus(b, Done)
	wantCode(t, w.setStatus(a, InProgress), CodeDoneLeafBlocked)

	// Several Edges away: a new SubTask of A makes A not_started.
	_, err = w.write(func(tx *Tx) error {
		_, err := CreateNode(tx, TypeSubtask, []ParentRef{{ID: a}}, FieldsInput{"title": "Z"})
		return err
	})
	wantCode(t, err, CodeDoneLeafBlocked)

	// A done SubTask keeps A done. Then a change to the SubTask would make A
	// stop being done, so the server refuses it.
	w.mustStatus(b, NotStarted)
	z := w.node(TypeSubtask, "Z", a)
	w.mustStatus(z, Done)
	w.mustStatus(b, Done)
	wantCode(t, w.setStatus(z, InProgress), CodeDoneLeafBlocked)

	// A Prerequisite that isn't done stops being archived.
	w.mustStatus(b, NotStarted)
	w.mustStatus(z, InProgress)
	if err := w.setMark(a, true); err != nil {
		t.Fatal(err)
	}
	w.mustStatus(b, Done) // the archived A doesn't block B
	wantCode(t, w.setMark(a, false), CodeDoneLeafBlocked)

	// Also through the archived mark of an ancestor of the Prerequisite, and
	// also for an archived leaf.
	if err := w.setMark(a, false); err == nil {
		t.Fatal("unarchive must still be refused")
	}
	w.mustStatus(b, NotStarted)
	if err := w.setMark(a, false); err != nil {
		t.Fatal(err)
	}
	w.mustStatus(z, Done)
	r2 := w.node(TypeRequirement, "R2", f)
	c := w.node(TypeTask, "C", r2)
	if err := w.setMark(r2, true); err != nil {
		t.Fatal(err)
	}
	w.mustEdge(Dependency, b, c)
	if err := w.setMark(b, true); err != nil {
		t.Fatal(err)
	}
	w.mustStatus(b, Done) // C is archived through R2
	wantCode(t, w.setMark(r2, false), CodeDoneLeafBlocked)
}

// ── Cycles ──────────────────────────────────────────────────────────────────

func TestCycles(t *testing.T) {
	w := newWorld(t)
	v := w.node(TypeVision, "V", w.product)
	g := w.node(TypeGoal, "G", v)
	f := w.node(TypeFeature, "F", g)
	f2 := w.node(TypeFeature, "F2", g)
	q := w.node(TypeRequirement, "Q", f)
	r := w.node(TypeRequirement, "R", f)
	b := w.node(TypeTask, "B", q)
	a := w.node(TypeTask, "A", q)

	// Y1: R depends on B, B depends on A, and a new Hierarchical Edge from A
	// to R would close the cycle.
	w.mustEdge(Dependency, r, b)
	w.mustEdge(Dependency, b, a)
	_, err := w.edge(Hierarchical, a, r)
	wantCode(t, err, CodeCycle)
	// A Node can't depend on its own descendant, or on itself.
	za := w.node(TypeSubtask, "ZA", a)
	_, err = w.edge(Dependency, q, za)
	wantCode(t, err, CodeCycle)
	_, err = w.edge(Dependency, a, a)
	wantCode(t, err, CodeCycle)

	// Y2: R2 is the Parent of T, T depends on X, Z is a Child of X, and a new
	// Dependency Edge from Z to R2 would close the cycle.
	r2 := w.node(TypeRequirement, "R2", f2)
	tt := w.node(TypeTask, "T", r2)
	x := w.node(TypeTask, "X", q)
	z := w.node(TypeSubtask, "Z", x)
	w.mustEdge(Dependency, tt, x)
	_, err = w.edge(Dependency, z, r2)
	wantCode(t, err, CodeCycle)
	// A Node can't depend on its own ancestor.
	_, err = w.edge(Dependency, z, x)
	wantCode(t, err, CodeAlreadyConnected)
	_, err = w.edge(Dependency, tt, r2)
	wantCode(t, err, CodeAlreadyConnected)
	_, err = w.edge(Dependency, tt, f2)
	wantCode(t, err, CodeCycle)

	// Y2 with a new Hierarchical Edge: T2 has R3 as its Parent and depends on
	// F3. A new Hierarchical Edge from R3 to F3 would close the cycle.
	f3 := w.node(TypeFeature, "F3", g)
	r3 := w.node(TypeRequirement, "R3", f)
	t2 := w.node(TypeTask, "T2", r3)
	w.mustEdge(Dependency, t2, f3)
	_, err = w.edge(Hierarchical, r3, f3)
	wantCode(t, err, CodeCycle)

	// Archived Nodes count (Y3).
	if err := w.setMark(x, true); err != nil {
		t.Fatal(err)
	}
	_, err = w.edge(Dependency, z, r2)
	wantCode(t, err, CodeCycle)

	// An Edge that forms no cycle is accepted.
	w.mustEdge(Hierarchical, r3, f2)
}

// ── Change Entries and versions ─────────────────────────────────────────────

func TestChangeEntries(t *testing.T) {
	w := newWorld(t)
	if w.entries[0].ChangeID != 1 || w.entries[0].Changes[0].Kind != "creation" {
		t.Fatalf("the first Change Entry holds the creation of the Product, got %+v", w.entries[0])
	}
	v := w.node(TypeVision, "V", w.product)
	g := w.node(TypeGoal, "G", v)
	f := w.node(TypeFeature, "F", g)
	r1 := w.node(TypeRequirement, "R1", f)
	r2 := w.node(TypeRequirement, "R2", f)

	before := len(w.entries)
	task := w.node(TypeTask, "T", r1, r2)
	if len(w.entries) != before+1 {
		t.Fatal("one write records exactly one Change Entry")
	}
	e := w.entries[len(w.entries)-1]
	kinds := map[string]int{}
	for _, c := range e.Changes {
		kinds[c.Entity+" "+c.Kind]++
	}
	if kinds["node creation"] != 1 || kinds["edge creation"] != 2 {
		t.Errorf("creating a Task with 2 Parents holds its creation and 2 Edge creations, got %v", kinds)
	}
	if w.version(task) != e.ChangeID {
		t.Error("the version of a new Node is the Change ID of its Change Entry")
	}

	// An update of 2 tracked Fields holds one change for the Node.
	version := w.version(task)
	w.mustWrite(func(tx *Tx) error {
		return UpdateNode(tx, task, &version, FieldsInput{"title": "T2", "description": "d"})
	})
	if c := w.entries[len(w.entries)-1].Changes; len(c) != 1 || c[0].Kind != "update" {
		t.Errorf("an update of 2 Fields holds 1 change, got %d", len(c))
	}
	// An update that changes no value records no Change Entry.
	version = w.version(task)
	n := len(w.entries)
	w.mustWrite(func(tx *Tx) error { return UpdateNode(tx, task, &version, FieldsInput{"title": "T2"}) })
	if len(w.entries) != n {
		t.Error("an update that changes no value records no Change Entry")
	}
	// Watching records no Change Entry, and doesn't change the version.
	w.mustWrite(func(tx *Tx) error { return SetWatcher(tx, task, "bob", true) })
	if len(w.entries) != n || w.version(task) != version || !slices.Equal(w.g.Nodes[task].Watchers, []string{"bob"}) {
		t.Error("WatchNode records no Change Entry and keeps the version")
	}

	// Updating the priority of an Edge holds the Edge and its Tail Node.
	var edge uuid.UUID
	for id, ed := range w.g.Edges {
		if ed.TailID == task && ed.HeadID == r1 {
			edge = id
		}
	}
	w.mustWrite(func(tx *Tx) error { return UpdateEdge(tx, edge, 99, w.version(task)) })
	changed := w.lastNodeChanges()
	if changed[task] != "update" || len(w.entries[len(w.entries)-1].Changes) != 2 {
		t.Errorf("an Edge update holds the Edge and an update of its Tail Node, got %v", w.entries[len(w.entries)-1].Changes)
	}

	// Deleting a Node holds the deletion of each Edge from it.
	w.mustWrite(func(tx *Tx) error { return DeleteNode(tx, task, w.version(task)) })
	kinds = map[string]int{}
	for _, c := range w.entries[len(w.entries)-1].Changes {
		kinds[c.Entity+" "+c.Kind]++
	}
	if kinds["node deletion"] != 1 || kinds["edge deletion"] != 2 {
		t.Errorf("deleting a Node with 2 Parents holds 1 Node deletion and 2 Edge deletions, got %v", kinds)
	}
	// R1 and R2 become leaves and keep their derived status, so they don't
	// change.
	if _, ok := w.lastNodeChanges()[r1]; ok {
		t.Error("R1 keeps its status, so the Change Entry holds no update of it")
	}
}

func TestVersions(t *testing.T) {
	w := newWorld(t)
	v := w.node(TypeVision, "V", w.product)
	stale := w.version(v)
	vv := stale
	w.mustWrite(func(tx *Tx) error { return UpdateNode(tx, v, &vv, FieldsInput{"title": "V2"}) })

	_, err := w.write(func(tx *Tx) error { return UpdateNode(tx, v, &stale, FieldsInput{"title": "V3"}) })
	wantCode(t, err, CodeVersionConflict)
	_, err = w.write(func(tx *Tx) error { return UpdateNode(tx, v, nil, FieldsInput{"title": "V3"}) })
	wantCode(t, err, CodeInvalidRequest)
	wantCode(t, func() error {
		_, err := w.write(func(tx *Tx) error { return SetArchivedMark(tx, v, stale, true) })
		return err
	}(), CodeVersionConflict)
	wantCode(t, func() error { _, err := w.write(func(tx *Tx) error { return DeleteNode(tx, v, stale) }); return err }(), CodeVersionConflict)

	// Edge writes check the version of the Tail Node.
	g := w.node(TypeGoal, "G", v)
	g2 := w.node(TypeGoal, "G2", v)
	_, err = w.write(func(tx *Tx) error {
		_, err := CreateEdge(tx, CreateEdgeInput{Kind: Dependency, TailID: g, HeadID: g2, Version: w.version(g) - 1})
		return err
	})
	wantCode(t, err, CodeVersionConflict)
	e := w.mustEdge(Dependency, g, g2)
	wantCode(t, func() error {
		_, err := w.write(func(tx *Tx) error { return DeleteEdge(tx, e, w.version(g)-1) })
		return err
	}(), CodeVersionConflict)
	// A version higher than the current one is refused too.
	wantCode(t, func() error {
		_, err := w.write(func(tx *Tx) error { return DeleteEdge(tx, e, w.version(g)+5) })
		return err
	}(), CodeVersionConflict)
	w.mustWrite(func(tx *Tx) error { return DeleteEdge(tx, e, w.version(g)) })
}

func TestChangeIDsAndTime(t *testing.T) {
	w := newWorld(t)
	first := w.entries[0]
	w.now = w.now.Add(-time.Hour) // the clock is set back
	w.node(TypeVision, "V", w.product)
	second := w.entries[1]
	if second.ChangeID != 2 || second.Time != first.Time {
		t.Errorf("the time never decreases: %s then %s, ids %d %d", first.Time, second.Time, first.ChangeID, second.ChangeID)
	}
	w.now = w.now.Add(2*time.Hour + 1500*time.Nanosecond)
	w.node(TypeVision, "V2", w.product)
	if got := w.entries[2].Time; got != "2026-11-23T09:00:00.000001Z" {
		t.Errorf("the time is truncated to microseconds, got %s", got)
	}
}

// ── Fields ──────────────────────────────────────────────────────────────────

func TestFieldsChecks(t *testing.T) {
	cases := []FieldsInput{
		{},                                // no title
		{"title": ""},                     // empty title
		{"title": "x", "product": "a"},    // the server sets product
		{"title": "x", "slug": "a"},       // a task has no slug
		{"title": "x", "status": "later"}, // not a Status
		{"title": "x", "status": nil},     // a required Field with no value
		{"title": "x", "outputs": []any{map[string]any{"title": "no type"}}},
		{"title": "x", "outputs": []any{map[string]any{"outputType": "git", "extra": true}}},
		{"title": "x", "nickname": "y"},
	}
	for _, c := range cases {
		wantCode(t, checkFields(TypeTask, c, true), CodeInvalidRequest)
	}
	if err := checkFields(TypeTask, FieldsInput{"title": "x", "description": nil, "assignee": nil}, true); err != nil {
		t.Errorf("optional Fields can have no value: %v", err)
	}
}

func TestPayloadSameness(t *testing.T) {
	payload := func(n string) []any {
		return []any{map[string]any{"outputType": "git", "payload": map[string]any{"n": json.Number(n), "x": nil}}}
	}
	w := newWorld(t)
	v := w.node(TypeVision, "V", w.product)
	version := w.version(v)
	w.mustWrite(func(tx *Tx) error { return UpdateNode(tx, v, &version, FieldsInput{"outputs": payload("1")}) })
	n := len(w.entries)
	for _, same := range []string{"1.0", "10e-1", "0.1e1"} {
		version = w.version(v)
		w.mustWrite(func(tx *Tx) error { return UpdateNode(tx, v, &version, FieldsInput{"outputs": payload(same)}) })
	}
	if len(w.entries) != n {
		t.Error("1, 1.0, and 10e-1 are the same number, so the update changes no value")
	}
	version = w.version(v)
	w.mustWrite(func(tx *Tx) error {
		return UpdateNode(tx, v, &version, FieldsInput{"outputs": payload("1.000000000000000000001")})
	})
	if len(w.entries) != n+1 {
		t.Error("a payload number that differs in its 22nd digit is a change")
	}
	out, _ := Marshal(w.g.Nodes[v].Fields.Outputs)
	if string(out) != `[{"outputType":"git","payload":{"n":1.000000000000000000001,"x":null}}]` {
		t.Errorf("the payload keeps the exact number, got %s", out)
	}
	if decimalKey("1e999999999999") == decimalKey("1e999999999998") {
		t.Error("different exponents are different numbers")
	}
}

// ── Lists ───────────────────────────────────────────────────────────────────

func TestListNodesKeysetPagination(t *testing.T) {
	w := newWorld(t)
	v := w.node(TypeVision, "V", w.product)
	g := w.node(TypeGoal, "G", v)
	var goals []uuid.UUID
	for _, title := range []string{"G1", "G2", "G3", "G4"} {
		goals = append(goals, w.node(TypeGoal, title, v))
	}
	goals = append(goals, g)
	// Archiving V and unarchiving it gives V and each Goal the same version.
	if err := w.setMark(v, true); err != nil {
		t.Fatal(err)
	}
	if err := w.setMark(v, false); err != nil {
		t.Fatal(err)
	}
	all := append(slices.Clone(goals), v)
	sameVersion := w.version(v)
	version := w.version(goals[0])
	w.mustWrite(func(tx *Tx) error { return UpdateNode(tx, goals[0], &version, FieldsInput{"title": "G1 again"}) })

	page := func(in ListInput) NodePage {
		p, err := ListNodes(NewView(w.g), in)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	collect := func(in ListInput) []NodeJSON {
		var out []NodeJSON
		for {
			p := page(in)
			out = append(out, p.Nodes...)
			if len(p.Nodes) < in.Limit {
				return out
			}
			last := p.Nodes[len(p.Nodes)-1].ID
			in.StartAfterKey, in.StartAfterID = p.LastKey, &last
		}
	}
	for _, dir := range []string{"", "ascending"} {
		got := collect(ListInput{Types: []NodeType{TypeGoal, TypeVision}, Direction: dir, Limit: 2})
		if len(got) != len(all) {
			t.Fatalf("%q: want %d Nodes, got %d", dir, len(all), len(got))
		}
		for i := 1; i < len(got); i++ {
			a, b := got[i-1], got[i]
			if a.Version == b.Version && !idLess(a.ID, b.ID) {
				t.Errorf("%q: Nodes with the same version must be in ascending order of id", dir)
			}
			if a.Version != b.Version && (a.Version < b.Version) != (dir == "ascending") {
				t.Errorf("%q: wrong order of versions %d, %d", dir, a.Version, b.Version)
			}
		}
		if dir == "" && got[0].ID != goals[0] {
			t.Error("the highest version comes first by default")
		}
	}
	if w.version(goals[1]) != sameVersion {
		t.Fatal("the test needs Nodes with the same version")
	}

	// The sort priority, with 2 Children that have the same priority.
	for _, id := range goals {
		var e *Edge
		for _, ed := range w.g.Edges {
			if ed.TailID == id {
				e = ed
			}
		}
		w.mustWrite(func(tx *Tx) error {
			return UpdateEdge(tx, e.ID, map[uuid.UUID]int64{goals[0]: 5, goals[1]: 1, goals[2]: 5, goals[3]: 3, g: 1}[id], w.version(id))
		})
	}
	got := collect(ListInput{ChildrenOf: []uuid.UUID{v}, SortPriority: true, Limit: 2})
	var keys []int64
	for _, n := range got {
		for _, ed := range w.g.Edges {
			if ed.TailID == n.ID {
				keys = append(keys, ed.Priority)
			}
		}
	}
	if !slices.Equal(keys, []int64{1, 1, 3, 5, 5}) {
		t.Errorf("priority order: got %v", keys)
	}
	if !idLess(got[0].ID, got[1].ID) || !idLess(got[3].ID, got[4].ID) {
		t.Error("Children with the same priority are in ascending order of id")
	}

	_, err := ListNodes(NewView(w.g), ListInput{SortPriority: true})
	wantCode(t, err, CodeInvalidRequest)
	_, err = ListNodes(NewView(w.g), ListInput{SortPriority: true, ChildrenOf: []uuid.UUID{v}, Direction: "ascending"})
	wantCode(t, err, CodeInvalidRequest)
	one := int64(1)
	_, err = ListNodes(NewView(w.g), ListInput{StartAfterKey: &one})
	wantCode(t, err, CodeInvalidRequest)
}

func TestListNodesFilters(t *testing.T) {
	w := newWorld(t)
	v := w.node(TypeVision, "V", w.product)
	g1 := w.node(TypeGoal, "G1", v)
	g2 := w.node(TypeGoal, "G2", v)
	f := w.node(TypeFeature, "F", g1)
	w.mustEdge(Dependency, g2, g1)
	if err := w.setMark(g1, true); err != nil {
		t.Fatal(err)
	}
	ids := func(in ListInput) []uuid.UUID {
		p, err := ListNodes(NewView(w.g), in)
		if err != nil {
			t.Fatal(err)
		}
		var out []uuid.UUID
		for _, n := range p.Nodes {
			out = append(out, n.ID)
		}
		slices.SortFunc(out, idCmp)
		return out
	}
	sorted := func(s ...uuid.UUID) []uuid.UUID { slices.SortFunc(s, idCmp); return s }
	if got := ids(ListInput{DescendantsOf: []uuid.UUID{v}}); !slices.Equal(got, []uuid.UUID{g2}) {
		t.Errorf("by default, archived Nodes are left out: got %v", got)
	}
	if got := ids(ListInput{DescendantsOf: []uuid.UUID{v}, Archived: []bool{true, false}}); !slices.Equal(got, sorted(g1, g2, f)) {
		t.Errorf("descendants: got %v", got)
	}
	if got := ids(ListInput{AncestorsOf: []uuid.UUID{f}, Archived: []bool{true, false}}); !slices.Equal(got, sorted(g1, v, w.product)) {
		t.Errorf("ancestors: got %v", got)
	}
	if got := ids(ListInput{DependentsOf: []uuid.UUID{g1}}); !slices.Equal(got, []uuid.UUID{g2}) {
		t.Errorf("dependents: got %v", got)
	}
	if got := ids(ListInput{Blocked: []bool{true}, Archived: []bool{true, false}}); len(got) != 0 {
		t.Errorf("G1 is archived, so G2 isn't blocked: got %v", got)
	}
	if got := ids(ListInput{ChildrenOf: []uuid.UUID{uuid.New()}}); len(got) != 0 {
		t.Error("an id of no Node matches no Node")
	}
	none := (*string)(nil)
	if got := ids(ListInput{Watchers: []*string{none}, Types: []NodeType{TypeVision}}); !slices.Equal(got, []uuid.UUID{v}) {
		t.Errorf("no value matches a Node with no watchers: got %v", got)
	}
}

func TestNextWork(t *testing.T) {
	w := newWorld(t)
	v := w.node(TypeVision, "V", w.product)
	g := w.node(TypeGoal, "G", v)
	f := w.node(TypeFeature, "F", g)
	r := w.node(TypeRequirement, "R", f)
	t1 := w.node(TypeTask, "T1", r) // priority 10
	t2 := w.node(TypeTask, "T2", r) // priority 11
	t3 := w.node(TypeTask, "T3", r) // priority 12
	next := func() uuid.UUID {
		n := NextWork(NewView(w.g))
		if n == nil {
			return uuid.Nil
		}
		return n.ID
	}
	if next() != t1 {
		t.Fatal("the next available leaf is the first Child by priority")
	}
	w.mustEdge(Dependency, t1, t3) // T1 is blocked
	if next() != t2 {
		t.Fatal("a blocked leaf isn't available")
	}
	version := w.version(t2)
	w.mustWrite(func(tx *Tx) error { return UpdateNode(tx, t2, &version, FieldsInput{"assignee": "bob"}) })
	if next() != t3 {
		t.Fatal("an assigned leaf isn't available")
	}
	w.mustStatus(t3, Done)
	if next() != t1 {
		t.Fatal("T1 isn't blocked once T3 is done")
	}
	if err := w.setMark(r, true); err != nil {
		t.Fatal(err)
	}
	if next() != uuid.Nil {
		t.Fatal("an archived Node isn't visited")
	}
}

// TestNextWorkSkipsBlockedSubtree checks step 1 of W2: a blocked Node that
// isn't a leaf hides its Children.
func TestNextWorkSkipsBlockedSubtree(t *testing.T) {
	w := newWorld(t)
	v := w.node(TypeVision, "V", w.product)
	g := w.node(TypeGoal, "G", v)
	f1 := w.node(TypeFeature, "F1", g)
	f2 := w.node(TypeFeature, "F2", g)
	r1 := w.node(TypeRequirement, "R1", f1)
	r2 := w.node(TypeRequirement, "R2", f2)
	w.node(TypeTask, "T1", r1)
	t2 := w.node(TypeTask, "T2", r2)
	w.mustEdge(Dependency, f1, f2)
	if n := NextWork(NewView(w.g)); n == nil || n.ID != t2 {
		t.Fatalf("F1 is blocked, so the next work is under F2, got %v", n)
	}
}

func TestListNodesSearch(t *testing.T) {
	w := newWorld(t)
	sso := w.node(TypeVision, "SSO Integration", w.product)
	other := w.node(TypeVision, "Billing", w.product)
	version := w.version(other)
	w.mustWrite(func(tx *Tx) error {
		return UpdateNode(tx, other, &version, FieldsInput{"description": "Uses OAuth for payments"})
	})
	for search, want := range map[string][]uuid.UUID{
		"sso":     {sso},        // ignores case
		"TEGRAT":  {sso},        // any substring
		"oauth":   {other},      // the description
		"i":       {sso, other}, // both
		"missing": nil,          // neither
	} {
		p, err := ListNodes(NewView(w.g), ListInput{Search: &search, Types: []NodeType{TypeVision}})
		if err != nil {
			t.Fatal(err)
		}
		var got []uuid.UUID
		for _, n := range p.Nodes {
			got = append(got, n.ID)
		}
		slices.SortFunc(got, idCmp)
		slices.SortFunc(want, idCmp)
		if !slices.Equal(got, want) {
			t.Errorf("search %q: got %v, want %v", search, got, want)
		}
	}
}
