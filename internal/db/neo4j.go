package db

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
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

// RunMigrations creates uniqueness constraints for all node types.
func RunNeo4jMigrations(ctx context.Context, driver neo4j.DriverWithContext) error {
	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	for _, label := range nodeLabels {
		query := fmt.Sprintf(
			"CREATE CONSTRAINT IF NOT EXISTS FOR (n:%s) REQUIRE n.id IS UNIQUE",
			label,
		)
		if _, err := session.Run(ctx, query, nil); err != nil {
			return fmt.Errorf("neo4j: create constraint for %s: %w", label, err)
		}
	}
	return nil
}
