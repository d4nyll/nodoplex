package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/d4nyll/nodoplex/internal/changelog"
	"github.com/d4nyll/nodoplex/internal/events"
	"github.com/d4nyll/nodoplex/internal/graph"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const testNS = "ns-test"

// mockGraphRepo implements graph.Repo for handler testing.
type mockGraphRepo struct {
	createNodeFn       func(ctx context.Context, label string, props map[string]any) (graph.GenericNode, error)
	getNodeFn          func(ctx context.Context, id uuid.UUID) (graph.GenericNode, error)
	listNodesFn        func(ctx context.Context, nodeType *string, status *string, search *string, offset, limit int) ([]graph.GenericNode, int, error)
	updateNodeFn       func(ctx context.Context, id uuid.UUID, props map[string]any) (graph.GenericNode, graph.GenericNode, error)
	deleteNodeFn       func(ctx context.Context, id uuid.UUID) (graph.GenericNode, error)
	createEdgeFn       func(ctx context.Context, sourceID, targetID uuid.UUID, edgeType string) error
	deleteEdgeFn       func(ctx context.Context, sourceID, targetID uuid.UUID, edgeType string) error
	getChildrenFn      func(ctx context.Context, id uuid.UUID) ([]graph.GenericNode, error)
	getParentsFn       func(ctx context.Context, id uuid.UUID) ([]graph.GenericNode, error)
	getSubtreeFn       func(ctx context.Context, id uuid.UUID) (*graph.SubtreeNode, error)
	getLineageFn       func(ctx context.Context, id uuid.UUID) ([]graph.GenericNode, error)
	getNodeTypeFn      func(ctx context.Context, id uuid.UUID) (graph.NodeType, error)
	createDependencyFn func(ctx context.Context, sourceID, targetID uuid.UUID) error
	getDependenciesFn  func(ctx context.Context, id uuid.UUID) ([]graph.GenericNode, error)
	getDependentsFn    func(ctx context.Context, id uuid.UUID) ([]graph.GenericNode, error)

	mu         sync.Mutex
	namespaces []string // namespace passed to each call, in order
}

func (m *mockGraphRepo) record(ns string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.namespaces = append(m.namespaces, ns)
}

func (m *mockGraphRepo) CreateNode(ctx context.Context, ns string, label string, props map[string]any) (graph.GenericNode, error) {
	m.record(ns)
	if m.createNodeFn != nil {
		return m.createNodeFn(ctx, label, props)
	}
	return graph.GenericNode{}, nil
}
func (m *mockGraphRepo) GetNode(ctx context.Context, ns string, id uuid.UUID) (graph.GenericNode, error) {
	m.record(ns)
	if m.getNodeFn != nil {
		return m.getNodeFn(ctx, id)
	}
	return graph.GenericNode{}, nil
}
func (m *mockGraphRepo) ListNodes(ctx context.Context, ns string, nodeType *string, status *string, search *string, offset, limit int) ([]graph.GenericNode, int, error) {
	m.record(ns)
	if m.listNodesFn != nil {
		return m.listNodesFn(ctx, nodeType, status, search, offset, limit)
	}
	return nil, 0, nil
}
func (m *mockGraphRepo) UpdateNode(ctx context.Context, ns string, id uuid.UUID, props map[string]any) (graph.GenericNode, graph.GenericNode, error) {
	m.record(ns)
	if m.updateNodeFn != nil {
		return m.updateNodeFn(ctx, id, props)
	}
	return graph.GenericNode{}, graph.GenericNode{}, nil
}
func (m *mockGraphRepo) DeleteNode(ctx context.Context, ns string, id uuid.UUID) (graph.GenericNode, error) {
	m.record(ns)
	if m.deleteNodeFn != nil {
		return m.deleteNodeFn(ctx, id)
	}
	return graph.GenericNode{}, nil
}
func (m *mockGraphRepo) CreateEdge(ctx context.Context, ns string, sourceID, targetID uuid.UUID, edgeType string) error {
	m.record(ns)
	if m.createEdgeFn != nil {
		return m.createEdgeFn(ctx, sourceID, targetID, edgeType)
	}
	return nil
}
func (m *mockGraphRepo) DeleteEdge(ctx context.Context, ns string, sourceID, targetID uuid.UUID, edgeType string) error {
	m.record(ns)
	if m.deleteEdgeFn != nil {
		return m.deleteEdgeFn(ctx, sourceID, targetID, edgeType)
	}
	return nil
}
func (m *mockGraphRepo) GetChildren(ctx context.Context, ns string, id uuid.UUID) ([]graph.GenericNode, error) {
	m.record(ns)
	if m.getChildrenFn != nil {
		return m.getChildrenFn(ctx, id)
	}
	return []graph.GenericNode{}, nil
}
func (m *mockGraphRepo) GetParents(ctx context.Context, ns string, id uuid.UUID) ([]graph.GenericNode, error) {
	m.record(ns)
	if m.getParentsFn != nil {
		return m.getParentsFn(ctx, id)
	}
	return []graph.GenericNode{}, nil
}
func (m *mockGraphRepo) GetSubtree(ctx context.Context, ns string, id uuid.UUID) (*graph.SubtreeNode, error) {
	m.record(ns)
	if m.getSubtreeFn != nil {
		return m.getSubtreeFn(ctx, id)
	}
	return nil, nil
}
func (m *mockGraphRepo) GetLineage(ctx context.Context, ns string, id uuid.UUID) ([]graph.GenericNode, error) {
	m.record(ns)
	if m.getLineageFn != nil {
		return m.getLineageFn(ctx, id)
	}
	return nil, nil
}
func (m *mockGraphRepo) GetNodeType(ctx context.Context, ns string, id uuid.UUID) (graph.NodeType, error) {
	m.record(ns)
	if m.getNodeTypeFn != nil {
		return m.getNodeTypeFn(ctx, id)
	}
	return "", nil
}

