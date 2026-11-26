package store

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/d4nyll/nodoplex/internal/db"
	"github.com/d4nyll/nodoplex/internal/graph"
	"github.com/d4nyll/nodoplex/internal/graph/graphtest"
	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
)

// These tests compare the Neo4j loader with loadGraph, which reads the whole
// graph of a Product, as the server did before it read partial graphs. They
// run only if NODOPLEX_INTEGRATION=1, against the Neo4j server of NEO4J_URI.

// loadGraph reads each Node and each Edge of the Product.
func loadGraph(ctx context.Context, tx neo4j.ManagedTransaction, slug string) (*graph.Graph, error) {
	g := graph.NewGraph(slug)
	params := map[string]any{"slug": slug}
	recs, err := run(ctx, tx, `MATCH (n:Node {product: $slug})
		RETURN n.id, n.type, n.version, n.fields, n.watchers, n.archivedMark`, params)
	if err != nil {
		return nil, err
	}
	for _, r := range recs {
		n, err := recordToNode(r.Values)
		if err != nil {
			return nil, err
		}
		g.Nodes[n.ID] = n
	}
	recs, err = run(ctx, tx, `MATCH (t:Node {product: $slug})-[e:EDGE]->(h:Node)
		RETURN e.id, e.kind, t.id, h.id, e.priority`, params)
	if err != nil {
		return nil, err
	}
	for _, r := range recs {
		e := recordToEdge(r.Values)
		g.Edges[e.ID] = e
	}
	return g, nil
}

