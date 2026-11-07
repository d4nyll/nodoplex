package db

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
)

// NewNeo4jDriver creates a new Neo4j driver and verifies connectivity.
func NewNeo4jDriver(ctx context.Context, uri, user, password string) (neo4j.DriverWithContext, error) {
	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(user, password, ""))
	if err != nil {
		return nil, fmt.Errorf("neo4j: create driver: %w", err)
	}
	if err := driver.VerifyConnectivity(ctx); err != nil {
		return nil, fmt.Errorf("neo4j: verify connectivity: %w", err)
	}
	return driver, nil
}

// nodeLabels lists all node labels that need uniqueness constraints.
var nodeLabels = []string{"Vision", "Goal", "Feature", "Requirement", "Task", "GitRef"}

// RunMigrations creates uniqueness constraints for all node types, the
// constraint and index on the :Node label that namespace-scoped lookups use,
// and the constraint on the per-namespace lock nodes that
// graph.Repository.CreateDependency takes.
func RunNeo4jMigrations(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	queries := []string{}
	for _, label := range nodeLabels {
		queries = append(queries, fmt.Sprintf(
			"CREATE CONSTRAINT IF NOT EXISTS FOR (n:%s) REQUIRE n.id IS UNIQUE",
			label,
		))
	}
	queries = append(queries,
		// Lookups by id within a namespace seek this index, then filter on namespace.
		"CREATE CONSTRAINT node_id_unique IF NOT EXISTS FOR (n:Node) REQUIRE n.id IS UNIQUE",
		// Listing a namespace seeks this index.
		"CREATE INDEX node_namespace IF NOT EXISTS FOR (n:Node) ON (n.namespace)",
		// Lets concurrent MERGEs of a namespace's lock node agree on one node.
		"CREATE CONSTRAINT dependency_lock_namespace_unique IF NOT EXISTS FOR (l:DependencyLock) REQUIRE l.namespace IS UNIQUE",
	)
	for _, q := range queries {
		// ExecuteWrite retries transient errors such as the deadlock two
		// processes hit when they create the same constraints at once.
		_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
			_, err := tx.Run(ctx, q, nil)
			return nil, err
		})
		if err != nil {
			return fmt.Errorf("neo4j: %s: %w", q, err)
		}
	}
	return nil
}

// legacyNodesMatch matches work nodes created before namespaces existed: they
// carry a type label but no namespace property.
const legacyNodesMatch = `MATCH (n) WHERE n.namespace IS NULL AND (n:Vision OR n:Goal OR n:Feature OR n:Requirement OR n:Task OR n:GitRef)`

// CountNodesWithoutNamespace returns the number of work nodes that have no
// namespace. The API never returns these nodes; AssignNamespace moves them
// into a namespace.
func CountNodesWithoutNamespace(ctx context.Context, driver neo4j.DriverWithContext) (int64, error) {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx, legacyNodesMatch+" RETURN count(n) AS c", nil)
	if err != nil {
		return 0, fmt.Errorf("neo4j: count nodes without namespace: %w", err)
	}
	record, err := result.Single(ctx)
	if err != nil {
		return 0, fmt.Errorf("neo4j: count nodes without namespace: %w", err)
	}
	c, _ := record.Get("c")
	return c.(int64), nil
}

// AssignNamespace puts every work node that has no namespace into namespace
// ns and adds the :Node label to it. Nodes that already have a namespace are
// left alone, so running it again is a no-op. Returns the number of nodes
// updated.
func AssignNamespace(ctx context.Context, driver neo4j.DriverWithContext, ns string) (int64, error) {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	result, err := session.Run(ctx,
		legacyNodesMatch+" SET n:Node, n.namespace = $ns RETURN count(n) AS c",
		map[string]any{"ns": ns},
	)
	if err != nil {
		return 0, fmt.Errorf("neo4j: assign namespace: %w", err)
	}
	record, err := result.Single(ctx)
	if err != nil {
		return 0, fmt.Errorf("neo4j: assign namespace: %w", err)
	}
	c, _ := record.Get("c")
	return c.(int64), nil
}