// setupTestServer creates an httptest.Server with mocked dependencies.
func setupTestServer(repo *mockGraphRepo) *httptest.Server {
	graphSvc := graph.NewService(repo)
	bus := events.NewEventBus()
	// With a nil changelog store, publishChange skips the changelog insert.
	handlers := NewHandlers(graphSvc, nil, bus)
	mux := http.NewServeMux()
	handler := NewRouter(handlers, mux)
	return httptest.NewServer(handler)
}

func (m *mockGraphRepo) CreateDependency(ctx context.Context, ns string, sourceID, targetID uuid.UUID) error {
	m.record(ns)
	if m.createDependencyFn != nil {
		return m.createDependencyFn(ctx, sourceID, targetID)
	}
	return nil
}

func (m *mockGraphRepo) GetDependencies(ctx context.Context, ns string, id uuid.UUID) ([]graph.GenericNode, error) {
	m.record(ns)
	if m.getDependenciesFn != nil {
		return m.getDependenciesFn(ctx, id)
	}
	return []graph.GenericNode{}, nil
}

func (m *mockGraphRepo) GetDependents(ctx context.Context, ns string, id uuid.UUID) ([]graph.GenericNode, error) {
	m.record(ns)
	if m.getDependentsFn != nil {
		return m.getDependentsFn(ctx, id)
	}
	return []graph.GenericNode{}, nil
}

// ── CreateNode E2E Tests ─────────────────────────────────────────────────────

func TestE2E_CreateNode_Vision(t *testing.T) {
	id := uuid.New()
	repo := &mockGraphRepo{
		createNodeFn: func(ctx context.Context, label string, props map[string]any) (graph.GenericNode, error) {
			return graph.GenericNode{
				ID:          id,
				Type:        graph.NodeTypeVision,
				Title:       strPtr("E2E Vision"),
				Description: strPtr("Created via e2e test"),
			}, nil
		},
	}

	ts := setupTestServer(repo)
	defer ts.Close()

	body := `{"type":"vision","title":"E2E Vision","description":"Created via e2e test"}`
	resp, err := http.Post(ts.URL+"/namespaces/"+testNS+"/nodes", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, http.StatusCreated, b)
	}

	var node Node
	if err := json.NewDecoder(resp.Body).Decode(&node); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	vision, err := node.AsVision()
	if err != nil {
		t.Fatalf("AsVision error: %v", err)
	}
	if vision.Title != "E2E Vision" {
		t.Errorf("Title = %q, want %q", vision.Title, "E2E Vision")
	}
	if vision.Id.String() != id.String() {
		t.Errorf("ID = %q, want %q", vision.Id.String(), id.String())
	}
}

func TestE2E_CreateNode_Task(t *testing.T) {
	id := uuid.New()
	repo := &mockGraphRepo{
		createNodeFn: func(ctx context.Context, label string, props map[string]any) (graph.GenericNode, error) {
			return graph.GenericNode{
				ID:     id,
				Type:   graph.NodeTypeTask,
				Title:  strPtr("Test Task"),
				Status: strPtr("backlog"),
			}, nil
		},
	}

	ts := setupTestServer(repo)
	defer ts.Close()

	body := `{"type":"task","title":"Test Task","status":"backlog"}`
	resp, err := http.Post(ts.URL+"/namespaces/"+testNS+"/nodes", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, http.StatusCreated, b)
	}

	var node Node
	if err := json.NewDecoder(resp.Body).Decode(&node); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	task, err := node.AsTask()
	if err != nil {
		t.Fatalf("AsTask error: %v", err)
	}
	if task.Status != Backlog {
		t.Errorf("Status = %q, want %q", task.Status, Backlog)
	}
}

