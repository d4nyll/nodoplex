package graph

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/d4nyll/nodoplex/internal/config"
	"github.com/d4nyll/nodoplex/internal/db"
	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
)

// Integration tests run against the Neo4j instance in NEO4J_URI and are
// skipped unless NODOPLEX_INTEGRATION=1.

func integrationDriver(t *testing.T) neo4j.DriverWithContext {
	t.Helper()
	if os.Getenv("NODOPLEX_INTEGRATION") != "1" {
		t.Skip("set NODOPLEX_INTEGRATION=1 to run against Neo4j")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	ctx := context.Background()
	driver, err := db.NewNeo4jDriver(ctx, cfg.Neo4jURI, cfg.Neo4jUser, cfg.Neo4jPassword)
	if err != nil {
		t.Fatalf("neo4j: %v", err)
	}
	t.Cleanup(func() { driver.Close(ctx) })
	if err := db.RunNeo4jMigrations(ctx, driver); err != nil {
		t.Fatalf("neo4j migrations: %v", err)
	}
	return driver
}

// planDriver wraps a driver so that every query the repository runs through
// session.Run or a managed transaction is first run with EXPLAIN, and the
// operators of its plan are recorded against the query text. If beforeRun is
// set, it is called with each query before the query runs.
type planDriver struct {
	neo4j.DriverWithContext
	mu        sync.Mutex
	plans     map[string][]string
	beforeRun func(cypher string)
}

func (d *planDriver) NewSession(ctx context.Context, cfg neo4j.SessionConfig) neo4j.SessionWithContext {
	return &planSession{SessionWithContext: d.DriverWithContext.NewSession(ctx, cfg), d: d}
}

func (d *planDriver) explain(ctx context.Context, run func(string) (neo4j.ResultWithContext, error), cypher string) error {
	if d.beforeRun != nil {
		d.beforeRun(cypher)
	}
	res, err := run("EXPLAIN " + cypher)
	if err != nil {
		return err
	}
	summary, err := res.Consume(ctx)
	if err != nil {
		return err
	}
	var ops []string
	var walk func(p neo4j.Plan)
	walk = func(p neo4j.Plan) {
		ops = append(ops, strings.Split(p.Operator(), "@")[0])
		for _, c := range p.Children() {
			walk(c)
		}
	}
	walk(summary.Plan())
	d.mu.Lock()
	defer d.mu.Unlock()
	d.plans[cypher] = ops
	return nil
}

type planSession struct {
	neo4j.SessionWithContext
	d *planDriver
}

func (s *planSession) Run(ctx context.Context, cypher string, params map[string]any, configurers ...func(*neo4j.TransactionConfig)) (neo4j.ResultWithContext, error) {
	err := s.d.explain(ctx, func(q string) (neo4j.ResultWithContext, error) {
		return s.SessionWithContext.Run(ctx, q, params, configurers...)
	}, cypher)
	if err != nil {
		return nil, err
	}
	return s.SessionWithContext.Run(ctx, cypher, params, configurers...)
}

func (s *planSession) ExecuteWrite(ctx context.Context, work neo4j.ManagedTransactionWork, configurers ...func(*neo4j.TransactionConfig)) (any, error) {
	return s.SessionWithContext.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		return work(&planTx{ManagedTransaction: tx, d: s.d})
	}, configurers...)
}

type planTx struct {
	neo4j.ManagedTransaction
	d *planDriver
}

func (tx *planTx) Run(ctx context.Context, cypher string, params map[string]any) (neo4j.ResultWithContext, error) {
	err := tx.d.explain(ctx, func(q string) (neo4j.ResultWithContext, error) {
		return tx.ManagedTransaction.Run(ctx, q, params)
	}, cypher)
	if err != nil {
		return nil, err
	}
	return tx.ManagedTransaction.Run(ctx, cypher, params)
}

