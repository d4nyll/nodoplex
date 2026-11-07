package graph

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
)

// Repository handles all Neo4j CRUD and traversal operations.
type Repository struct {
	driver neo4j.DriverWithContext
}

// NewRepository creates a new graph Repository.
func NewRepository(driver neo4j.DriverWithContext) *Repository {
	return &Repository{driver: driver}
}

// CreateNode creates a node with the given label and properties.
func (r *Repository) CreateNode(ctx context.Context, label string, props map[string]any) (GenericNode, error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	query := fmt.Sprintf("CREATE (n:%s $props) RETURN n, labels(n) AS labels", label)
	result, err := session.Run(ctx, query, map[string]any{"props": props})
	if err != nil {
		return GenericNode{}, fmt.Errorf("create node: %w", err)
	}

	if result.Next(ctx) {
		return recordToNode(result.Record())
	}
	return GenericNode{}, fmt.Errorf("create node: no record returned")
}

// GetNode retrieves a single node by ID.
func (r *Repository) GetNode(ctx context.Context, id uuid.UUID) (GenericNode, error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx,
		"MATCH (n {id: $id}) RETURN n, labels(n) AS labels",
		map[string]any{"id": id.String()},
	)
	if err != nil {
		return GenericNode{}, fmt.Errorf("get node: %w", err)
	}
	if result.Next(ctx) {
		return recordToNode(result.Record())
	}
	return GenericNode{}, ErrNotFound
}

// ListNodes returns nodes matching the given filters.
func (r *Repository) ListNodes(ctx context.Context, nodeType *string, status *string, search *string, offset, limit int) ([]GenericNode, int, error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	// Build the MATCH clause
	matchClause := "MATCH (n)"
	if nodeType != nil {
		label, ok := NodeTypeToLabel[NodeType(*nodeType)]
		if !ok {
			return nil, 0, fmt.Errorf("unknown node type: %s", *nodeType)
		}
		matchClause = fmt.Sprintf("MATCH (n:%s)", label)
	}

	// Build WHERE conditions
	where := ""
	params := map[string]any{}
	conditions := []string{}

	if status != nil {
		conditions = append(conditions, "n.status = $status")
		params["status"] = *status
	}
	if search != nil && *search != "" {
		conditions = append(conditions, "(n.title CONTAINS $search OR n.description CONTAINS $search)")
		params["search"] = *search
	}

	if len(conditions) > 0 {
		where = " WHERE "
		for i, c := range conditions {
			if i > 0 {
				where += " AND "
			}
			where += c
		}
	}

	// Count query
	countQuery := matchClause + where + " RETURN count(n) AS total"
	countResult, err := session.Run(ctx, countQuery, params)
	if err != nil {
		return nil, 0, fmt.Errorf("list nodes count: %w", err)
	}
	var total int
	if countResult.Next(ctx) {
		totalVal, _ := countResult.Record().Get("total")
		total = int(totalVal.(int64))
	}

	// Data query
	params["offset"] = offset
	params["limit"] = limit
	dataQuery := matchClause + where + " RETURN n, labels(n) AS labels SKIP $offset LIMIT $limit"
	dataResult, err := session.Run(ctx, dataQuery, params)
	if err != nil {
		return nil, 0, fmt.Errorf("list nodes: %w", err)
	}

	var nodes []GenericNode
	for dataResult.Next(ctx) {
		node, err := recordToNode(dataResult.Record())
		if err != nil {
			return nil, 0, err
		}
		nodes = append(nodes, node)
	}
	return nodes, total, nil
}

// UpdateNode updates a node's properties. Returns the node before and after the update.
func (r *Repository) UpdateNode(ctx context.Context, id uuid.UUID, props map[string]any) (before GenericNode, after GenericNode, err error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	// First, get the current state
	before, err = r.GetNode(ctx, id)
	if err != nil {
		return GenericNode{}, GenericNode{}, err
	}

	// Remove id from props to prevent overwriting
	delete(props, "id")

	result, err := session.Run(ctx,
		"MATCH (n {id: $id}) SET n += $props RETURN n, labels(n) AS labels",
		map[string]any{"id": id.String(), "props": props},
	)
	if err != nil {
		return GenericNode{}, GenericNode{}, fmt.Errorf("update node: %w", err)
	}
	if result.Next(ctx) {
		after, err = recordToNode(result.Record())
		if err != nil {
			return GenericNode{}, GenericNode{}, err
		}
		return before, after, nil
	}
	return GenericNode{}, GenericNode{}, ErrNotFound
}

