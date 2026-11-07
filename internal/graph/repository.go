package graph

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
)

// Repository handles all Neo4j CRUD and traversal operations.
//
// Every method takes the namespace of the request and every query matches
// nodes on both their id and that namespace, so a node in another namespace
// is indistinguishable from a node that doesn't exist. Queries match on the
// :Node label so Neo4j can use the (:Node) indexes created by
// db.RunNeo4jMigrations.
type Repository struct {
	driver neo4j.DriverWithContext
}

// NewRepository creates a new graph Repository.
func NewRepository(driver neo4j.DriverWithContext) *Repository {
	return &Repository{driver: driver}
}

// CreateNode creates a node in namespace ns with the given label and properties.
func (r *Repository) CreateNode(ctx context.Context, ns string, label string, props map[string]any) (GenericNode, error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	query := fmt.Sprintf("CREATE (n:Node:%s $props) SET n.namespace = $ns RETURN n, labels(n) AS labels", label)
	result, err := session.Run(ctx, query, map[string]any{"props": props, "ns": ns})
	if err != nil {
		return GenericNode{}, fmt.Errorf("create node: %w", err)
	}

	if result.Next(ctx) {
		return recordToNode(result.Record())
	}
	return GenericNode{}, fmt.Errorf("create node: no record returned")
}