func TestE2E_CreateNode_GitRef(t *testing.T) {
	id := uuid.New()
	repo := &mockGraphRepo{
		createNodeFn: func(ctx context.Context, label string, props map[string]any) (graph.GenericNode, error) {
			return graph.GenericNode{
				ID:      id,
				Type:    graph.NodeTypeGitRef,
				Repo:    strPtr("my-repo"),
				RefType: strPtr("commit"),
				Ref:     strPtr("abc123"),
			}, nil
		},
	}

	ts := setupTestServer(repo)
	defer ts.Close()

	body := `{"type":"gitref","repo":"my-repo","refType":"commit","ref":"abc123"}`
	resp, err := http.Post(ts.URL+"/namespaces/"+testNS+"/nodes", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, http.StatusCreated, b)
	}
}

func TestE2E_CreateNode_InvalidJSON(t *testing.T) {
	ts := setupTestServer(&mockGraphRepo{})
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/namespaces/"+testNS+"/nodes", "application/json", strings.NewReader("{invalid"))
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// ── GetNode E2E Tests ────────────────────────────────────────────────────────

func TestE2E_GetNode_Success(t *testing.T) {
	id := uuid.New()
	repo := &mockGraphRepo{
		getNodeFn: func(ctx context.Context, rid uuid.UUID) (graph.GenericNode, error) {
			return graph.GenericNode{
				ID:    id,
				Type:  graph.NodeTypeGoal,
				Title: strPtr("My Goal"),
			}, nil
		},
	}

	ts := setupTestServer(repo)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/namespaces/" + testNS + "/nodes/" + id.String())
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, http.StatusOK, b)
	}

	var result NodeWithRelationships
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	goal, err := result.Node.AsGoal()
	if err != nil {
		t.Fatalf("AsGoal error: %v", err)
	}
	if goal.Title != "My Goal" {
		t.Errorf("Title = %q, want %q", goal.Title, "My Goal")
	}
}

func TestE2E_GetNode_NotFound(t *testing.T) {
	repo := &mockGraphRepo{
		getNodeFn: func(ctx context.Context, id uuid.UUID) (graph.GenericNode, error) {
			return graph.GenericNode{}, graph.ErrNotFound
		},
	}

	ts := setupTestServer(repo)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/namespaces/" + testNS + "/nodes/" + uuid.New().String())
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

// ── ListNodes E2E Tests ──────────────────────────────────────────────────────

func TestE2E_ListNodes(t *testing.T) {
	repo := &mockGraphRepo{
		listNodesFn: func(ctx context.Context, nodeType *string, status *string, search *string, offset, limit int) ([]graph.GenericNode, int, error) {
			return []graph.GenericNode{
				{ID: uuid.New(), Type: graph.NodeTypeVision, Title: strPtr("V1")},
				{ID: uuid.New(), Type: graph.NodeTypeVision, Title: strPtr("V2")},
			}, 2, nil
		},
	}

	ts := setupTestServer(repo)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/namespaces/" + testNS + "/nodes?type=vision")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, http.StatusOK, b)
	}

	var result NodeList
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if result.Total != 2 {
		t.Errorf("Total = %d, want 2", result.Total)
	}
	if len(result.Nodes) != 2 {
		t.Errorf("len(Nodes) = %d, want 2", len(result.Nodes))
	}
}

// ── UpdateNode E2E Tests ─────────────────────────────────────────────────────

func TestE2E_UpdateNode_Success(t *testing.T) {
	id := uuid.New()
	repo := &mockGraphRepo{
		updateNodeFn: func(ctx context.Context, rid uuid.UUID, props map[string]any) (graph.GenericNode, graph.GenericNode, error) {
			return graph.GenericNode{ID: id, Type: graph.NodeTypeVision, Title: strPtr("Before")},
				graph.GenericNode{ID: id, Type: graph.NodeTypeVision, Title: strPtr("After")},
				nil
		},
	}

	ts := setupTestServer(repo)
	defer ts.Close()

	body := `{"title":"After"}`
	req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/namespaces/"+testNS+"/nodes/"+id.String(), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, http.StatusOK, b)
	}

	var node Node
	if err := json.NewDecoder(resp.Body).Decode(&node); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	vision, _ := node.AsVision()
	if vision.Title != "After" {
		t.Errorf("Title = %q, want %q", vision.Title, "After")
	}
}

func TestE2E_UpdateNode_EmptyBody(t *testing.T) {
	ts := setupTestServer(&mockGraphRepo{})
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/namespaces/"+testNS+"/nodes/"+uuid.New().String(), strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, http.StatusBadRequest, b)
	}
}