func getenv(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func neo4jDriver(t *testing.T) neo4j.Driver {
	t.Helper()
	if os.Getenv("NODOPLEX_INTEGRATION") != "1" {
		t.Skip("set NODOPLEX_INTEGRATION=1 to run against Neo4j")
	}
	neo, err := db.NewNeo4jDriver(getenv("NEO4J_URI", "bolt://localhost:7687"), getenv("NEO4J_USER", "neo4j"), getenv("NEO4J_PASSWORD", "changeme"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { neo.Close(context.Background()) })
	if err := migrateNeo4j(context.Background(), neo); err != nil {
		t.Fatal(err)
	}
	return neo
}

// storeGraph writes g to Neo4j as a Product with a new slug, which the test
// deletes when it ends.
func storeGraph(t *testing.T, neo neo4j.Driver, g *graph.Graph) {
	t.Helper()
	ctx := context.Background()
	session := neo.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)
	t.Cleanup(func() {
		s := neo.NewSession(ctx, neo4j.SessionConfig{})
		defer s.Close(ctx)
		for _, q := range []string{
			"MATCH (n:Node {product: $slug}) DETACH DELETE n",
			"MATCH (n:DeletedNode {product: $slug}) DELETE n",
			"MATCH (n:OutboxEntry {slug: $slug}) DELETE n",
			"MATCH (n:Changefeed {slug: $slug}) DELETE n",
		} {
			s.Run(ctx, q, map[string]any{"slug": g.Slug})
		}
	})
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		if _, err := run(ctx, tx, "CREATE (:Changefeed {slug: $slug, state: $state, latestChangeId: 0, latestTime: 0})",
			map[string]any{"slug": g.Slug, "state": feedLive}); err != nil {
			return nil, err
		}
		return nil, persist(ctx, tx, &graph.Tx{Before: graph.NewGraph(g.Slug), G: g})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func testSlug() string { return "it.partial" + strings.ReplaceAll(uuid.NewString(), "-", "") + ".app" }

// TestIntegrationPartialMatchesWhole runs random operations on random
// Products in Neo4j 2 ways: on the graph that loadGraph reads, and on a
// partial graph that the Neo4j loader fills. Each response, refusal, Change
// Entry, and written graph must be the same.
func TestIntegrationPartialMatchesWhole(t *testing.T) {
	neo := neo4jDriver(t)
	ctx := context.Background()
	for seed := range 40 {
		rng := rand.New(rand.NewPCG(uint64(seed), 11))
		w, feed := graphtest.Generate(rng, testSlug(), 2+rng.IntN(100))
		storeGraph(t, neo, w)
		session := neo.NewSession(ctx, neo4j.SessionConfig{})
		for i := range 60 {
			op := graphtest.RandomOp(rng, w)
			opSeed := rng.Uint64()
			type result struct {
				stored         string
				want, got      graphtest.Outcome
				after, written *graph.Graph
				feed           graph.Feed
			}
			res, err := neo4j.ExecuteWrite(ctx, session, func(tx neo4j.ManagedTransaction) (result, error) {
				var r result
				whole, err := loadGraph(ctx, tx, w.Slug)
				if err != nil {
					return r, err
				}
				r.stored = graphtest.Diff(whole, w)
				r.want, r.after, r.feed = graphtest.RunWhole(whole, feed, op, opSeed)
				var ptx *graph.Tx
				r.got, _, ptx = graphtest.RunPartial(w.Slug, feed, loader(ctx, tx, w.Slug), op, opSeed)
				if ptx != nil {
					if err := persist(ctx, tx, ptx); err != nil {
						return r, err
					}
					// The outbox copier of a server in another test would
					// copy the Change Entry.
					if _, err := run(ctx, tx, "MATCH (e:OutboxEntry {slug: $slug}) DELETE e", map[string]any{"slug": w.Slug}); err != nil {
						return r, err
					}
				}
				r.written, err = loadGraph(ctx, tx, w.Slug)
				return r, err
			})
			if err != nil {
				t.Fatal(err)
			}
			if res.stored != "" {
				t.Fatalf("seed %d, op %d: Neo4j doesn't hold the graph: %s", seed, i, res.stored)
			}
			if res.got != res.want {
				t.Fatalf("seed %d, op %d (%s):\nwhole:   %+v\npartial: %+v", seed, i, op.Name, res.want, res.got)
			}
			if d := graphtest.Diff(res.written, res.after); d != "" {
				t.Fatalf("seed %d, op %d (%s): the written graphs differ: %s", seed, i, op.Name, d)
			}
			w, feed = res.after, res.feed
			if w.ProductNode() == nil {
				break
			}
		}
		session.Close(ctx)
	}
}

// TestIntegrationPartialTimes generates a Product of 5000 Nodes in Neo4j,
// and reports for common operations the Nodes that each way reads and its
// time. The 2 ways must give the same outcome.
func TestIntegrationPartialTimes(t *testing.T) {
	neo := neo4jDriver(t)
	ctx := context.Background()
	rng := rand.New(rand.NewPCG(5000, 1))
	w, feed := graphtest.Generate(rng, testSlug(), 5000)
	storeGraph(t, neo, w)
	session := neo.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)
	var report strings.Builder
	for _, c := range graphtest.CommonOps(rng, w) {
		var want, got graphtest.Outcome
		var wholeTime, partTime time.Duration
		var read, loads int
		// The best of 3 runs of each way.
		for range 3 {
			_, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
				start := time.Now()
				whole, err := loadGraph(ctx, tx, w.Slug)
				if err != nil {
					return nil, err
				}
				want, _, _ = graphtest.RunWhole(whole, feed, c.Op, 1)
				d1 := time.Since(start)
				load := loader(ctx, tx, w.Slug)
				loads = 0
				start = time.Now()
				var g *graph.Graph
				got, g, _ = graphtest.RunPartial(w.Slug, feed, func(g *graph.Graph, m graph.Misses) error {
					loads++
					return load(g, m)
				}, c.Op, 1)
				d2 := time.Since(start)
				read = len(g.Nodes)
				if wholeTime == 0 || d1 < wholeTime {
					wholeTime = d1
				}
				if partTime == 0 || d2 < partTime {
					partTime = d2
				}
				return nil, nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		if got != want {
			t.Fatalf("%s:\nwhole:   %+v\npartial: %+v", c.Name, want, got)
		}
		fmt.Fprintf(&report, "\n%-48s whole %4d Nodes %7.1f ms | partial %4d Nodes, %2d loads %6.1f ms",
			c.Name, len(w.Nodes), float64(wholeTime.Microseconds())/1000, read, loads, float64(partTime.Microseconds())/1000)
	}
	t.Log(report.String())
}
