package graph_test

import (
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/d4nyll/nodoplex/internal/graph"
	"github.com/d4nyll/nodoplex/internal/graph/graphtest"
)

// TestPartialMatchesWhole runs random operations on random graphs 2 ways:
// on the whole graph, and on a partial graph that the in-memory loader
// fills. Each response, refusal, Change Entry, and written graph must be the
// same. NODOPLEX_PARTIAL_SEEDS sets the number of graphs.
func TestPartialMatchesWhole(t *testing.T) {
	seeds := 400
	if s, err := strconv.Atoi(os.Getenv("NODOPLEX_PARTIAL_SEEDS")); err == nil {
		seeds = s
	}
	if testing.Short() {
		seeds = 40
	}
	seen := map[string]int{}
	for seed := range seeds {
		rng := rand.New(rand.NewPCG(uint64(seed), 7))
		w, feed := graphtest.Generate(rng, "com.example.app", 2+rng.IntN(120))
		for i := range 80 {
			op := graphtest.RandomOp(rng, w)
			opSeed := rng.Uint64()
			want, after, feedAfter := graphtest.RunWhole(w, feed, op, opSeed)
			got, _, tx := graphtest.RunPartial(w.Slug, feed, graphtest.MemoryLoader(w), op, opSeed)
			if got != want {
				t.Fatalf("seed %d, op %d (%s):\nwhole:   %+v\npartial: %+v", seed, i, op.Name, want, got)
			}
			if tx != nil {
				if d := graphtest.Diff(graphtest.Apply(w, tx), after); d != "" {
					t.Fatalf("seed %d, op %d (%s): the written graphs differ: %s", seed, i, op.Name, d)
				}
			}
			code, _, _ := strings.Cut(want.Err, ":")
			seen[op.Name+" "+code]++
			w, feed = after, feedAfter
			if w.ProductNode() == nil {
				break // the store refuses each operation on a deleted Product
			}
		}
	}
	for _, code := range []string{graph.CodeInvalidRequest, graph.CodeNotFound, graph.CodeInvalidReference,
		graph.CodeAlreadyConnected, graph.CodeCycle, graph.CodeParentRequired, graph.CodeHasIncomingEdges,
		graph.CodeVersionConflict, graph.CodeStatusIsDerived, graph.CodeDoneLeafBlocked, graph.CodePriorityOutOfRange} {
		found := false
		for k := range seen {
			found = found || strings.HasSuffix(k, " "+code)
		}
		if !found && !testing.Short() {
			t.Errorf("no operation was refused with %s", code)
		}
	}
	t.Logf("outcomes: %v", seen)
}

// TestPartialReadsLittle generates a Product of 5000 Nodes, and runs common
// operations on it. Each one must give what it gives on the whole graph, and
// read at most 1 in 10 of its Nodes, or 1 in 2 for a page of ListNodes that
// selects few Nodes by a filter that the Graph checks.
func TestPartialReadsLittle(t *testing.T) {
	rng := rand.New(rand.NewPCG(5000, 1))
	w, feed := graphtest.Generate(rng, "com.example.big", 5000)
	for _, c := range graphtest.CommonOps(rng, w) {
		want, _, _ := graphtest.RunWhole(w, feed, c.Op, 1)
		loads := 0
		load := graphtest.MemoryLoader(w)
		got, g, _ := graphtest.RunPartial(w.Slug, feed, func(g *graph.Graph, m graph.Misses) error {
			loads++
			return load(g, m)
		}, c.Op, 1)
		if got != want {
			t.Fatalf("%s:\nwhole:   %+v\npartial: %+v", c.Name, want, got)
		}
		t.Logf("%-40s reads %4d of %d Nodes in %2d loads; %s", c.Name, len(g.Nodes), len(w.Nodes), loads, short(want))
		if limit := len(w.Nodes) / 10; len(g.Nodes) > limit && !(strings.HasPrefix(c.Name, "ListNodes, ") && len(g.Nodes) <= limit*5) {
			t.Errorf("%s reads %d of %d Nodes", c.Name, len(g.Nodes), len(w.Nodes))
		}
	}
}

func short(o graphtest.Outcome) string {
	if o.Err != "" {
		return o.Err
	}
	return "ok"
}
