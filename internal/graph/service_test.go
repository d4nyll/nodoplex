package graph

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const testNS = "ns-test"

// mockRepo implements the Repo interface for testing.
type mockRepo struct {
	createNodeFn       func(ctx context.Context, label string, props map[string]any) (GenericNode, error)
	getNodeFn          func(ctx context.Context, id uuid.UUID) (GenericNode, error)
	listNodesFn        func(ctx context.Context, nodeType *string, status *string, search *string, offset, limit int) ([]GenericNode, int, error)
	updateNodeFn       func(ctx context.Context, id uuid.UUID, props map[string]any) (GenericNode, GenericNode, error)
	deleteNodeFn       func(ctx context.Context, id uuid.UUID) (GenericNode, error)
	createEdgeFn       func(ctx context.Context, sourceID, targetID uuid.UUID, edgeType string) error
	deleteEdgeFn       func(ctx context.Context, sourceID, targetID uuid.UUID, edgeType string) error
	getChildrenFn      func(ctx context.Context, id uuid.UUID) ([]GenericNode, error)
	getParentsFn       func(ctx context.Context, id uuid.UUID) ([]GenericNode, error)
	getSubtreeFn       func(ctx context.Context, id uuid.UUID) (*SubtreeNode, error)
	getLineageFn       func(ctx context.Context, id uuid.UUID) ([]GenericNode, error)
	getNodeTypeFn      func(ctx context.Context, id uuid.UUID) (NodeType, error)
	createDependencyFn func(ctx context.Context, sourceID, targetID uuid.UUID) error
	getDependenciesFn  func(ctx context.Context, id uuid.UUID) ([]GenericNode, error)
	getDependentsFn    func(ctx context.Context, id uuid.UUID) ([]GenericNode, error)

	namespaces []string // namespace passed to each call, in order
}

func (m *mockRepo) CreateNode(ctx context.Context, ns string, label string, props map[string]any) (GenericNode, error) {
	m.namespaces = append(m.namespaces, ns)
	if m.createNodeFn != nil {
		return m.createNodeFn(ctx, label, props)
	}
	return GenericNode{}, nil
}

func (m *mockRepo) GetNode(ctx context.Context, ns string, id uuid.UUID) (GenericNode, error) {
	m.namespaces = append(m.namespaces, ns)
	if m.getNodeFn != nil {
		return m.getNodeFn(ctx, id)
	}
	return GenericNode{}, nil
}

func (m *mockRepo) ListNodes(ctx context.Context, ns string, nodeType *string, status *string, search *string, offset, limit int) ([]GenericNode, int, error) {
	m.namespaces = append(m.namespaces, ns)
	if m.listNodesFn != nil {
		return m.listNodesFn(ctx, nodeType, status, search, offset, limit)
	}
	return nil, 0, nil
}

func (m *mockRepo) UpdateNode(ctx context.Context, ns string, id uuid.UUID, props map[string]any) (GenericNode, GenericNode, error) {
	m.namespaces = append(m.namespaces, ns)
	if m.updateNodeFn != nil {
		return m.updateNodeFn(ctx, id, props)
	}
	return GenericNode{}, GenericNode{}, nil
}

func (m *mockRepo) DeleteNode(ctx context.Context, ns string, id uuid.UUID) (GenericNode, error) {
	m.namespaces = append(m.namespaces, ns)
	if m.deleteNodeFn != nil {
		return m.deleteNodeFn(ctx, id)
	}
	return GenericNode{}, nil
}

func (m *mockRepo) CreateEdge(ctx context.Context, ns string, sourceID, targetID uuid.UUID, edgeType string) error {
	m.namespaces = append(m.namespaces, ns)
	if m.createEdgeFn != nil {
		return m.createEdgeFn(ctx, sourceID, targetID, edgeType)
	}
	return nil
}

func (m *mockRepo) DeleteEdge(ctx context.Context, ns string, sourceID, targetID uuid.UUID, edgeType string) error {
	m.namespaces = append(m.namespaces, ns)
	if m.deleteEdgeFn != nil {
		return m.deleteEdgeFn(ctx, sourceID, targetID, edgeType)
	}
	return nil
}