func TestE2E_UpdateNode_NotFound(t *testing.T) {
	repo := &mockGraphRepo{
		updateNodeFn: func(ctx context.Context, id uuid.UUID, props map[string]any) (graph.GenericNode, graph.GenericNode, error) {
			return graph.GenericNode{}, graph.GenericNode{}, graph.ErrNotFound
		},
	}

	ts := setupTestServer(repo)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/namespaces/"+testNS+"/nodes/"+uuid.New().String(), strings.NewReader(`{"title":"X"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

// ── DeleteNode E2E Tests ─────────────────────────────────────────────────────

func TestE2E_DeleteNode_Success(t *testing.T) {
	id := uuid.New()
	repo := &mockGraphRepo{
		deleteNodeFn: func(ctx context.Context, rid uuid.UUID) (graph.GenericNode, error) {
			return graph.GenericNode{ID: id, Type: graph.NodeTypeTask, Title: strPtr("Deleted")}, nil
		},
	}

	ts := setupTestServer(repo)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/namespaces/"+testNS+"/nodes/"+id.String(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, http.StatusNoContent, b)
	}
}

func TestE2E_DeleteNode_NotFound(t *testing.T) {
	repo := &mockGraphRepo{
		deleteNodeFn: func(ctx context.Context, id uuid.UUID) (graph.GenericNode, error) {
			return graph.GenericNode{}, graph.ErrNotFound
		},
	}

	ts := setupTestServer(repo)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/namespaces/"+testNS+"/nodes/"+uuid.New().String(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

// ── CreateEdge E2E Tests ─────────────────────────────────────────────────────

func TestE2E_CreateEdge_Success(t *testing.T) {
	sourceID := uuid.New()
	targetID := uuid.New()

	repo := &mockGraphRepo{
		getNodeTypeFn: func(ctx context.Context, id uuid.UUID) (graph.NodeType, error) {
			if id == sourceID {
				return graph.NodeTypeVision, nil
			}
			return graph.NodeTypeGoal, nil
		},
		createEdgeFn: func(ctx context.Context, sID, tID uuid.UUID, edgeType string) error {
			return nil
		},
	}

	ts := setupTestServer(repo)
	defer ts.Close()

	body := `{"sourceId":"` + sourceID.String() + `","targetId":"` + targetID.String() + `","type":"BREAKS_DOWN_TO"}`
	resp, err := http.Post(ts.URL+"/namespaces/"+testNS+"/edges", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, http.StatusCreated, b)
	}

	var edge Edge
	if err := json.NewDecoder(resp.Body).Decode(&edge); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if edge.Type != BREAKSDOWNTO {
		t.Errorf("Type = %q, want %q", edge.Type, BREAKSDOWNTO)
	}
}

func TestE2E_CreateEdge_InvalidType(t *testing.T) {
	ts := setupTestServer(&mockGraphRepo{})
	defer ts.Close()

	body := `{"sourceId":"` + uuid.New().String() + `","targetId":"` + uuid.New().String() + `","type":"INVALID"}`
	resp, err := http.Post(ts.URL+"/namespaces/"+testNS+"/edges", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, http.StatusBadRequest, b)
	}
}

// ── DeleteEdge E2E Tests ─────────────────────────────────────────────────────

func TestE2E_DeleteEdge_Success(t *testing.T) {
	repo := &mockGraphRepo{
		deleteEdgeFn: func(ctx context.Context, sID, tID uuid.UUID, edgeType string) error {
			return nil
		},
	}

	ts := setupTestServer(repo)
	defer ts.Close()

	body := `{"sourceId":"` + uuid.New().String() + `","targetId":"` + uuid.New().String() + `","type":"BREAKS_DOWN_TO"}`
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/namespaces/"+testNS+"/edges", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, http.StatusNoContent, b)
	}
}

func TestE2E_DeleteEdge_NotFound(t *testing.T) {
	repo := &mockGraphRepo{
		deleteEdgeFn: func(ctx context.Context, sID, tID uuid.UUID, edgeType string) error {
			return graph.ErrNotFound
		},
	}

	ts := setupTestServer(repo)
	defer ts.Close()

	body := `{"sourceId":"` + uuid.New().String() + `","targetId":"` + uuid.New().String() + `","type":"BREAKS_DOWN_TO"}`
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/namespaces/"+testNS+"/edges", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		b, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, http.StatusNotFound, b)
	}
}

// ── GetChildren E2E Tests ────────────────────────────────────────────────────

func TestE2E_GetChildren_Success(t *testing.T) {
	id := uuid.New()
	childID := uuid.New()

	repo := &mockGraphRepo{
		getNodeFn: func(ctx context.Context, rid uuid.UUID) (graph.GenericNode, error) {
			return graph.GenericNode{ID: id, Type: graph.NodeTypeVision, Title: strPtr("Parent")}, nil
		},
		getChildrenFn: func(ctx context.Context, rid uuid.UUID) ([]graph.GenericNode, error) {
			return []graph.GenericNode{
				{ID: childID, Type: graph.NodeTypeGoal, Title: strPtr("Child Goal")},
			}, nil
		},
	}

	ts := setupTestServer(repo)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/namespaces/" + testNS + "/nodes/" + id.String() + "/children")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, http.StatusOK, b)
	}

	var result NodeArray
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(result.Nodes) != 1 {
		t.Fatalf("expected 1 child, got %d", len(result.Nodes))
	}
}

