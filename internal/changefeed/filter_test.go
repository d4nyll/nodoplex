package changefeed

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/d4nyll/nodoplex/internal/graph"
	"github.com/google/uuid"
)

func node(t graph.NodeType, s graph.Status, assignee *string, archived bool) *NodeState {
	return &NodeState{Type: t, Status: s, Assignee: assignee, Archived: archived}
}

func ptr[T any](v T) *T { return &v }

func groups(t *testing.T, text string) []Group {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	g, err := ParseGroups(v)
	if err != nil {
		t.Fatalf("ParseGroups(%s): %v", text, err)
	}
	return g
}

var noWatchers = func(uuid.UUID) []string { return nil }

func TestStateFilterChecks(t *testing.T) {
	task := graph.TypeTask
	update := Change{NodeID: uuid.New(), Before: node(task, graph.Done, nil, false), After: node(task, graph.InProgress, nil, false)}
	creation := Change{NodeID: uuid.New(), After: node(task, graph.Done, nil, false)}
	deletion := Change{NodeID: uuid.New(), Before: node(task, graph.Done, nil, false)}
	cases := []struct {
		filter string
		change Change
		want   bool
	}{
		{`[{"status":{"check":"before","values":["done"]}}]`, update, true},
		{`[{"status":{"check":"after","values":["done"]}}]`, update, false},
		{`[{"status":{"check":"both","values":["done"]}}]`, update, false},
		{`[{"status":{"check":"either","values":["done"]}}]`, update, true},
		{`[{"status":{"check":"both","beforeValues":["done"],"afterValues":["in_progress"]}}]`, update, true},
		{`[{"status":{"check":"both","beforeValues":["in_progress"],"afterValues":["done"]}}]`, update, false},
		// A creation has nothing before, and a deletion has nothing after.
		{`[{"status":{"check":"before","values":["done"]}}]`, creation, false},
		{`[{"status":{"check":"both","values":["done"]}}]`, creation, false},
		{`[{"status":{"check":"either","values":["done"]}}]`, creation, true},
		{`[{"status":{"check":"after","values":["done"]}}]`, deletion, false},
		{`[{"status":{"check":"both","values":["done"]}}]`, deletion, false},
		{`[{"status":{"check":"either","values":["done"]}}]`, deletion, true},
		{`[{"assignee":{"check":"after","values":[null]}}]`, update, true},
		{`[{"assignee":{"check":"after","values":["bob"]}}]`, update, false},
		{`[{"type":["requirement"]}]`, update, false},
		{`[{"type":["requirement"]},{"type":["task"],"status":{"check":"after","values":["in_progress"]}}]`, update, true},
	}
	for _, c := range cases {
		if got := Matches(groups(t, c.filter), []Change{c.change}, noWatchers); got != c.want {
			t.Errorf("%s: got %v, want %v", c.filter, got, c.want)
		}
	}
}

func TestDefaultArchivedFilter(t *testing.T) {
	task := graph.TypeTask
	staysArchived := Change{NodeID: uuid.New(), Before: node(task, graph.Done, nil, true), After: node(task, graph.InProgress, nil, true)}
	becomesArchived := Change{NodeID: uuid.New(), Before: node(task, graph.Done, nil, false), After: node(task, graph.Done, nil, true)}
	if Matches(nil, []Change{staysArchived}, noWatchers) {
		t.Error("with no filter groups, a change to a Node that stays archived doesn't match")
	}
	if !Matches(nil, []Change{becomesArchived}, noWatchers) {
		t.Error("with no filter groups, a change to a Node that wasn't archived before matches")
	}
	if Matches(groups(t, `[{"type":["task"]}]`), []Change{staysArchived}, noWatchers) {
		t.Error("each filter group holds the default archived filter")
	}
	if !Matches(groups(t, `[{"type":["task"],"archived":{"check":"after","values":[true]}}]`), []Change{staysArchived}, noWatchers) {
		t.Error("a filter group's own archived filter replaces the default")
	}
	// One matching change is enough for the whole Change Entry.
	if !Matches(nil, []Change{staysArchived, becomesArchived}, noWatchers) {
		t.Error("a Change Entry matches if one of its changes matches")
	}
}