func (m *mockRepo) GetChildren(ctx context.Context, ns string, id uuid.UUID) ([]GenericNode, error) {
	m.namespaces = append(m.namespaces, ns)
	if m.getChildrenFn != nil {
		return m.getChildrenFn(ctx, id)
	}
	return nil, nil
}

func (m *mockRepo) GetParents(ctx context.Context, ns string, id uuid.UUID) ([]GenericNode, error) {
	m.namespaces = append(m.namespaces, ns)
	if m.getParentsFn != nil {
		return m.getParentsFn(ctx, id)
	}
	return nil, nil
}

func (m *mockRepo) GetSubtree(ctx context.Context, ns string, id uuid.UUID) (*SubtreeNode, error) {
	m.namespaces = append(m.namespaces, ns)
	if m.getSubtreeFn != nil {
		return m.getSubtreeFn(ctx, id)
	}
	return nil, nil
}

func (m *mockRepo) GetLineage(ctx context.Context, ns string, id uuid.UUID) ([]GenericNode, error) {
	m.namespaces = append(m.namespaces, ns)
	if m.getLineageFn != nil {
		return m.getLineageFn(ctx, id)
	}
	return nil, nil
}

func (m *mockRepo) GetNodeType(ctx context.Context, ns string, id uuid.UUID) (NodeType, error) {
	m.namespaces = append(m.namespaces, ns)
	if m.getNodeTypeFn != nil {
		return m.getNodeTypeFn(ctx, id)
	}
	return "", nil
}

func (m *mockRepo) CreateDependency(ctx context.Context, ns string, sourceID, targetID uuid.UUID) error {
	m.namespaces = append(m.namespaces, ns)
	if m.createDependencyFn != nil {
		return m.createDependencyFn(ctx, sourceID, targetID)
	}
	return nil
}

func (m *mockRepo) GetDependencies(ctx context.Context, ns string, id uuid.UUID) ([]GenericNode, error) {
	m.namespaces = append(m.namespaces, ns)
	if m.getDependenciesFn != nil {
		return m.getDependenciesFn(ctx, id)
	}
	return []GenericNode{}, nil
}

func (m *mockRepo) GetDependents(ctx context.Context, ns string, id uuid.UUID) ([]GenericNode, error) {
	m.namespaces = append(m.namespaces, ns)
	if m.getDependentsFn != nil {
		return m.getDependentsFn(ctx, id)
	}
	return []GenericNode{}, nil
}

// ── CreateNode Tests ─────────────────────────────────────────────────────────