// TestIntegration_QueriesUseIndexes runs every repository method and checks
// that no query scans all nodes or a whole label: each one that looks nodes
// up starts from a unique constraint or the (:Node) namespace index.
func TestIntegration_QueriesUseIndexes(t *testing.T) {
	pd := &planDriver{DriverWithContext: integrationDriver(t), plans: map[string][]string{}}
	repo := NewRepository(pd)
	svc := NewService(repo)
	ctx := context.Background()
	ns := "plans-" + uuid.NewString()

	create := func(nt NodeType) uuid.UUID {
		n, err := svc.CreateNode(ctx, ns, nt, map[string]any{"title": string(nt), "status": "backlog"})
		if err != nil {
			t.Fatalf("create %s: %v", nt, err)
		}
		return n.ID
	}
	vision, goal, goal2 := create(NodeTypeVision), create(NodeTypeGoal), create(NodeTypeGoal)
	if err := svc.CreateEdge(ctx, ns, vision, goal, EdgeTypeBreaksDownTo); err != nil {
		t.Fatalf("create edge: %v", err)
	}
	if err := svc.CreateEdge(ctx, ns, goal2, goal, EdgeTypeDependsOn); err != nil {
		t.Fatalf("create dependency: %v", err)
	}
	featureType, search := "feature", "x"
	runs := []func() error{
		func() error { _, err := svc.GetNode(ctx, ns, goal); return err },
		func() error { _, _, err := svc.ListNodes(ctx, ns, nil, nil, nil, 0, 10); return err },
		func() error { _, _, err := svc.ListNodes(ctx, ns, &featureType, nil, &search, 0, 10); return err },
		func() error { _, _, err := svc.UpdateNode(ctx, ns, goal, map[string]any{"title": "g2"}); return err },
		func() error { _, err := svc.GetChildren(ctx, ns, vision); return err },
		func() error { _, err := svc.GetParents(ctx, ns, goal); return err },
		func() error { _, err := svc.GetSubtree(ctx, ns, vision); return err },
		func() error { _, err := svc.GetLineage(ctx, ns, goal); return err },
		func() error { _, err := svc.GetDependencies(ctx, ns, goal2); return err },
		func() error { _, err := svc.GetDependents(ctx, ns, goal); return err },
		func() error { return svc.DeleteEdge(ctx, ns, goal2, goal, EdgeTypeDependsOn) },
		func() error { return svc.DeleteEdge(ctx, ns, vision, goal, EdgeTypeBreaksDownTo) },
		func() error { _, err := svc.DeleteNode(ctx, ns, goal); return err },
	}
	for i, run := range runs {
		if err := run(); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}

	if len(pd.plans) == 0 {
		t.Fatal("no plans recorded")
	}
	for q, ops := range pd.plans {
		joined := strings.Join(ops, " ")
		for _, bad := range []string{"AllNodesScan", "NodeByLabelScan", "UnionNodeByLabelsScan", "IntersectionNodeByLabelsScan"} {
			if strings.Contains(joined, bad) {
				t.Errorf("query uses %s:\n%s\nplan: %s", bad, q, joined)
			}
		}
		if strings.HasPrefix(q, "CREATE ") {
			continue
		}
		// MergeUniqueNode is a MERGE that looks the node up through a unique constraint.
		if !strings.Contains(joined, "NodeUniqueIndexSeek") && !strings.Contains(joined, "NodeIndexSeek") && !strings.Contains(joined, "MergeUniqueNode") {
			t.Errorf("query uses no index seek:\n%s\nplan: %s", q, joined)
		}
	}
	t.Logf("checked %d distinct queries", len(pd.plans))
}

// TestIntegration_ConcurrentDependenciesCannotFormCycle creates the edges of
// a ring (n0->n1->...->n0) concurrently. Each edge alone is valid and any
// k-1 of them form a chain, so exactly one request must fail with
// ErrDependencyCycle and the graph must end up without a cycle.
func TestIntegration_ConcurrentDependenciesCannotFormCycle(t *testing.T) {
	repo := NewRepository(integrationDriver(t))
	svc := NewService(repo)
	ctx := context.Background()

	for _, k := range []int{2, 3, 5} {
		for round := 0; round < 20; round++ {
			ns := "cycle-" + uuid.NewString()
			ids := make([]uuid.UUID, k)
			for i := range ids {
				n, err := svc.CreateNode(ctx, ns, NodeTypeRequirement, map[string]any{"title": "r"})
				if err != nil {
					t.Fatalf("create node: %v", err)
				}
				ids[i] = n.ID
			}

			start := make(chan struct{})
			errs := make([]error, k)
			var wg sync.WaitGroup
			for i := range k {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					errs[i] = svc.CreateEdge(ctx, ns, ids[i], ids[(i+1)%k], EdgeTypeDependsOn)
				}()
			}
			close(start)
			wg.Wait()

			cycles := 0
			for _, err := range errs {
				switch {
				case err == nil:
				case errors.Is(err, ErrDependencyCycle):
					cycles++
				default:
					t.Fatalf("k=%d round %d: unexpected error: %v", k, round, err)
				}
			}
			if cycles != 1 {
				t.Errorf("k=%d round %d: %d requests failed with a cycle, want 1 (errors: %v)", k, round, cycles, errs)
			}

			res, err := neo4j.ExecuteQuery(ctx, repo.driver,
				`MATCH (a:Node {namespace: $ns}) WHERE EXISTS { (a)-[:DEPENDS_ON]->+(a) } RETURN count(a) AS c`,
				map[string]any{"ns": ns}, neo4j.EagerResultTransformer)
			if err != nil {
				t.Fatalf("cycle query: %v", err)
			}
			if c, _ := res.Records[0].Get("c"); c.(int64) != 0 {
				t.Fatalf("k=%d round %d: %d nodes are on a DEPENDS_ON cycle", k, round, c)
			}
		}
	}
}

// TestIntegration_DependencyTargetDeletedDuringCreate deletes the target node
// after CreateDependency's checks pass and before its CREATE runs. Deletes
// don't take the dependency lock, so this can happen; the request must
// report the node as missing rather than claim it created the edge.
func TestIntegration_DependencyTargetDeletedDuringCreate(t *testing.T) {
	driver := integrationDriver(t)
	ctx := context.Background()
	ns := "deleted-" + uuid.NewString()
	plain := NewService(NewRepository(driver))
	source, err := plain.CreateNode(ctx, ns, NodeTypeTask, map[string]any{"title": "source"})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	target, err := plain.CreateNode(ctx, ns, NodeTypeTask, map[string]any{"title": "target"})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}

	pd := &planDriver{DriverWithContext: driver, plans: map[string][]string{}}
	pd.beforeRun = func(cypher string) {
		if strings.Contains(cypher, "CREATE (a)-[:DEPENDS_ON]->(b)") {
			if _, err := plain.DeleteNode(ctx, ns, target.ID); err != nil {
				t.Errorf("delete target: %v", err)
			}
		}
	}
	err = NewService(NewRepository(pd)).CreateEdge(ctx, ns, source.ID, target.ID, EdgeTypeDependsOn)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("CreateEdge = %v, want ErrNotFound", err)
	}
}
