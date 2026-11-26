package db

import (
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
)

// NewNeo4jDriver creates a Neo4j driver. The driver doesn't connect until its
// first query.
func NewNeo4jDriver(uri, user, password string) (neo4j.Driver, error) {
	driver, err := neo4j.NewDriver(uri, neo4j.BasicAuth(user, password, ""))
	if err != nil {
		return nil, fmt.Errorf("neo4j: create driver: %w", err)
	}
	return driver, nil
}