func TestE2E_GetChildren_NodeNotFound(t *testing.T) {
	repo := &mockGraphRepo{
		getNodeFn: func(ctx context.Context, id uuid.UUID) (graph.GenericNode, error) {
			return graph.GenericNode{}, graph.ErrNotFound
		},
	}

	ts := setupTestServer(repo)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/namespaces/" + testNS + "/nodes/" + uuid.New().String() + "/children")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

// ── GetParents E2E Tests ─────────────────────────────────────────────────────

func TestE2E_GetParents_Success(t *testing.T) {
	id := uuid.New()
	parentID := uuid.New()

	repo := &mockGraphRepo{
		getNodeFn: func(ctx context.Context, rid uuid.UUID) (graph.GenericNode, error) {
			return graph.GenericNode{ID: id, Type: graph.NodeTypeGoal, Title: strPtr("Child")}, nil
		},
		getParentsFn: func(ctx context.Context, rid uuid.UUID) ([]graph.GenericNode, error) {
			return []graph.GenericNode{
				{ID: parentID, Type: graph.NodeTypeVision, Title: strPtr("Parent Vision")},
			}, nil
		},
	}

	ts := setupTestServer(repo)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/namespaces/" + testNS + "/nodes/" + id.String() + "/parents")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, http.StatusOK, b)
	}

	var result NodeArray
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(result.Nodes) != 1 {
		t.Fatalf("expected 1 parent, got %d", len(result.Nodes))
	}
}

// ── GetSubtree E2E Tests ─────────────────────────────────────────────────────

func TestE2E_GetSubtree_Success(t *testing.T) {
	id := uuid.New()

	repo := &mockGraphRepo{
		getSubtreeFn: func(ctx context.Context, rid uuid.UUID) (*graph.SubtreeNode, error) {
			return &graph.SubtreeNode{
				Node: graph.GenericNode{ID: id, Type: graph.NodeTypeVision, Title: strPtr("Root")},
				Children: []graph.SubtreeNode{
					{
						Node:     graph.GenericNode{ID: uuid.New(), Type: graph.NodeTypeGoal, Title: strPtr("Goal")},
						Children: []graph.SubtreeNode{},
					},
				},
			}, nil
		},
	}

	ts := setupTestServer(repo)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/namespaces/" + testNS + "/nodes/" + id.String() + "/subtree")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, http.StatusOK, b)
	}

	var tree TreeNode
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(tree.Children) != 1 {
		t.Errorf("expected 1 child, got %d", len(tree.Children))
	}
}

func TestE2E_GetSubtree_NotFound(t *testing.T) {
	repo := &mockGraphRepo{
		getSubtreeFn: func(ctx context.Context, id uuid.UUID) (*graph.SubtreeNode, error) {
			return nil, graph.ErrNotFound
		},
	}

	ts := setupTestServer(repo)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/namespaces/" + testNS + "/nodes/" + uuid.New().String() + "/subtree")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

// ── GetLineage E2E Tests ─────────────────────────────────────────────────────

func TestE2E_GetLineage_Success(t *testing.T) {
	id := uuid.New()

	repo := &mockGraphRepo{
		getNodeFn: func(ctx context.Context, rid uuid.UUID) (graph.GenericNode, error) {
			return graph.GenericNode{ID: id, Type: graph.NodeTypeTask, Title: strPtr("Task")}, nil
		},
		getLineageFn: func(ctx context.Context, rid uuid.UUID) ([]graph.GenericNode, error) {
			return []graph.GenericNode{
				{ID: uuid.New(), Type: graph.NodeTypeVision, Title: strPtr("Vision")},
				{ID: uuid.New(), Type: graph.NodeTypeGoal, Title: strPtr("Goal")},
				{ID: id, Type: graph.NodeTypeTask, Title: strPtr("Task")},
			}, nil
		},
	}

	ts := setupTestServer(repo)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/namespaces/" + testNS + "/nodes/" + id.String() + "/lineage")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, http.StatusOK, b)
	}

	var result NodeArray
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(result.Nodes) != 3 {
		t.Errorf("expected 3 nodes in lineage, got %d", len(result.Nodes))
	}
}

