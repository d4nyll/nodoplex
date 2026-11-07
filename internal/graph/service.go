package graph

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Repo defines the interface for graph storage operations. Every method is
// scoped to the namespace ns.
type Repo interface {
	CreateNode(ctx context.Context, ns string, label string, props map[string]any) (GenericNode, error)
	GetNode(ctx context.Context, ns string, id uuid.UUID) (GenericNode, error)
	ListNodes(ctx context.Context, ns string, nodeType *string, status *string, search *string, offset, limit int) ([]GenericNode, int, error)
	UpdateNode(ctx context.Context, ns string, id uuid.UUID, props map[string]any) (before GenericNode, after GenericNode, err error)
	DeleteNode(ctx context.Context, ns string, id uuid.UUID) (GenericNode, error)
	CreateEdge(ctx context.Context, ns string, sourceID, targetID uuid.UUID, edgeType string) error
	DeleteEdge(ctx context.Context, ns string, sourceID, targetID uuid.UUID, edgeType string) error
	GetChildren(ctx context.Context, ns string, id uuid.UUID) ([]GenericNode, error)
	GetParents(ctx context.Context, ns string, id uuid.UUID) ([]GenericNode, error)
	GetSubtree(ctx context.Context, ns string, id uuid.UUID) (*SubtreeNode, error)
	GetLineage(ctx context.Context, ns string, id uuid.UUID) ([]GenericNode, error)
	GetNodeType(ctx context.Context, ns string, id uuid.UUID) (NodeType, error)
	CreateDependency(ctx context.Context, ns string, sourceID, targetID uuid.UUID) error
	GetDependencies(ctx context.Context, ns string, id uuid.UUID) ([]GenericNode, error)
	GetDependents(ctx context.Context, ns string, id uuid.UUID) ([]GenericNode, error)
}

// ErrUnknownEdgeType is returned when an edge type is not one of the
// EdgeType constants.
var ErrUnknownEdgeType = errors.New("unknown edge type")

// ErrDependencyCycle is returned when a DEPENDS_ON edge would close a cycle of
// DEPENDS_ON edges.
var ErrDependencyCycle = errors.New("invalid edge: DEPENDS_ON would create a dependency cycle")

// Service provides business logic on top of the graph Repository.
type Service struct {
	repo Repo
}

// NewService creates a new graph Service.
func NewService(repo Repo) *Service {
	return &Service{repo: repo}
}

// CreateNode creates a new node in namespace ns after validating its type.
func (s *Service) CreateNode(ctx context.Context, ns string, nodeType NodeType, props map[string]any) (GenericNode, error) {
	if err := ValidateNamespace(ns); err != nil {
		return GenericNode{}, err
	}
	label, ok := NodeTypeToLabel[nodeType]
	if !ok {
		return GenericNode{}, fmt.Errorf("unknown node type: %s", string(nodeType))
	}

	// Generate UUID if not provided
	if _, ok := props["id"]; !ok {
		props["id"] = uuid.New().String()
	}

	return s.repo.CreateNode(ctx, ns, label, props)
}

// GetNode retrieves a node by ID.
func (s *Service) GetNode(ctx context.Context, ns string, id uuid.UUID) (GenericNode, error) {
	return s.repo.GetNode(ctx, ns, id)
}

// ListNodes returns nodes matching the given filters.
func (s *Service) ListNodes(ctx context.Context, ns string, nodeType *string, status *string, search *string, offset, limit int) ([]GenericNode, int, error) {
	return s.repo.ListNodes(ctx, ns, nodeType, status, search, offset, limit)
}

// UpdateNode updates a node's properties. Returns both the old and new state.
func (s *Service) UpdateNode(ctx context.Context, ns string, id uuid.UUID, props map[string]any) (before GenericNode, after GenericNode, err error) {
	return s.repo.UpdateNode(ctx, ns, id, props)
}

// DeleteNode removes a node and its edges. Returns the deleted node.
func (s *Service) DeleteNode(ctx context.Context, ns string, id uuid.UUID) (GenericNode, error) {
	return s.repo.DeleteNode(ctx, ns, id)
}

