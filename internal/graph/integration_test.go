package graph

import (
	"context"
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
// operators of its plan are recorded against the query text.
type planDriver struct {
	neo4j.DriverWithContext
	mu    sync.Mutex
	plans map[string][]string
}

func (d *planDriver) NewSession(ctx context.Context, cfg neo4j.SessionConfig) neo4j.SessionWithContext {
	return &planSession{SessionWithContext: d.DriverWithContext.NewSession(ctx, cfg), d: d}
}

func (d *planDriver) explain(ctx context.Context, run func(string) (neo4j.ResultWithContext, error), cypher string) error {
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
// up starts from the (:Node) id constraint or the (:Node) namespace index.
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
	vision, goal := create(NodeTypeVision), create(NodeTypeGoal)
	if err := svc.CreateEdge(ctx, ns, vision, goal, EdgeTypeBreaksDownTo); err != nil {
		t.Fatalf("create edge: %v", err)
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
		if !strings.Contains(joined, "NodeUniqueIndexSeek") && !strings.Contains(joined, "NodeIndexSeek") {
			t.Errorf("query uses no index seek:\n%s\nplan: %s", q, joined)
		}
	}
	t.Logf("checked %d distinct queries", len(pd.plans))
}