func TestE2E_GetLineage_NotFound(t *testing.T) {
	repo := &mockGraphRepo{
		getNodeFn: func(ctx context.Context, id uuid.UUID) (graph.GenericNode, error) {
			return graph.GenericNode{}, graph.ErrNotFound
		},
	}

	ts := setupTestServer(repo)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/namespaces/" + testNS + "/nodes/" + uuid.New().String() + "/lineage")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

// ── Content-Type Tests ───────────────────────────────────────────────────────

func TestE2E_ResponseContentType(t *testing.T) {
	repo := &mockGraphRepo{
		listNodesFn: func(ctx context.Context, nodeType *string, status *string, search *string, offset, limit int) ([]graph.GenericNode, int, error) {
			return []graph.GenericNode{}, 0, nil
		},
	}

	ts := setupTestServer(repo)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/namespaces/" + testNS + "/nodes")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

// ── Full CRUD Flow E2E ───────────────────────────────────────────────────────

func TestE2E_FullCRUDFlow(t *testing.T) {
	id := uuid.New()
	currentTitle := "Original"

	repo := &mockGraphRepo{
		createNodeFn: func(ctx context.Context, label string, props map[string]any) (graph.GenericNode, error) {
			return graph.GenericNode{
				ID:    id,
				Type:  graph.NodeTypeVision,
				Title: strPtr(currentTitle),
			}, nil
		},
		getNodeFn: func(ctx context.Context, rid uuid.UUID) (graph.GenericNode, error) {
			if rid == id {
				return graph.GenericNode{
					ID:    id,
					Type:  graph.NodeTypeVision,
					Title: strPtr(currentTitle),
				}, nil
			}
			return graph.GenericNode{}, graph.ErrNotFound
		},
		updateNodeFn: func(ctx context.Context, rid uuid.UUID, props map[string]any) (graph.GenericNode, graph.GenericNode, error) {
			before := graph.GenericNode{ID: id, Type: graph.NodeTypeVision, Title: strPtr(currentTitle)}
			if t, ok := props["title"].(string); ok {
				currentTitle = t
			}
			after := graph.GenericNode{ID: id, Type: graph.NodeTypeVision, Title: strPtr(currentTitle)}
			return before, after, nil
		},
		deleteNodeFn: func(ctx context.Context, rid uuid.UUID) (graph.GenericNode, error) {
			return graph.GenericNode{ID: id, Type: graph.NodeTypeVision, Title: strPtr(currentTitle)}, nil
		},
	}

	ts := setupTestServer(repo)
	defer ts.Close()

	// 1. Create
	createBody := `{"type":"vision","title":"Original"}`
	createResp, err := http.Post(ts.URL+"/namespaces/"+testNS+"/nodes", "application/json", strings.NewReader(createBody))
	if err != nil {
		t.Fatalf("create error: %v", err)
	}
	createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", createResp.StatusCode)
	}

	// 2. Read
	getResp, err := http.Get(ts.URL + "/namespaces/" + testNS + "/nodes/" + id.String())
	if err != nil {
		t.Fatalf("get error: %v", err)
	}
	getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get status = %d", getResp.StatusCode)
	}

	// 3. Update
	updateReq, _ := http.NewRequest(http.MethodPatch, ts.URL+"/namespaces/"+testNS+"/nodes/"+id.String(), strings.NewReader(`{"title":"Updated"}`))
	updateReq.Header.Set("Content-Type", "application/json")
	updateResp, err := http.DefaultClient.Do(updateReq)
	if err != nil {
		t.Fatalf("update error: %v", err)
	}
	updateResp.Body.Close()
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d", updateResp.StatusCode)
	}

	// 4. Delete
	deleteReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/namespaces/"+testNS+"/nodes/"+id.String(), nil)
	deleteResp, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatalf("delete error: %v", err)
	}
	deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d", deleteResp.StatusCode)
	}
}

// Verify that Handlers implements the ServerInterface
var _ ServerInterface = (*Handlers)(nil)

// Verify that changelog.Store exists and has expected methods
var _ = changelog.Entry{}

// ── Namespace Tests ──────────────────────────────────────────────────────────

func TestE2E_MissingNamespace(t *testing.T) {
	repo := &mockGraphRepo{}
	ts := setupTestServer(repo)
	defer ts.Close()

	id := uuid.New().String()
	tests := []struct{ method, path string }{
		{http.MethodGet, "/nodes"},
		{http.MethodPost, "/nodes"},
		{http.MethodGet, "/nodes/" + id},
		{http.MethodPatch, "/nodes/" + id},
		{http.MethodDelete, "/nodes/" + id},
		{http.MethodGet, "/nodes/" + id + "/children"},
		{http.MethodGet, "/nodes/" + id + "/parents"},
		{http.MethodGet, "/nodes/" + id + "/subtree"},
		{http.MethodGet, "/nodes/" + id + "/lineage"},
		{http.MethodGet, "/nodes/" + id + "/dependencies"},
		{http.MethodGet, "/nodes/" + id + "/dependents"},
		{http.MethodPost, "/edges"},
		{http.MethodDelete, "/edges"},
		{http.MethodGet, "/changes?type=task"},
		{http.MethodGet, "/subscribe"},
		{http.MethodGet, "/namespaces//nodes"},
		{http.MethodGet, "/namespaces//nodes/" + id},
		{http.MethodGet, "/namespaces/./nodes"},
		{http.MethodGet, "/namespaces/../nodes"},
		{http.MethodGet, "/namespaces/%2E%2E/nodes/" + id},
		{http.MethodGet, "/namespaces/"},
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, tt := range tests {
		req, _ := http.NewRequest(tt.method, ts.URL+tt.path, strings.NewReader(`{}`))
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", tt.method, tt.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s %s: status = %d, want 400; body: %s", tt.method, tt.path, resp.StatusCode, body)
		}
	}
	if len(repo.namespaces) != 0 {
		t.Errorf("repo was called %d times, want 0", len(repo.namespaces))
	}
}