// CreateEdge creates a relationship between two nodes after validating the
// source -> target type constraint. A DEPENDS_ON edge must also not point at
// its own source or close a cycle; creating one that already exists is a
// no-op.
func (s *Service) CreateEdge(ctx context.Context, ns string, sourceID, targetID uuid.UUID, edgeType EdgeType) error {
	// Validate edge type
	allowedPairs, ok := AllowedEdges[edgeType]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownEdgeType, string(edgeType))
	}
	if edgeType == EdgeTypeDependsOn && sourceID == targetID {
		return errors.New("invalid edge: a node can't depend on itself")
	}

	// Get source and target node types
	sourceType, err := s.repo.GetNodeType(ctx, ns, sourceID)
	if err != nil {
		return fmt.Errorf("source node: %w", err)
	}
	targetType, err := s.repo.GetNodeType(ctx, ns, targetID)
	if err != nil {
		return fmt.Errorf("target node: %w", err)
	}

	// Validate the source -> target type pair
	expectedTarget, ok := allowedPairs[sourceType]
	if !ok {
		return fmt.Errorf(
			"invalid edge: %s does not allow source type %s",
			string(edgeType), string(sourceType),
		)
	}
	if expectedTarget != targetType {
		return fmt.Errorf(
			"invalid edge: %s from %s to %s (expected target type %s)",
			string(edgeType), string(sourceType), string(targetType),
			string(expectedTarget),
		)
	}

	if edgeType == EdgeTypeDependsOn {
		return s.repo.CreateDependency(ctx, ns, sourceID, targetID)
	}
	return s.repo.CreateEdge(ctx, ns, sourceID, targetID, string(edgeType))
}

// DeleteEdge removes a relationship between two nodes. The edge type is
// checked against AllowedEdges because the repository interpolates it into
// the Cypher query.
func (s *Service) DeleteEdge(ctx context.Context, ns string, sourceID, targetID uuid.UUID, edgeType EdgeType) error {
	if _, ok := AllowedEdges[edgeType]; !ok {
		return fmt.Errorf("%w: %s", ErrUnknownEdgeType, string(edgeType))
	}
	return s.repo.DeleteEdge(ctx, ns, sourceID, targetID, string(edgeType))
}

// GetChildren returns the direct children of a node.
func (s *Service) GetChildren(ctx context.Context, ns string, id uuid.UUID) ([]GenericNode, error) {
	// Verify node exists
	if _, err := s.repo.GetNode(ctx, ns, id); err != nil {
		return nil, err
	}
	return s.repo.GetChildren(ctx, ns, id)
}

// GetParents returns the direct parents of a node.
func (s *Service) GetParents(ctx context.Context, ns string, id uuid.UUID) ([]GenericNode, error) {
	if _, err := s.repo.GetNode(ctx, ns, id); err != nil {
		return nil, err
	}
	return s.repo.GetParents(ctx, ns, id)
}

// GetDependencies returns the nodes that a node depends on.
func (s *Service) GetDependencies(ctx context.Context, ns string, id uuid.UUID) ([]GenericNode, error) {
	if _, err := s.repo.GetNode(ctx, ns, id); err != nil {
		return nil, err
	}
	return s.repo.GetDependencies(ctx, ns, id)
}

// GetDependents returns the nodes that depend on a node.
func (s *Service) GetDependents(ctx context.Context, ns string, id uuid.UUID) ([]GenericNode, error) {
	if _, err := s.repo.GetNode(ctx, ns, id); err != nil {
		return nil, err
	}
	return s.repo.GetDependents(ctx, ns, id)
}

// GetSubtree returns the full descendant tree of a node.
func (s *Service) GetSubtree(ctx context.Context, ns string, id uuid.UUID) (*SubtreeNode, error) {
	return s.repo.GetSubtree(ctx, ns, id)
}

// GetLineage traces ancestors from root to the given node.
func (s *Service) GetLineage(ctx context.Context, ns string, id uuid.UUID) ([]GenericNode, error) {
	// Verify node exists
	if _, err := s.repo.GetNode(ctx, ns, id); err != nil {
		return nil, err
	}
	return s.repo.GetLineage(ctx, ns, id)
}