func TestEdgeChanges(t *testing.T) {
	tail, head := uuid.New(), uuid.New()
	edge := func(tb, ta, hb, ha *bool) Change {
		return Change{Edge: true,
			Tail: End{ID: tail, Type: graph.TypeTask, ArchivedBefore: tb, ArchivedAfter: ta},
			Head: End{ID: head, Type: graph.TypeRequirement, ArchivedBefore: hb, ArchivedAfter: ha}}
	}
	f, tr := ptr(false), ptr(true)
	bothArchived := edge(tr, tr, tr, tr)
	tailArchived := edge(tr, tr, f, f)
	// The deletion of a Node and of its Edge: the Tail Node exists only before.
	deletedWithTail := edge(tr, nil, tr, tr)
	if Matches(nil, []Change{bothArchived}, noWatchers) {
		t.Error("an Edge whose ends are both archived doesn't match the default archived filter")
	}
	if !Matches(nil, []Change{tailArchived}, noWatchers) {
		t.Error("an Edge matches if one end isn't archived")
	}
	if Matches(groups(t, `[{"archived":{"check":"after","values":[true]}}]`), []Change{edge(tr, nil, f, f)}, noWatchers) {
		t.Error("a deleted end Node isn't checked after the write")
	}
	if !Matches(groups(t, `[{"archived":{"check":"after","values":[true]}}]`), []Change{deletedWithTail}, noWatchers) {
		t.Error("with after, the deletion of an Edge matches if an end Node matches after the write")
	}
	if !Matches(groups(t, `[{"type":["requirement"]}]`), []Change{tailArchived}, noWatchers) {
		t.Error("type matches an Edge with a Node of that Type at either end")
	}
	if Matches(groups(t, `[{"status":{"check":"either","values":["not_started","in_progress","done"]}}]`), []Change{tailArchived}, noWatchers) {
		t.Error("status never matches a change to an Edge")
	}
	if Matches(groups(t, `[{"assignee":{"check":"either","values":[null]}}]`), []Change{tailArchived}, noWatchers) {
		t.Error("assignee never matches a change to an Edge")
	}
	// The creation of an Edge from a new Node: the Tail Node exists only after.
	if !Matches(groups(t, `[{"archived":{"check":"both","values":[false]}}]`), []Change{edge(nil, f, f, f)}, noWatchers) {
		t.Error("with both, the Head Node that isn't archived before and after matches")
	}
	if Matches(groups(t, `[{"archived":{"check":"both","values":[false]}}]`), []Change{edge(nil, f, tr, tr)}, noWatchers) {
		t.Error("with both, a Tail Node that doesn't exist before doesn't match")
	}
}

func TestWatcherFilter(t *testing.T) {
	watched, other := uuid.New(), uuid.New()
	// watchers gives the watchers at the time of the check: for a deleted
	// Node, the store gives those that the Node had when it was deleted.
	watchers := map[uuid.UUID][]string{watched: {"alice"}}
	lookup := func(id uuid.UUID) []string { return watchers[id] }
	g := groups(t, `[{"watcher":["alice"]}]`)
	nodeChange := func(id uuid.UUID) Change {
		return Change{NodeID: id, Before: node(graph.TypeTask, graph.Done, nil, false), After: node(graph.TypeTask, graph.Done, nil, false)}
	}
	edge := Change{Edge: true, Tail: End{ID: other, ArchivedAfter: ptr(false)}, Head: End{ID: watched, ArchivedAfter: ptr(false)}}
	if !Matches(g, []Change{nodeChange(watched)}, lookup) || Matches(g, []Change{nodeChange(other)}, lookup) {
		t.Error("watcher matches a change to a Node that the user watches")
	}
	if !Matches(g, []Change{edge}, lookup) {
		t.Error("watcher matches an Edge with a watched Node at either end")
	}
	// The user stops watching: the same Change Entry no longer matches.
	watchers[watched] = nil
	if Matches(g, []Change{nodeChange(watched)}, lookup) {
		t.Error("the watcher filter uses the watchers at the time of the check")
	}
	if !Matches(groups(t, `[{"watcher":[null]}]`), []Change{nodeChange(watched)}, lookup) {
		t.Error("null matches a Node that has no watchers")
	}
}