func TestE2E_InvalidNamespace(t *testing.T) {
	repo := &mockGraphRepo{}
	ts := setupTestServer(repo)
	defer ts.Close()

	for _, ns := range []string{strings.Repeat("a", graph.MaxNamespaceLength+1), "org%FF", "%00", "%2E%2E", "org%2F42", "a@b", "a%20b", "gr%C3%B6%C3%9Fe", ".a"} {
		resp, err := http.Get(ts.URL + "/namespaces/" + ns + "/nodes")
		if err != nil {
			t.Fatalf("request error: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("namespace %q: status = %d, want 400", ns, resp.StatusCode)
		}
	}
	if len(repo.namespaces) != 0 {
		t.Errorf("repo was called %d times, want 0", len(repo.namespaces))
	}
}

func TestE2E_NamespaceFromPathReachesRepo(t *testing.T) {
	repo := &mockGraphRepo{
		getNodeFn: func(ctx context.Context, id uuid.UUID) (graph.GenericNode, error) {
			return graph.GenericNode{}, graph.ErrNotFound
		},
	}
	ts := setupTestServer(repo)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/namespaces/Org-1_team:prod.eu/nodes/" + uuid.New().String())
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
	if len(repo.namespaces) != 1 || repo.namespaces[0] != "Org-1_team:prod.eu" {
		t.Errorf("repo namespaces = %q, want [\"Org-1_team:prod.eu\"]", repo.namespaces)
	}
}

func TestE2E_DeleteEdge_UnknownType(t *testing.T) {
	repo := &mockGraphRepo{}
	ts := setupTestServer(repo)
	defer ts.Close()

	body := `{"sourceId":"` + uuid.New().String() + `","targetId":"` + uuid.New().String() + `","type":"X]->() DETACH DELETE b //"}`
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/namespaces/"+testNS+"/edges", strings.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
	if len(repo.namespaces) != 0 {
		t.Errorf("repo was called %d times, want 0", len(repo.namespaces))
	}
}

// ── Dependency Tests ─────────────────────────────────────────────────────────

func TestE2E_CreateEdge_DependsOnStatuses(t *testing.T) {
	tests := []struct {
		name    string
		repoErr error
		self    bool
		want    int
	}{
		{"created", nil, false, http.StatusCreated},
		{"cycle", graph.ErrDependencyCycle, false, http.StatusConflict},
		{"not found", graph.ErrNotFound, false, http.StatusNotFound},
		{"self", nil, true, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockGraphRepo{
				getNodeTypeFn: func(ctx context.Context, id uuid.UUID) (graph.NodeType, error) {
					return graph.NodeTypeRequirement, nil
				},
				createDependencyFn: func(ctx context.Context, s, tg uuid.UUID) error { return tt.repoErr },
			}
			ts := setupTestServer(repo)
			defer ts.Close()

			source, target := uuid.New().String(), uuid.New().String()
			if tt.self {
				target = source
			}
			body := `{"sourceId":"` + source + `","targetId":"` + target + `","type":"DEPENDS_ON"}`
			resp, err := http.Post(ts.URL+"/namespaces/"+testNS+"/edges", "application/json", strings.NewReader(body))
			if err != nil {
				t.Fatalf("request error: %v", err)
			}
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != tt.want {
				t.Errorf("status = %d, want %d; body: %s", resp.StatusCode, tt.want, b)
			}
		})
	}
}

func TestE2E_GetDependenciesAndDependents(t *testing.T) {
	dep := graph.GenericNode{ID: uuid.New(), Type: graph.NodeTypeTask, Title: strPtr("dep"), Status: strPtr("backlog")}
	repo := &mockGraphRepo{
		getDependenciesFn: func(ctx context.Context, id uuid.UUID) ([]graph.GenericNode, error) {
			return []graph.GenericNode{dep}, nil
		},
		getDependentsFn: func(ctx context.Context, id uuid.UUID) ([]graph.GenericNode, error) {
			return []graph.GenericNode{}, nil
		},
	}
	ts := setupTestServer(repo)
	defer ts.Close()

	for path, want := range map[string]int{"dependencies": 1, "dependents": 0} {
		resp, err := http.Get(ts.URL + "/namespaces/" + testNS + "/nodes/" + uuid.New().String() + "/" + path)
		if err != nil {
			t.Fatalf("request error: %v", err)
		}
		var arr NodeArray
		json.NewDecoder(resp.Body).Decode(&arr)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || len(arr.Nodes) != want {
			t.Errorf("%s: status %d, %d nodes; want 200, %d nodes", path, resp.StatusCode, len(arr.Nodes), want)
		}
	}
}

