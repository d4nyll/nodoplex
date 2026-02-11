package graph

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// Repo defines the interface for graph storage operations.
type Repo interface {
	CreateNode(ctx context.Context, label string, props map[string]any) (GenericNode, error)
	GetNode(ctx context.Context, id uuid.UUID) (GenericNode, error)
	ListNodes(ctx context.Context, nodeType *string, status *string, search *string, offset, limit int) ([]GenericNode, int, error)
	UpdateNode(ctx context.Context, id uuid.UUID, props map[string]any) (before GenericNode, after GenericNode, err error)
	DeleteNode(ctx context.Context, id uuid.UUID) (GenericNode, error)
	CreateEdge(ctx context.Context, sourceID, targetID uuid.UUID, edgeType string) error
	DeleteEdge(ctx context.Context, sourceID, targetID uuid.UUID, edgeType string) error
	GetChildren(ctx context.Context, id uuid.UUID) ([]GenericNode, error)
	GetParents(ctx context.Context, id uuid.UUID) ([]GenericNode, error)
	GetSubtree(ctx context.Context, id uuid.UUID) (*SubtreeNode, error)
	GetLineage(ctx context.Context, id uuid.UUID) ([]GenericNode, error)
	GetNodeType(ctx context.Context, id uuid.UUID) (NodeType, error)
}

// Service provides business logic on top of the graph Repository.
type Service struct {
	repo Repo
}

// NewService creates a new graph Service.
func NewService(repo Repo) *Service {
	return &Service{repo: repo}
}

// CreateNode creates a new node after validating its type.
func (s *Service) CreateNode(ctx context.Context, nodeType NodeType, props map[string]any) (GenericNode, error) {
	label, ok := NodeTypeToLabel[nodeType]
	if !ok {
		return GenericNode{}, fmt.Errorf("unknown node type: %s", string(nodeType))
	}

	// Generate UUID if not provided
	if _, ok := props["id"]; !ok {
		props["id"] = uuid.New().String()
	}

	return s.repo.CreateNode(ctx, label, props)
}

// GetNode retrieves a node by ID.
func (s *Service) GetNode(ctx context.Context, id uuid.UUID) (GenericNode, error) {
	return s.repo.GetNode(ctx, id)
}

// ListNodes returns nodes matching the given filters.
func (s *Service) ListNodes(ctx context.Context, nodeType *string, status *string, search *string, offset, limit int) ([]GenericNode, int, error) {
	return s.repo.ListNodes(ctx, nodeType, status, search, offset, limit)
}

// UpdateNode updates a node's properties. Returns both the old and new state.
func (s *Service) UpdateNode(ctx context.Context, id uuid.UUID, props map[string]any) (before GenericNode, after GenericNode, err error) {
	return s.repo.UpdateNode(ctx, id, props)
}

// DeleteNode removes a node and its edges. Returns the deleted node.
func (s *Service) DeleteNode(ctx context.Context, id uuid.UUID) (GenericNode, error) {
	return s.repo.DeleteNode(ctx, id)
}

// CreateEdge creates a relationship between two nodes after validating the
// parent-child type constraint.
func (s *Service) CreateEdge(ctx context.Context, sourceID, targetID uuid.UUID, edgeType EdgeType) error {
	// Validate edge type
	allowedPairs, ok := AllowedEdges[edgeType]
	if !ok {
		return fmt.Errorf("unknown edge type: %s", string(edgeType))
	}

	// Get source and target node types
	sourceType, err := s.repo.GetNodeType(ctx, sourceID)
	if err != nil {
		return fmt.Errorf("source node: %w", err)
	}
	targetType, err := s.repo.GetNodeType(ctx, targetID)
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

	return s.repo.CreateEdge(ctx, sourceID, targetID, string(edgeType))
}

// DeleteEdge removes a relationship between two nodes.
func (s *Service) DeleteEdge(ctx context.Context, sourceID, targetID uuid.UUID, edgeType string) error {
	return s.repo.DeleteEdge(ctx, sourceID, targetID, edgeType)
}

// GetChildren returns the direct children of a node.
func (s *Service) GetChildren(ctx context.Context, id uuid.UUID) ([]GenericNode, error) {
	// Verify node exists
	if _, err := s.repo.GetNode(ctx, id); err != nil {
		return nil, err
	}
	return s.repo.GetChildren(ctx, id)
}

// GetParents returns the direct parents of a node.
func (s *Service) GetParents(ctx context.Context, id uuid.UUID) ([]GenericNode, error) {
	if _, err := s.repo.GetNode(ctx, id); err != nil {
		return nil, err
	}
	return s.repo.GetParents(ctx, id)
}

// GetSubtree returns the full descendant tree of a node.
func (s *Service) GetSubtree(ctx context.Context, id uuid.UUID) (*SubtreeNode, error) {
	return s.repo.GetSubtree(ctx, id)
}

// GetLineage traces ancestors from root to the given node.
func (s *Service) GetLineage(ctx context.Context, id uuid.UUID) ([]GenericNode, error) {
	// Verify node exists
	if _, err := s.repo.GetNode(ctx, id); err != nil {
		return nil, err
	}
	return s.repo.GetLineage(ctx, id)
}