// DeleteNode removes a node and all its edges. Returns the node before deletion.
func (r *Repository) DeleteNode(ctx context.Context, id uuid.UUID) (GenericNode, error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	// Get current state first
	node, err := r.GetNode(ctx, id)
	if err != nil {
		return GenericNode{}, err
	}

	_, err = session.Run(ctx,
		"MATCH (n {id: $id}) DETACH DELETE n",
		map[string]any{"id": id.String()},
	)
	if err != nil {
		return GenericNode{}, fmt.Errorf("delete node: %w", err)
	}
	return node, nil
}

// CreateEdge creates a relationship between two nodes.
func (r *Repository) CreateEdge(ctx context.Context, sourceID, targetID uuid.UUID, edgeType string) error {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	query := fmt.Sprintf(
		"MATCH (a {id: $sourceId}), (b {id: $targetId}) CREATE (a)-[:%s]->(b)",
		edgeType,
	)
	result, err := session.Run(ctx, query, map[string]any{
		"sourceId": sourceID.String(),
		"targetId": targetID.String(),
	})
	if err != nil {
		return fmt.Errorf("create edge: %w", err)
	}
	summary, err := result.Consume(ctx)
	if err != nil {
		return fmt.Errorf("create edge consume: %w", err)
	}
	if summary.Counters().RelationshipsCreated() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteEdge removes a relationship between two nodes.
func (r *Repository) DeleteEdge(ctx context.Context, sourceID, targetID uuid.UUID, edgeType string) error {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	query := fmt.Sprintf(
		"MATCH (a {id: $sourceId})-[r:%s]->(b {id: $targetId}) DELETE r",
		edgeType,
	)
	result, err := session.Run(ctx, query, map[string]any{
		"sourceId": sourceID.String(),
		"targetId": targetID.String(),
	})
	if err != nil {
		return fmt.Errorf("delete edge: %w", err)
	}
	summary, err := result.Consume(ctx)
	if err != nil {
		return fmt.Errorf("delete edge consume: %w", err)
	}
	if summary.Counters().RelationshipsDeleted() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetChildren returns the direct children of a node (via BREAKS_DOWN_TO or IMPLEMENTED_BY).
func (r *Repository) GetChildren(ctx context.Context, id uuid.UUID) ([]GenericNode, error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx,
		"MATCH (n {id: $id})-[:BREAKS_DOWN_TO|IMPLEMENTED_BY]->(child) RETURN child AS n, labels(child) AS labels",
		map[string]any{"id": id.String()},
	)
	if err != nil {
		return nil, fmt.Errorf("get children: %w", err)
	}
	return collectNodes(ctx, result)
}

// GetParents returns the direct parents of a node.
func (r *Repository) GetParents(ctx context.Context, id uuid.UUID) ([]GenericNode, error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx,
		"MATCH (parent)-[:BREAKS_DOWN_TO|IMPLEMENTED_BY]->(n {id: $id}) RETURN parent AS n, labels(parent) AS labels",
		map[string]any{"id": id.String()},
	)
	if err != nil {
		return nil, fmt.Errorf("get parents: %w", err)
	}
	return collectNodes(ctx, result)
}

// SubtreeNode represents a node and its children in a tree structure.
type SubtreeNode struct {
	Node     GenericNode   `json:"node"`
	Children []SubtreeNode `json:"children"`
}