func TestE2E_GetDependenciesAndDependents_NotFound(t *testing.T) {
	repo := &mockGraphRepo{
		getNodeFn: func(ctx context.Context, id uuid.UUID) (graph.GenericNode, error) {
			return graph.GenericNode{}, graph.ErrNotFound
		},
	}
	ts := setupTestServer(repo)
	defer ts.Close()

	for _, path := range []string{"dependencies", "dependents"} {
		resp, err := http.Get(ts.URL + "/namespaces/" + testNS + "/nodes/" + uuid.New().String() + "/" + path)
		if err != nil {
			t.Fatalf("request error: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, resp.StatusCode)
		}
	}
}

// ── ListChanges E2E Tests ────────────────────────────────────────────────────

func TestE2E_ListChanges_InvalidType(t *testing.T) {
	ts := setupTestServer(&mockGraphRepo{})
	defer ts.Close()

	for _, query := range []string{"type=", "type=bogus"} {
		resp, err := http.Get(ts.URL + "/namespaces/" + testNS + "/changes?" + query)
		if err != nil {
			t.Fatalf("request error: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want %d", query, resp.StatusCode, http.StatusBadRequest)
		}
	}
}

// TestPublishChange_SnapshotCarriesAPIFields checks that the node snapshot in
// a change event holds every field the API returns for each node type.
func TestPublishChange_SnapshotCarriesAPIFields(t *testing.T) {
	props := map[string]any{
		"title": "t", "description": "d", "priority": int64(2), "acceptanceCriteria": "ac",
		"status": "in_progress", "targetDate": "2026-12-31",
		"repo": "r", "refType": "branch", "ref": "main", "url": "https://example.com", "message": "m",
	}
	for _, label := range []string{"Vision", "Goal", "Feature", "Requirement", "Task", "GitRef"} {
		props["id"] = uuid.NewString()
		n := graph.NodeFromProps(props, label)

		bus := events.NewEventBus()
		ch, unsub := bus.Subscribe(testNS, nil)
		h := NewHandlers(nil, nil, bus)
		h.publishChange(httptest.NewRequest(http.MethodPost, "/", nil), testNS, events.ActionCreated, n, nil)
		ev := <-ch
		unsub()

		apiNode, err := genericNodeToAPI(n)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		apiJSON, _ := json.Marshal(apiNode)
		var want, got map[string]any
		json.Unmarshal(apiJSON, &want)
		json.Unmarshal(ev.Node, &got)
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s: snapshot %s = %v, want %v (API: %s, snapshot: %s)", label, k, got[k], v, apiJSON, ev.Node)
			}
		}
	}
}

// TestE2E_ChangelogFailure checks that a node write whose changelog insert
// fails is answered with 500, logged, and not published.
func TestE2E_ChangelogFailure(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://127.0.0.1:1/none")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close() // every query on a closed pool fails

	id := uuid.New()
	node := graph.GenericNode{ID: id, Type: graph.NodeTypeVision, Title: strPtr("v")}
	repo := &mockGraphRepo{
		createNodeFn: func(ctx context.Context, label string, props map[string]any) (graph.GenericNode, error) {
			return node, nil
		},
		updateNodeFn: func(ctx context.Context, id uuid.UUID, props map[string]any) (graph.GenericNode, graph.GenericNode, error) {
			return node, node, nil
		},
		deleteNodeFn: func(ctx context.Context, id uuid.UUID) (graph.GenericNode, error) {
			return node, nil
		},
	}
	bus := events.NewEventBus()
	ch, unsub := bus.Subscribe(testNS, nil)
	defer unsub()
	ts := httptest.NewServer(NewRouter(NewHandlers(graph.NewService(repo), changelog.NewStore(pool), bus), http.NewServeMux()))
	defer ts.Close()

	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)

	url := ts.URL + "/namespaces/" + testNS + "/nodes"
	for _, c := range []struct{ method, path, body string }{
		{http.MethodPost, "", `{"type":"vision","title":"v"}`},
		{http.MethodPatch, "/" + id.String(), `{"title":"v"}`},
		{http.MethodDelete, "/" + id.String(), ""},
	} {
		req, _ := http.NewRequest(c.method, url+c.path, strings.NewReader(c.body))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", c.method, err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusInternalServerError || !strings.Contains(string(b), "not recorded in the changelog") {
			t.Errorf("%s: status = %d, body = %s; want 500 saying the change was not recorded", c.method, resp.StatusCode, b)
		}
	}
	if n := strings.Count(logs.String(), id.String()+` in namespace "`+testNS+`" not recorded`); n != 3 {
		t.Errorf("logged %d unrecorded changes, want 3; log:\n%s", n, logs.String())
	}
	select {
	case ev := <-ch:
		t.Errorf("published %+v, want nothing", ev)
	default:
	}
}