func TestService_CreateNode_Success(t *testing.T) {
	id := uuid.New()
	repo := &mockRepo{
		createNodeFn: func(ctx context.Context, label string, props map[string]any) (GenericNode, error) {
			if label != "Vision" {
				t.Errorf("label = %q, want %q", label, "Vision")
			}
			return GenericNode{
				ID:    id,
				Type:  NodeTypeVision,
				Title: strPtr("Test Vision"),
			}, nil
		},
	}

	svc := NewService(repo)
	node, err := svc.CreateNode(context.Background(), testNS, NodeTypeVision, map[string]any{"title": "Test Vision"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.ID != id {
		t.Errorf("ID = %v, want %v", node.ID, id)
	}
}

func TestService_CreateNode_GeneratesUUID(t *testing.T) {
	var capturedProps map[string]any
	repo := &mockRepo{
		createNodeFn: func(ctx context.Context, label string, props map[string]any) (GenericNode, error) {
			capturedProps = props
			return GenericNode{}, nil
		},
	}

	svc := NewService(repo)
	props := map[string]any{"title": "No ID"}
	_, err := svc.CreateNode(context.Background(), testNS, NodeTypeGoal, props)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := capturedProps["id"]; !ok {
		t.Error("expected UUID to be generated in props")
	}
}

func TestService_CreateNode_PreservesExistingID(t *testing.T) {
	existingID := uuid.New().String()
	var capturedProps map[string]any
	repo := &mockRepo{
		createNodeFn: func(ctx context.Context, label string, props map[string]any) (GenericNode, error) {
			capturedProps = props
			return GenericNode{}, nil
		},
	}

	svc := NewService(repo)
	props := map[string]any{"id": existingID, "title": "With ID"}
	_, err := svc.CreateNode(context.Background(), testNS, NodeTypeGoal, props)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedProps["id"] != existingID {
		t.Errorf("id = %v, want %v", capturedProps["id"], existingID)
	}
}

func TestService_CreateNode_UnknownType(t *testing.T) {
	svc := NewService(&mockRepo{})
	_, err := svc.CreateNode(context.Background(), testNS, NodeType("unknown"), map[string]any{})
	if err == nil {
		t.Fatal("expected error for unknown node type")
	}
	if got := err.Error(); got != "unknown node type: unknown" {
		t.Errorf("error = %q, want %q", got, "unknown node type: unknown")
	}
}

// ── CreateEdge Tests ─────────────────────────────────────────────────────────

func TestService_CreateEdge_ValidBreaksDownTo(t *testing.T) {
	visionID := uuid.New()
	goalID := uuid.New()

	var capturedEdgeType string
	repo := &mockRepo{
		getNodeTypeFn: func(ctx context.Context, id uuid.UUID) (NodeType, error) {
			if id == visionID {
				return NodeTypeVision, nil
			}
			if id == goalID {
				return NodeTypeGoal, nil
			}
			return "", ErrNotFound
		},
		createEdgeFn: func(ctx context.Context, sourceID, targetID uuid.UUID, edgeType string) error {
			capturedEdgeType = edgeType
			return nil
		},
	}

	svc := NewService(repo)
	err := svc.CreateEdge(context.Background(), testNS, visionID, goalID, EdgeTypeBreaksDownTo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedEdgeType != "BREAKS_DOWN_TO" {
		t.Errorf("edgeType = %q, want %q", capturedEdgeType, "BREAKS_DOWN_TO")
	}
}

func TestService_CreateEdge_ValidImplementedBy(t *testing.T) {
	taskID := uuid.New()
	gitRefID := uuid.New()

	repo := &mockRepo{
		getNodeTypeFn: func(ctx context.Context, id uuid.UUID) (NodeType, error) {
			if id == taskID {
				return NodeTypeTask, nil
			}
			if id == gitRefID {
				return NodeTypeGitRef, nil
			}
			return "", ErrNotFound
		},
		createEdgeFn: func(ctx context.Context, sourceID, targetID uuid.UUID, edgeType string) error {
			return nil
		},
	}

	svc := NewService(repo)
	err := svc.CreateEdge(context.Background(), testNS, taskID, gitRefID, EdgeTypeImplementedBy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestService_CreateEdge_UnknownEdgeType(t *testing.T) {
	svc := NewService(&mockRepo{})
	err := svc.CreateEdge(context.Background(), testNS, uuid.New(), uuid.New(), EdgeType("UNKNOWN"))
	if err == nil {
		t.Fatal("expected error for unknown edge type")
	}
	if got := err.Error(); got != "unknown edge type: UNKNOWN" {
		t.Errorf("error = %q", got)
	}
}

func TestService_CreateEdge_InvalidSourceType(t *testing.T) {
	// GitRef -> Goal via BREAKS_DOWN_TO is not allowed
	gitRefID := uuid.New()
	goalID := uuid.New()

	repo := &mockRepo{
		getNodeTypeFn: func(ctx context.Context, id uuid.UUID) (NodeType, error) {
			if id == gitRefID {
				return NodeTypeGitRef, nil
			}
			return NodeTypeGoal, nil
		},
	}

	svc := NewService(repo)
	err := svc.CreateEdge(context.Background(), testNS, gitRefID, goalID, EdgeTypeBreaksDownTo)
	if err == nil {
		t.Fatal("expected error for invalid source type")
	}
	// Error should mention the edge type doesn't allow the source type
	if got := err.Error(); got == "" {
		t.Error("expected non-empty error message")
	}
}

func TestService_CreateEdge_InvalidTargetType(t *testing.T) {
	// Vision -> Feature via BREAKS_DOWN_TO is not valid (Vision -> Goal is expected)
	visionID := uuid.New()
	featureID := uuid.New()

	repo := &mockRepo{
		getNodeTypeFn: func(ctx context.Context, id uuid.UUID) (NodeType, error) {
			if id == visionID {
				return NodeTypeVision, nil
			}
			return NodeTypeFeature, nil
		},
	}

	svc := NewService(repo)
	err := svc.CreateEdge(context.Background(), testNS, visionID, featureID, EdgeTypeBreaksDownTo)
	if err == nil {
		t.Fatal("expected error for invalid target type")
	}
	// Error should mention expected target type
	if got := err.Error(); got == "" {
		t.Error("expected non-empty error message")
	}
}

func TestService_CreateEdge_SourceNotFound(t *testing.T) {
	repo := &mockRepo{
		getNodeTypeFn: func(ctx context.Context, id uuid.UUID) (NodeType, error) {
			return "", ErrNotFound
		},
	}

	svc := NewService(repo)
	err := svc.CreateEdge(context.Background(), testNS, uuid.New(), uuid.New(), EdgeTypeBreaksDownTo)
	if err == nil {
		t.Fatal("expected error when source not found")
	}
}

// ── CreateEdge Exhaustive Type Pairs ─────────────────────────────────────────

func TestService_CreateEdge_AllValidPairs(t *testing.T) {
	validPairs := []struct {
		edgeType   EdgeType
		sourceType NodeType
		targetType NodeType
	}{
		{EdgeTypeBreaksDownTo, NodeTypeVision, NodeTypeGoal},
		{EdgeTypeBreaksDownTo, NodeTypeGoal, NodeTypeFeature},
		{EdgeTypeBreaksDownTo, NodeTypeFeature, NodeTypeRequirement},
		{EdgeTypeBreaksDownTo, NodeTypeRequirement, NodeTypeTask},
		{EdgeTypeImplementedBy, NodeTypeTask, NodeTypeGitRef},
	}

	for _, tt := range validPairs {
		t.Run(string(tt.edgeType)+"_"+string(tt.sourceType)+"_"+string(tt.targetType), func(t *testing.T) {
			sourceID := uuid.New()
			targetID := uuid.New()

			repo := &mockRepo{
				getNodeTypeFn: func(ctx context.Context, id uuid.UUID) (NodeType, error) {
					if id == sourceID {
						return tt.sourceType, nil
					}
					return tt.targetType, nil
				},
				createEdgeFn: func(ctx context.Context, sID, tID uuid.UUID, edgeType string) error {
					return nil
				},
			}

			svc := NewService(repo)
			err := svc.CreateEdge(context.Background(), testNS, sourceID, targetID, tt.edgeType)
			if err != nil {
				t.Errorf("unexpected error for valid pair: %v", err)
			}
		})
	}
}

func TestService_CreateEdge_InvalidPairs(t *testing.T) {
	invalidPairs := []struct {
		name       string
		edgeType   EdgeType
		sourceType NodeType
		targetType NodeType
	}{
		{"Vision->Feature via BDT", EdgeTypeBreaksDownTo, NodeTypeVision, NodeTypeFeature},
		{"Goal->Task via BDT", EdgeTypeBreaksDownTo, NodeTypeGoal, NodeTypeTask},
		{"Task->Goal via BDT", EdgeTypeBreaksDownTo, NodeTypeTask, NodeTypeGoal},
		{"Vision->GitRef via IB", EdgeTypeImplementedBy, NodeTypeVision, NodeTypeGitRef},
		{"Goal->GitRef via IB", EdgeTypeImplementedBy, NodeTypeGoal, NodeTypeGitRef},
		{"GitRef->Task via IB", EdgeTypeImplementedBy, NodeTypeGitRef, NodeTypeTask},
	}

	for _, tt := range invalidPairs {
		t.Run(tt.name, func(t *testing.T) {
			sourceID := uuid.New()
			targetID := uuid.New()

			repo := &mockRepo{
				getNodeTypeFn: func(ctx context.Context, id uuid.UUID) (NodeType, error) {
					if id == sourceID {
						return tt.sourceType, nil
					}
					return tt.targetType, nil
				},
			}

			svc := NewService(repo)
			err := svc.CreateEdge(context.Background(), testNS, sourceID, targetID, tt.edgeType)
			if err == nil {
				t.Error("expected error for invalid pair")
			}
		})
	}
}

// ── GetChildren / GetParents Tests ───────────────────────────────────────────

func TestService_GetChildren_NodeNotFound(t *testing.T) {
	repo := &mockRepo{
		getNodeFn: func(ctx context.Context, id uuid.UUID) (GenericNode, error) {
			return GenericNode{}, ErrNotFound
		},
	}

	svc := NewService(repo)
	_, err := svc.GetChildren(context.Background(), testNS, uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestService_GetChildren_Success(t *testing.T) {
	parentID := uuid.New()
	childNode := GenericNode{ID: uuid.New(), Type: NodeTypeGoal, Title: strPtr("Child")}

	repo := &mockRepo{
		getNodeFn: func(ctx context.Context, id uuid.UUID) (GenericNode, error) {
			return GenericNode{ID: parentID, Type: NodeTypeVision}, nil
		},
		getChildrenFn: func(ctx context.Context, id uuid.UUID) ([]GenericNode, error) {
			return []GenericNode{childNode}, nil
		},
	}

	svc := NewService(repo)
	children, err := svc.GetChildren(context.Background(), testNS, parentID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(children))
	}
	if children[0].ID != childNode.ID {
		t.Errorf("child ID = %v, want %v", children[0].ID, childNode.ID)
	}
}

func TestService_GetParents_NodeNotFound(t *testing.T) {
	repo := &mockRepo{
		getNodeFn: func(ctx context.Context, id uuid.UUID) (GenericNode, error) {
			return GenericNode{}, ErrNotFound
		},
	}

	svc := NewService(repo)
	_, err := svc.GetParents(context.Background(), testNS, uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestService_GetLineage_NodeNotFound(t *testing.T) {
	repo := &mockRepo{
		getNodeFn: func(ctx context.Context, id uuid.UUID) (GenericNode, error) {
			return GenericNode{}, ErrNotFound
		},
	}

	svc := NewService(repo)
	_, err := svc.GetLineage(context.Background(), testNS, uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// ── Delegation Tests ─────────────────────────────────────────────────────────

func TestService_GetNode_Delegates(t *testing.T) {
	id := uuid.New()
	expected := GenericNode{ID: id, Type: NodeTypeFeature, Title: strPtr("F")}
	repo := &mockRepo{
		getNodeFn: func(ctx context.Context, rid uuid.UUID) (GenericNode, error) {
			if rid != id {
				t.Errorf("called with wrong ID: %v", rid)
			}
			return expected, nil
		},
	}

	svc := NewService(repo)
	node, err := svc.GetNode(context.Background(), testNS, id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.ID != expected.ID {
		t.Errorf("ID = %v, want %v", node.ID, expected.ID)
	}
}

func TestService_DeleteNode_Delegates(t *testing.T) {
	id := uuid.New()
	expected := GenericNode{ID: id, Type: NodeTypeTask, Title: strPtr("Deleted")}
	repo := &mockRepo{
		deleteNodeFn: func(ctx context.Context, rid uuid.UUID) (GenericNode, error) {
			return expected, nil
		},
	}

	svc := NewService(repo)
	node, err := svc.DeleteNode(context.Background(), testNS, id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.ID != expected.ID {
		t.Errorf("ID = %v, want %v", node.ID, expected.ID)
	}
}

func TestService_UpdateNode_Delegates(t *testing.T) {
	id := uuid.New()
	beforeNode := GenericNode{ID: id, Type: NodeTypeGoal, Title: strPtr("Before")}
	afterNode := GenericNode{ID: id, Type: NodeTypeGoal, Title: strPtr("After")}

	repo := &mockRepo{
		updateNodeFn: func(ctx context.Context, rid uuid.UUID, props map[string]any) (GenericNode, GenericNode, error) {
			return beforeNode, afterNode, nil
		},
	}

	svc := NewService(repo)
	before, after, err := svc.UpdateNode(context.Background(), testNS, id, map[string]any{"title": "After"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *before.Title != "Before" {
		t.Errorf("before Title = %q, want %q", *before.Title, "Before")
	}
	if *after.Title != "After" {
		t.Errorf("after Title = %q, want %q", *after.Title, "After")
	}
}

func TestService_DeleteEdge_Delegates(t *testing.T) {
	sourceID := uuid.New()
	targetID := uuid.New()

	called := false
	repo := &mockRepo{
		deleteEdgeFn: func(ctx context.Context, sID, tID uuid.UUID, edgeType string) error {
			called = true
			if sID != sourceID || tID != targetID {
				t.Error("wrong IDs passed")
			}
			if edgeType != "BREAKS_DOWN_TO" {
				t.Errorf("edgeType = %q, want %q", edgeType, "BREAKS_DOWN_TO")
			}
			return nil
		},
	}

	svc := NewService(repo)
	err := svc.DeleteEdge(context.Background(), testNS, sourceID, targetID, EdgeTypeBreaksDownTo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("DeleteEdge was not called on repo")
	}
}

func TestService_GetSubtree_Delegates(t *testing.T) {
	id := uuid.New()
	expected := &SubtreeNode{
		Node:     GenericNode{ID: id, Type: NodeTypeVision},
		Children: []SubtreeNode{},
	}

	repo := &mockRepo{
		getSubtreeFn: func(ctx context.Context, rid uuid.UUID) (*SubtreeNode, error) {
			return expected, nil
		},
	}

	svc := NewService(repo)
	result, err := svc.GetSubtree(context.Background(), testNS, id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Node.ID != expected.Node.ID {
		t.Errorf("ID = %v, want %v", result.Node.ID, expected.Node.ID)
	}
}

func TestService_ListNodes_Delegates(t *testing.T) {
	nodeType := "vision"
	repo := &mockRepo{
		listNodesFn: func(ctx context.Context, nt *string, status *string, search *string, offset, limit int) ([]GenericNode, int, error) {
			if nt == nil || *nt != "vision" {
				t.Errorf("nodeType = %v, want %q", nt, "vision")
			}
			if offset != 10 || limit != 20 {
				t.Errorf("offset=%d limit=%d, want 10,20", offset, limit)
			}
			return []GenericNode{
				{ID: uuid.New(), Type: NodeTypeVision, Title: strPtr("V1")},
			}, 1, nil
		},
	}

	svc := NewService(repo)
	nodes, total, err := svc.ListNodes(context.Background(), testNS, &nodeType, nil, nil, 10, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(nodes) != 1 {
		t.Errorf("len(nodes) = %d, want 1", len(nodes))
	}
}

// ── Namespace Tests ──────────────────────────────────────────────────────────

func TestValidateNamespace(t *testing.T) {
	tests := []struct {
		name  string
		ns    string
		valid bool
	}{
		{"simple", "org_123", true},
		{"uuid", "0b6f7c1e-3a0e-4b8e-9a51-6d3c2d7c1f00", true},
		{"all allowed characters", "Org-1_team:prod.eu", true},
		{"single character", "a", true},
		{"max length", strings.Repeat("a", MaxNamespaceLength), true},
		{"dots inside", "a..b", true},
		{"empty", "", false},
		{"too long", strings.Repeat("a", MaxNamespaceLength+1), false},
		{"slash", "a/b", false},
		{"space", "a b", false},
		{"at sign", "a@b", false},
		{"percent", "a%2Fb", false},
		{"unicode", "größe", false},
		{"invalid utf-8", "org\xff", false},
		{"nul", "org\x00", false},
		{"newline", "org\nfake log line", false},
		{"dot", ".", false},
		{"dot dot", "..", false},
		{"leading dot", ".a", false},
		{"trailing dot", "a.", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNamespace(tt.ns)
			if tt.valid && err != nil {
				t.Errorf("ValidateNamespace(%q) = %v, want nil", tt.ns, err)
			}
			if !tt.valid && !errors.Is(err, ErrInvalidNamespace) {
				t.Errorf("ValidateNamespace(%q) = %v, want ErrInvalidNamespace", tt.ns, err)
			}
		})
	}
}

func TestService_CreateNode_InvalidNamespace(t *testing.T) {
	repo := &mockRepo{}
	svc := NewService(repo)
	_, err := svc.CreateNode(context.Background(), "", NodeTypeVision, map[string]any{"title": "x"})
	if !errors.Is(err, ErrInvalidNamespace) {
		t.Fatalf("err = %v, want ErrInvalidNamespace", err)
	}
	if len(repo.namespaces) != 0 {
		t.Errorf("repo was called %d times, want 0", len(repo.namespaces))
	}
}

func TestService_PassesNamespaceToEveryRepoCall(t *testing.T) {
	// CreateEdge looks up the source (vision) then the target (goal).
	calls := 0
	repo := &mockRepo{
		getNodeTypeFn: func(ctx context.Context, id uuid.UUID) (NodeType, error) {
			calls++
			if calls == 2 {
				return NodeTypeGoal, nil
			}
			return NodeTypeVision, nil
		},
	}
	svc := NewService(repo)
	ctx := context.Background()
	id := uuid.New()

	_, _ = svc.CreateNode(ctx, testNS, NodeTypeVision, map[string]any{})
	_, _ = svc.GetNode(ctx, testNS, id)
	_, _, _ = svc.ListNodes(ctx, testNS, nil, nil, nil, 0, 10)
	_, _, _ = svc.UpdateNode(ctx, testNS, id, map[string]any{"title": "x"})
	_, _ = svc.DeleteNode(ctx, testNS, id)
	_ = svc.CreateEdge(ctx, testNS, id, uuid.New(), EdgeTypeBreaksDownTo)
	_ = svc.DeleteEdge(ctx, testNS, id, uuid.New(), EdgeTypeBreaksDownTo)
	_, _ = svc.GetChildren(ctx, testNS, id)
	_, _ = svc.GetParents(ctx, testNS, id)
	_, _ = svc.GetSubtree(ctx, testNS, id)
	_, _ = svc.GetLineage(ctx, testNS, id)
	calls = 10 // both type lookups below return vision
	_ = svc.CreateEdge(ctx, testNS, id, uuid.New(), EdgeTypeDependsOn)
	_, _ = svc.GetDependencies(ctx, testNS, id)
	_, _ = svc.GetDependents(ctx, testNS, id)

	// Each CreateEdge makes 3 calls; GetChildren, GetParents, GetLineage,
	// GetDependencies and GetDependents make 2.
	if len(repo.namespaces) != 23 {
		t.Errorf("repo calls = %d, want 23", len(repo.namespaces))
	}
	for i, ns := range repo.namespaces {
		if ns != testNS {
			t.Errorf("call %d namespace = %q, want %q", i, ns, testNS)
		}
	}
}

func TestService_DeleteEdge_UnknownEdgeType(t *testing.T) {
	repo := &mockRepo{}
	svc := NewService(repo)
	err := svc.DeleteEdge(context.Background(), testNS, uuid.New(), uuid.New(), EdgeType("X]->() DETACH DELETE b //"))
	if !errors.Is(err, ErrUnknownEdgeType) {
		t.Fatalf("err = %v, want ErrUnknownEdgeType", err)
	}
	if len(repo.namespaces) != 0 {
		t.Errorf("repo was called %d times, want 0", len(repo.namespaces))
	}
}

// ── Dependency Tests ─────────────────────────────────────────────────────────

func TestService_CreateEdge_DependsOn(t *testing.T) {
	sourceID, targetID := uuid.New(), uuid.New()
	var gotSource, gotTarget uuid.UUID
	repo := &mockRepo{
		getNodeTypeFn: func(ctx context.Context, id uuid.UUID) (NodeType, error) {
			return NodeTypeRequirement, nil
		},
		createEdgeFn: func(ctx context.Context, sourceID, targetID uuid.UUID, edgeType string) error {
			t.Error("DEPENDS_ON went through CreateEdge, want CreateDependency")
			return nil
		},
		createDependencyFn: func(ctx context.Context, s, tg uuid.UUID) error {
			gotSource, gotTarget = s, tg
			return nil
		},
	}
	svc := NewService(repo)
	if err := svc.CreateEdge(context.Background(), testNS, sourceID, targetID, EdgeTypeDependsOn); err != nil {
		t.Fatalf("CreateEdge: %v", err)
	}
	if gotSource != sourceID || gotTarget != targetID {
		t.Errorf("CreateDependency(%s, %s), want (%s, %s)", gotSource, gotTarget, sourceID, targetID)
	}
}

func TestService_CreateEdge_DependsOnDifferentTypes(t *testing.T) {
	calls := 0
	repo := &mockRepo{
		getNodeTypeFn: func(ctx context.Context, id uuid.UUID) (NodeType, error) {
			calls++
			if calls == 1 {
				return NodeTypeRequirement, nil
			}
			return NodeTypeTask, nil
		},
		createDependencyFn: func(ctx context.Context, s, tg uuid.UUID) error {
			t.Error("CreateDependency called for nodes of different types")
			return nil
		},
	}
	svc := NewService(repo)
	err := svc.CreateEdge(context.Background(), testNS, uuid.New(), uuid.New(), EdgeTypeDependsOn)
	if err == nil || !strings.Contains(err.Error(), "expected target type requirement") {
		t.Errorf("err = %v, want a type mismatch error", err)
	}
}

func TestService_CreateEdge_DependsOnSelf(t *testing.T) {
	repo := &mockRepo{}
	svc := NewService(repo)
	id := uuid.New()
	err := svc.CreateEdge(context.Background(), testNS, id, id, EdgeTypeDependsOn)
	if err == nil || !strings.Contains(err.Error(), "can't depend on itself") {
		t.Errorf("err = %v, want a self-dependency error", err)
	}
	if len(repo.namespaces) != 0 {
		t.Errorf("repo was called %d times, want 0", len(repo.namespaces))
	}
}

func TestService_CreateEdge_DependsOnPropagatesRepoErrors(t *testing.T) {
	for _, want := range []error{ErrDependencyCycle, ErrNotFound} {
		repo := &mockRepo{
			getNodeTypeFn: func(ctx context.Context, id uuid.UUID) (NodeType, error) {
				return NodeTypeTask, nil
			},
			createDependencyFn: func(ctx context.Context, s, tg uuid.UUID) error { return want },
		}
		err := NewService(repo).CreateEdge(context.Background(), testNS, uuid.New(), uuid.New(), EdgeTypeDependsOn)
		if !errors.Is(err, want) {
			t.Errorf("err = %v, want %v", err, want)
		}
	}
}

func TestService_GetDependenciesAndDependents(t *testing.T) {
	dep := GenericNode{ID: uuid.New(), Type: NodeTypeTask}
	repo := &mockRepo{
		getDependenciesFn: func(ctx context.Context, id uuid.UUID) ([]GenericNode, error) { return []GenericNode{dep}, nil },
		getDependentsFn:   func(ctx context.Context, id uuid.UUID) ([]GenericNode, error) { return []GenericNode{dep, dep}, nil },
	}
	svc := NewService(repo)
	deps, err := svc.GetDependencies(context.Background(), testNS, uuid.New())
	if err != nil || len(deps) != 1 {
		t.Errorf("GetDependencies = %v, %v; want 1 node", deps, err)
	}
	dependents, err := svc.GetDependents(context.Background(), testNS, uuid.New())
	if err != nil || len(dependents) != 2 {
		t.Errorf("GetDependents = %v, %v; want 2 nodes", dependents, err)
	}
}

func TestService_GetDependenciesAndDependents_NodeNotFound(t *testing.T) {
	repo := &mockRepo{
		getNodeFn: func(ctx context.Context, id uuid.UUID) (GenericNode, error) { return GenericNode{}, ErrNotFound },
		getDependenciesFn: func(ctx context.Context, id uuid.UUID) ([]GenericNode, error) {
			t.Error("GetDependencies queried a missing node")
			return nil, nil
		},
		getDependentsFn: func(ctx context.Context, id uuid.UUID) ([]GenericNode, error) {
			t.Error("GetDependents queried a missing node")
			return nil, nil
		},
	}
	svc := NewService(repo)
	if _, err := svc.GetDependencies(context.Background(), testNS, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetDependencies err = %v, want ErrNotFound", err)
	}
	if _, err := svc.GetDependents(context.Background(), testNS, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetDependents err = %v, want ErrNotFound", err)
	}
}