func TestParseGroupsRefusals(t *testing.T) {
	for _, text := range []string{
		`{}`,
		`[{}]`,
		`[{"type":[]}]`,
		`[{"type":["output"]}]`,
		`[{"colour":["red"]}]`,
		`[{"status":{"values":["done"]}}]`,
		`[{"status":{"check":"after","beforeValues":["done"],"afterValues":["done"]}}]`,
		`[{"status":{"check":"both","values":["done"],"afterValues":["done"]}}]`,
		`[{"status":{"check":"both","beforeValues":["done"]}}]`,
		`[{"status":{"check":"both","values":["later"]}}]`,
		`[{"status":{"check":"sometimes","values":["done"]}}]`,
		`[{"assignee":{"check":"after","values":[""]}}]`,
		`[{"archived":{"check":"after","values":["false"]}}]`,
		`[{"watcher":[""]}]`,
		`[{"status":{"check":"after","values":["done"],"extra":1}}]`,
	} {
		dec := json.NewDecoder(strings.NewReader(text))
		dec.UseNumber()
		var v any
		dec.Decode(&v)
		if _, err := ParseGroups(v); err == nil {
			t.Errorf("ParseGroups(%s) must refuse", text)
		}
	}
}

func TestParseEntry(t *testing.T) {
	// A Change Entry as graph.Tx.Complete writes it.
	g := graph.NewGraph("com.example.app")
	tx := graph.NewTx(g, graph.Feed{}, "alice", time.Now())
	product, err := graph.CreateProduct(tx, graph.FieldsInput{"title": "P"}, false)
	if err != nil || tx.Complete() != nil {
		t.Fatal(err)
	}
	tx = graph.NewTx(tx.G, tx.Feed, "alice", time.Now())
	vision, err := graph.CreateNode(tx, graph.TypeVision, []graph.ParentRef{{ID: product}}, graph.FieldsInput{"title": "V", "assignee": "bob"})
	if err != nil || tx.Complete() != nil {
		t.Fatal(err)
	}
	changes, err := ParseEntry(tx.Entry.JSON, tx.Entry.EdgeEnds)
	if err != nil {
		t.Fatal(err)
	}
	var sawVision, sawEdge bool
	for _, c := range changes {
		if !c.Edge && c.NodeID == vision {
			sawVision = c.Before == nil && c.After.Type == graph.TypeVision && *c.After.Assignee == "bob"
		}
		if c.Edge {
			sawEdge = c.Tail.ID == vision && c.Head.ID == product && c.Tail.ArchivedBefore == nil &&
				*c.Head.ArchivedBefore == false && c.Head.Type == graph.TypeProduct
		}
	}
	if !sawVision || !sawEdge {
		t.Errorf("ParseEntry: vision %v, edge %v, changes %+v", sawVision, sawEdge, changes)
	}
}

func TestHubKeepsOnePendingSignal(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe("com.example.app")
	other, cancelOther := h.Subscribe("com.example.other")
	defer cancelOther()
	h.Signal("com.example.app")
	h.Signal("com.example.app") // no buffer: still one pending signal
	select {
	case <-ch:
	default:
		t.Fatal("a signal that comes while nobody waits stays pending")
	}
	select {
	case <-ch:
		t.Fatal("the subscription holds at most one pending signal")
	default:
	}
	select {
	case <-other:
		t.Fatal("a signal reaches only the subscriptions of its Product")
	default:
	}
	cancel()
	h.Signal("com.example.app") // no subscription: nothing blocks
}