// GetNode retrieves a single node by ID.
func (r *Repository) GetNode(ctx context.Context, ns string, id uuid.UUID) (GenericNode, error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx,
		"MATCH (n:Node {id: $id, namespace: $ns}) RETURN n, labels(n) AS labels",
		map[string]any{"id": id.String(), "ns": ns},
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
func (r *Repository) ListNodes(ctx context.Context, ns string, nodeType *string, status *string, search *string, offset, limit int) ([]GenericNode, int, error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	// Build the MATCH clause. The index hint keeps Neo4j from scanning every
	// node with the type label across all namespaces.
	// ponytail: the type filter runs over all of the namespace's nodes; add a
	// per-type namespace index if one namespace grows large.
	matchClause := "MATCH (n:Node) USING INDEX n:Node(namespace)"
	if nodeType != nil {
		label, ok := NodeTypeToLabel[NodeType(*nodeType)]
		if !ok {
			return nil, 0, fmt.Errorf("unknown node type: %s", *nodeType)
		}
		matchClause = fmt.Sprintf("MATCH (n:Node:%s) USING INDEX n:Node(namespace)", label)
	}

	// Build WHERE conditions
	where := ""
	params := map[string]any{"ns": ns}
	conditions := []string{"n.namespace = $ns"}

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
func (r *Repository) UpdateNode(ctx context.Context, ns string, id uuid.UUID, props map[string]any) (before GenericNode, after GenericNode, err error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	// First, get the current state
	before, err = r.GetNode(ctx, ns, id)
	if err != nil {
		return GenericNode{}, GenericNode{}, err
	}

	// Remove id and namespace from props to prevent overwriting; both are
	// fixed at creation.
	delete(props, "id")
	delete(props, "namespace")

	result, err := session.Run(ctx,
		"MATCH (n:Node {id: $id, namespace: $ns}) SET n += $props RETURN n, labels(n) AS labels",
		map[string]any{"id": id.String(), "ns": ns, "props": props},
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
func (r *Repository) DeleteNode(ctx context.Context, ns string, id uuid.UUID) (GenericNode, error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	// Get current state first
	node, err := r.GetNode(ctx, ns, id)
	if err != nil {
		return GenericNode{}, err
	}

	_, err = session.Run(ctx,
		"MATCH (n:Node {id: $id, namespace: $ns}) DETACH DELETE n",
		map[string]any{"id": id.String(), "ns": ns},
	)
	if err != nil {
		return GenericNode{}, fmt.Errorf("delete node: %w", err)
	}
	return node, nil
}

// CreateEdge creates a relationship between two nodes. Both nodes must be in
// namespace ns, so an edge can never connect two namespaces.
func (r *Repository) CreateEdge(ctx context.Context, ns string, sourceID, targetID uuid.UUID, edgeType string) error {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	query := fmt.Sprintf(
		"MATCH (a:Node {id: $sourceId, namespace: $ns}), (b:Node {id: $targetId, namespace: $ns}) CREATE (a)-[:%s]->(b)",
		edgeType,
	)
	result, err := session.Run(ctx, query, map[string]any{
		"sourceId": sourceID.String(),
		"targetId": targetID.String(),
		"ns":       ns,
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
func (r *Repository) DeleteEdge(ctx context.Context, ns string, sourceID, targetID uuid.UUID, edgeType string) error {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	query := fmt.Sprintf(
		"MATCH (a:Node {id: $sourceId, namespace: $ns})-[r:%s]->(b:Node {id: $targetId, namespace: $ns}) DELETE r",
		edgeType,
	)
	result, err := session.Run(ctx, query, map[string]any{
		"sourceId": sourceID.String(),
		"targetId": targetID.String(),
		"ns":       ns,
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

// CreateDependency creates a DEPENDS_ON edge from source to target, both in
// namespace ns. It returns nil without creating anything if the edge already
// exists, ErrNotFound if either node is missing, and ErrDependencyCycle if
// target already depends on source, directly or through other nodes.
//
// The cycle check is safe under concurrent requests. The check and the create
// run in one transaction that first writes to the namespace's
// (:DependencyLock) node. Neo4j holds a write lock on that node until the
// transaction ends, so dependency creations in one namespace run one at a
// time, and each cycle check reads every DEPENDS_ON edge committed before it.
// Without the lock, two transactions could each add half of a cycle (A->B
// and B->A), each check a graph that lacks the other's uncommitted edge, and
// both commit. Edges never connect two namespaces, so a cycle can only form
// inside one namespace and one lock per namespace is enough.
//
// ponytail: one lock per namespace serializes all dependency creation in it;
// key the lock on namespace and node type if that becomes a bottleneck.
func (r *Repository) CreateDependency(ctx context.Context, ns string, sourceID, targetID uuid.UUID) error {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	params := map[string]any{"ns": ns, "sourceId": sourceID.String(), "targetId": targetID.String()}
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		if _, err := tx.Run(ctx,
			"MERGE (l:DependencyLock {namespace: $ns}) SET l.lockedAt = timestamp()",
			params,
		); err != nil {
			return nil, fmt.Errorf("lock: %w", err)
		}

		result, err := tx.Run(ctx,
			`MATCH (a:Node {id: $sourceId, namespace: $ns}), (b:Node {id: $targetId, namespace: $ns})
			 OPTIONAL MATCH p = shortestPath((b)-[:DEPENDS_ON*]->(a))
			 RETURN EXISTS { (a)-[:DEPENDS_ON]->(b) } AS exists, p IS NOT NULL AS cycle`,
			params,
		)
		if err != nil {
			return nil, fmt.Errorf("check: %w", err)
		}
		if !result.Next(ctx) {
			return nil, ErrNotFound
		}
		exists, _ := result.Record().Get("exists")
		cycle, _ := result.Record().Get("cycle")
		if exists.(bool) {
			return nil, nil
		}
		if cycle.(bool) {
			return nil, ErrDependencyCycle
		}

		result, err = tx.Run(ctx,
			`MATCH (a:Node {id: $sourceId, namespace: $ns}), (b:Node {id: $targetId, namespace: $ns})
			 CREATE (a)-[:DEPENDS_ON]->(b)`,
			params,
		)
		if err != nil {
			return nil, fmt.Errorf("create: %w", err)
		}
		summary, err := result.Consume(ctx)
		if err != nil {
			return nil, fmt.Errorf("create: %w", err)
		}
		// A node deleted since the check (deletes don't take the lock).
		if summary.Counters().RelationshipsCreated() == 0 {
			return nil, ErrNotFound
		}
		return nil, nil
	})
	if err != nil && !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrDependencyCycle) {
		return fmt.Errorf("create dependency: %w", err)
	}
	return err
}

// GetDependencies returns the nodes that a node depends on (targets of its
// outgoing DEPENDS_ON edges).
func (r *Repository) GetDependencies(ctx context.Context, ns string, id uuid.UUID) ([]GenericNode, error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx,
		"MATCH (n:Node {id: $id, namespace: $ns})-[:DEPENDS_ON]->(d:Node {namespace: $ns}) RETURN d AS n, labels(d) AS labels",
		map[string]any{"id": id.String(), "ns": ns},
	)
	if err != nil {
		return nil, fmt.Errorf("get dependencies: %w", err)
	}
	return collectNodes(ctx, result)
}

// GetDependents returns the nodes that depend on a node (sources of its
// incoming DEPENDS_ON edges).
func (r *Repository) GetDependents(ctx context.Context, ns string, id uuid.UUID) ([]GenericNode, error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx,
		"MATCH (d:Node {namespace: $ns})-[:DEPENDS_ON]->(n:Node {id: $id, namespace: $ns}) RETURN d AS n, labels(d) AS labels",
		map[string]any{"id": id.String(), "ns": ns},
	)
	if err != nil {
		return nil, fmt.Errorf("get dependents: %w", err)
	}
	return collectNodes(ctx, result)
}

// GetChildren returns the direct children of a node (via BREAKS_DOWN_TO or IMPLEMENTED_BY).
func (r *Repository) GetChildren(ctx context.Context, ns string, id uuid.UUID) ([]GenericNode, error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx,
		"MATCH (n:Node {id: $id, namespace: $ns})-[:BREAKS_DOWN_TO|IMPLEMENTED_BY]->(child:Node {namespace: $ns}) RETURN child AS n, labels(child) AS labels",
		map[string]any{"id": id.String(), "ns": ns},
	)
	if err != nil {
		return nil, fmt.Errorf("get children: %w", err)
	}
	return collectNodes(ctx, result)
}

// GetParents returns the direct parents of a node.
func (r *Repository) GetParents(ctx context.Context, ns string, id uuid.UUID) ([]GenericNode, error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx,
		"MATCH (parent:Node {namespace: $ns})-[:BREAKS_DOWN_TO|IMPLEMENTED_BY]->(n:Node {id: $id, namespace: $ns}) RETURN parent AS n, labels(parent) AS labels",
		map[string]any{"id": id.String(), "ns": ns},
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
func (r *Repository) GetSubtree(ctx context.Context, ns string, id uuid.UUID) (*SubtreeNode, error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	// Get root node first
	root, err := r.GetNode(ctx, ns, id)
	if err != nil {
		return nil, err
	}

	// Get all descendants with their paths
	result, err := session.Run(ctx,
		`MATCH path = (n:Node {id: $id, namespace: $ns})-[:BREAKS_DOWN_TO|IMPLEMENTED_BY*]->(desc:Node)
		 WHERE all(x IN nodes(path) WHERE x.namespace = $ns)
		 WITH desc, labels(desc) AS labels, length(path) AS depth,
		      [rel IN relationships(path) | startNode(rel).id] AS parentIds
		 RETURN desc AS n, labels, depth, parentIds`,
		map[string]any{"id": id.String(), "ns": ns},
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
func (r *Repository) GetLineage(ctx context.Context, ns string, id uuid.UUID) ([]GenericNode, error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx,
		`MATCH path = (root:Node)-[:BREAKS_DOWN_TO|IMPLEMENTED_BY*]->(n:Node {id: $id, namespace: $ns})
		 WHERE all(x IN nodes(path) WHERE x.namespace = $ns)
		   AND NOT EXISTS { MATCH (p:Node)-[:BREAKS_DOWN_TO|IMPLEMENTED_BY]->(root) WHERE p.namespace = $ns }
		 UNWIND nodes(path) AS node
		 RETURN node AS n, labels(node) AS labels`,
		map[string]any{"id": id.String(), "ns": ns},
	)
	if err != nil {
		return nil, fmt.Errorf("get lineage: %w", err)
	}
	return collectNodes(ctx, result)
}

// GetNodeType returns the NodeType of a node by its ID.
func (r *Repository) GetNodeType(ctx context.Context, ns string, id uuid.UUID) (NodeType, error) {
	session := r.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx,
		"MATCH (n:Node {id: $id, namespace: $ns}) RETURN labels(n) AS labels",
		map[string]any{"id": id.String(), "ns": ns},
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