// GetSubtree returns the full descendant tree of a node.
func (r *Repository) GetSubtree(ctx context.Context, id uuid.UUID) (*SubtreeNode, error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	// Get root node first
	root, err := r.GetNode(ctx, id)
	if err != nil {
		return nil, err
	}

	// Get all descendants with their paths
	result, err := session.Run(ctx,
		`MATCH path = (n {id: $id})-[:BREAKS_DOWN_TO|IMPLEMENTED_BY*]->(desc)
		 WITH desc, labels(desc) AS labels, length(path) AS depth,
		      [rel IN relationships(path) | startNode(rel).id] AS parentIds
		 RETURN desc AS n, labels, depth, parentIds`,
		map[string]any{"id": id.String()},
	)
	if err != nil {
		return nil, fmt.Errorf("get subtree: %w", err)
	}

	// Build tree from flat results
	tree := &SubtreeNode{Node: root, Children: []SubtreeNode{}}
	nodeMap := map[string]*SubtreeNode{id.String(): tree}

	for result.Next(ctx) {
		record := result.Record()
		node, err := recordToNode(record)
		if err != nil {
			return nil, err
		}

		parentIdsVal, _ := record.Get("parentIds")
		parentIds := parentIdsVal.([]any)

		// The direct parent is the last element in parentIds
		directParentID := parentIds[len(parentIds)-1].(string)

		child := SubtreeNode{Node: node, Children: []SubtreeNode{}}
		nodeMap[node.ID.String()] = &child

		if parent, ok := nodeMap[directParentID]; ok {
			parent.Children = append(parent.Children, child)
		}
	}

	// Re-link children since we may have appended copies
	rebuildChildren(tree, nodeMap)

	return tree, nil
}

// rebuildChildren reconstructs the tree pointers from the nodeMap.
func rebuildChildren(node *SubtreeNode, nodeMap map[string]*SubtreeNode) {
	latest := nodeMap[node.Node.ID.String()]
	if latest != nil {
		node.Children = latest.Children
	}
	for i := range node.Children {
		rebuildChildren(&node.Children[i], nodeMap)
	}
}

// GetLineage traces the path from root Vision down to the given node.
func (r *Repository) GetLineage(ctx context.Context, id uuid.UUID) ([]GenericNode, error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx,
		`MATCH path = (root)-[:BREAKS_DOWN_TO|IMPLEMENTED_BY*]->(n {id: $id})
		 WHERE NOT ()-[:BREAKS_DOWN_TO|IMPLEMENTED_BY]->(root)
		 UNWIND nodes(path) AS node
		 RETURN node AS n, labels(node) AS labels`,
		map[string]any{"id": id.String()},
	)
	if err != nil {
		return nil, fmt.Errorf("get lineage: %w", err)
	}
	return collectNodes(ctx, result)
}

// GetNodeType returns the NodeType of a node by its ID.
func (r *Repository) GetNodeType(ctx context.Context, id uuid.UUID) (NodeType, error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx,
		"MATCH (n {id: $id}) RETURN labels(n) AS labels",
		map[string]any{"id": id.String()},
	)
	if err != nil {
		return "", fmt.Errorf("get node type: %w", err)
	}
	if result.Next(ctx) {
		labelsVal, _ := result.Record().Get("labels")
		labels := labelsVal.([]any)
		for _, l := range labels {
			if nt, ok := LabelToNodeType[l.(string)]; ok {
				return nt, nil
			}
		}
	}
	return "", ErrNotFound
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// ErrNotFound is returned when a node or edge is not found.
var ErrNotFound = fmt.Errorf("not found")

func recordToNode(record *neo4j.Record) (GenericNode, error) {
	nodeVal, _ := record.Get("n")
	labelsVal, _ := record.Get("labels")

	dbNode, ok := nodeVal.(neo4j.Node)
	if !ok {
		return GenericNode{}, fmt.Errorf("unexpected node type: %T", nodeVal)
	}

	labels := labelsVal.([]any)
	label := ""
	for _, l := range labels {
		if _, ok := LabelToNodeType[l.(string)]; ok {
			label = l.(string)
			break
		}
	}

	return NodeFromProps(dbNode.Props, label), nil
}

func collectNodes(ctx context.Context, result neo4j.ResultWithContext) ([]GenericNode, error) {
	var nodes []GenericNode
	for result.Next(ctx) {
		node, err := recordToNode(result.Record())
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	if nodes == nil {
		nodes = []GenericNode{}
	}
	return nodes, nil
}
