package api

import (
	"encoding/json"
	"testing"

	"github.com/d4nyll/nodoplex/internal/graph"
	"github.com/google/uuid"
)

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

func TestDerefStr(t *testing.T) {
	if got := derefStr(nil); got != "" {
		t.Errorf("derefStr(nil) = %q, want empty", got)
	}
	s := "hello"
	if got := derefStr(&s); got != "hello" {
		t.Errorf("derefStr(&%q) = %q, want %q", s, got, s)
	}
}

func TestUpdateRequestToProps_AllFields(t *testing.T) {
	status := TaskStatus("in_progress")
	refType := GitRefKind("commit")
	req := UpdateNodeRequest{
		Title:              strPtr("New Title"),
		Description:        strPtr("New Description"),
		Priority:           intPtr(2),
		AcceptanceCriteria: strPtr("AC"),
		Status:             &status,
		Repo:               strPtr("my-repo"),
		RefType:            &refType,
		Ref:                strPtr("abc123"),
		Url:                strPtr("https://example.com"),
		Message:            strPtr("commit msg"),
	}

	props := updateRequestToProps(req)

	expected := map[string]any{
		"title":              "New Title",
		"description":        "New Description",
		"priority":           2,
		"acceptanceCriteria": "AC",
		"status":             "in_progress",
		"repo":               "my-repo",
		"refType":            "commit",
		"ref":                "abc123",
		"url":                "https://example.com",
		"message":            "commit msg",
	}

	if len(props) != len(expected) {
		t.Fatalf("expected %d props, got %d: %v", len(expected), len(props), props)
	}

	for k, want := range expected {
		got, ok := props[k]
		if !ok {
			t.Errorf("missing key %q", k)
			continue
		}
		// Handle int comparison (priority is int, not any)
		switch w := want.(type) {
		case int:
			if g, ok := got.(int); !ok || g != w {
				t.Errorf("props[%q] = %v (type %T), want %v", k, got, got, want)
			}
		case string:
			if g, ok := got.(string); !ok || g != w {
				t.Errorf("props[%q] = %v, want %v", k, got, want)
			}
		}
	}
}

func TestUpdateRequestToProps_Empty(t *testing.T) {
	req := UpdateNodeRequest{}
	props := updateRequestToProps(req)
	if len(props) != 0 {
		t.Errorf("expected empty props, got %d: %v", len(props), props)
	}
}

func TestUpdateRequestToProps_PartialFields(t *testing.T) {
	req := UpdateNodeRequest{
		Title: strPtr("Only Title"),
	}
	props := updateRequestToProps(req)
	if len(props) != 1 {
		t.Errorf("expected 1 prop, got %d: %v", len(props), props)
	}
	if props["title"] != "Only Title" {
		t.Errorf("title = %v, want %q", props["title"], "Only Title")
	}
}

func TestGenericNodeToAPI_Vision(t *testing.T) {
	id := uuid.New()
	node := graph.GenericNode{
		ID:          id,
		Type:        graph.NodeTypeVision,
		Title:       strPtr("My Vision"),
		Description: strPtr("A great vision"),
	}

	apiNode, err := genericNodeToAPI(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	vision, err := apiNode.AsVision()
	if err != nil {
		t.Fatalf("AsVision error: %v", err)
	}
	if vision.Title != "My Vision" {
		t.Errorf("Title = %q, want %q", vision.Title, "My Vision")
	}
	if vision.Description == nil || *vision.Description != "A great vision" {
		t.Errorf("Description = %v, want %q", vision.Description, "A great vision")
	}
	if vision.Type != VisionTypeVision {
		t.Errorf("Type = %q, want %q", vision.Type, VisionTypeVision)
	}
}

func TestGenericNodeToAPI_Goal(t *testing.T) {
	node := graph.GenericNode{
		ID:    uuid.New(),
		Type:  graph.NodeTypeGoal,
		Title: strPtr("My Goal"),
	}

	apiNode, err := genericNodeToAPI(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	goal, err := apiNode.AsGoal()
	if err != nil {
		t.Fatalf("AsGoal error: %v", err)
	}
	if goal.Title != "My Goal" {
		t.Errorf("Title = %q, want %q", goal.Title, "My Goal")
	}
}

func TestGenericNodeToAPI_Feature(t *testing.T) {
	node := graph.GenericNode{
		ID:       uuid.New(),
		Type:     graph.NodeTypeFeature,
		Title:    strPtr("Feature X"),
		Priority: intPtr(1),
	}

	apiNode, err := genericNodeToAPI(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	feat, err := apiNode.AsFeature()
	if err != nil {
		t.Fatalf("AsFeature error: %v", err)
	}
	if feat.Title != "Feature X" {
		t.Errorf("Title = %q, want %q", feat.Title, "Feature X")
	}
	if feat.Priority == nil || *feat.Priority != 1 {
		t.Errorf("Priority = %v, want 1", feat.Priority)
	}
}

func TestGenericNodeToAPI_Requirement(t *testing.T) {
	node := graph.GenericNode{
		ID:                 uuid.New(),
		Type:               graph.NodeTypeRequirement,
		Title:              strPtr("Req 1"),
		AcceptanceCriteria: strPtr("Must work"),
	}

	apiNode, err := genericNodeToAPI(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req, err := apiNode.AsRequirement()
	if err != nil {
		t.Fatalf("AsRequirement error: %v", err)
	}
	if req.Title != "Req 1" {
		t.Errorf("Title = %q, want %q", req.Title, "Req 1")
	}
	if req.AcceptanceCriteria == nil || *req.AcceptanceCriteria != "Must work" {
		t.Errorf("AcceptanceCriteria = %v, want %q", req.AcceptanceCriteria, "Must work")
	}
}

func TestGenericNodeToAPI_Task(t *testing.T) {
	node := graph.GenericNode{
		ID:     uuid.New(),
		Type:   graph.NodeTypeTask,
		Title:  strPtr("Task 1"),
		Status: strPtr("in_progress"),
	}

	apiNode, err := genericNodeToAPI(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	task, err := apiNode.AsTask()
	if err != nil {
		t.Fatalf("AsTask error: %v", err)
	}
	if task.Title != "Task 1" {
		t.Errorf("Title = %q, want %q", task.Title, "Task 1")
	}
	if task.Status != InProgress {
		t.Errorf("Status = %q, want %q", task.Status, InProgress)
	}
}

func TestGenericNodeToAPI_TaskDefaultStatus(t *testing.T) {
	node := graph.GenericNode{
		ID:    uuid.New(),
		Type:  graph.NodeTypeTask,
		Title: strPtr("Task No Status"),
	}

	apiNode, err := genericNodeToAPI(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	task, err := apiNode.AsTask()
	if err != nil {
		t.Fatalf("AsTask error: %v", err)
	}
	if task.Status != Backlog {
		t.Errorf("default Status = %q, want %q", task.Status, Backlog)
	}
}

func TestGenericNodeToAPI_GitRef(t *testing.T) {
	node := graph.GenericNode{
		ID:      uuid.New(),
		Type:    graph.NodeTypeGitRef,
		Repo:    strPtr("my-repo"),
		RefType: strPtr("branch"),
		Ref:     strPtr("main"),
		URL:     strPtr("https://github.com"),
		Message: strPtr("Initial"),
	}

	apiNode, err := genericNodeToAPI(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gitRef, err := apiNode.AsGitRef()
	if err != nil {
		t.Fatalf("AsGitRef error: %v", err)
	}
	if gitRef.Repo != "my-repo" {
		t.Errorf("Repo = %q, want %q", gitRef.Repo, "my-repo")
	}
	if gitRef.RefType != Branch {
		t.Errorf("RefType = %q, want %q", gitRef.RefType, Branch)
	}
	if gitRef.Ref != "main" {
		t.Errorf("Ref = %q, want %q", gitRef.Ref, "main")
	}
	if gitRef.Url == nil || *gitRef.Url != "https://github.com" {
		t.Errorf("URL = %v, want %q", gitRef.Url, "https://github.com")
	}
	if gitRef.Message == nil || *gitRef.Message != "Initial" {
		t.Errorf("Message = %v, want %q", gitRef.Message, "Initial")
	}
}

func TestGenericNodeToAPI_GitRefDefaultRefType(t *testing.T) {
	node := graph.GenericNode{
		ID:   uuid.New(),
		Type: graph.NodeTypeGitRef,
		Repo: strPtr("repo"),
		Ref:  strPtr("abc"),
	}

	apiNode, err := genericNodeToAPI(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gitRef, err := apiNode.AsGitRef()
	if err != nil {
		t.Fatalf("AsGitRef error: %v", err)
	}
	if gitRef.RefType != Commit {
		t.Errorf("default RefType = %q, want %q", gitRef.RefType, Commit)
	}
}

func TestGenericNodesToAPI(t *testing.T) {
	nodes := []graph.GenericNode{
		{ID: uuid.New(), Type: graph.NodeTypeVision, Title: strPtr("V1")},
		{ID: uuid.New(), Type: graph.NodeTypeGoal, Title: strPtr("G1")},
	}

	result := genericNodesToAPI(nodes)
	if len(result) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(result))
	}

	// Verify first is a vision
	v, err := result[0].AsVision()
	if err != nil {
		t.Fatalf("first node AsVision error: %v", err)
	}
	if v.Title != "V1" {
		t.Errorf("first Title = %q, want %q", v.Title, "V1")
	}

	// Verify second is a goal
	g, err := result[1].AsGoal()
	if err != nil {
		t.Fatalf("second node AsGoal error: %v", err)
	}
	if g.Title != "G1" {
		t.Errorf("second Title = %q, want %q", g.Title, "G1")
	}
}

func TestGenericNodesToAPI_Empty(t *testing.T) {
	result := genericNodesToAPI(nil)
	if len(result) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(result))
	}
}

func TestSubtreeToAPI(t *testing.T) {
	parentID := uuid.New()
	child1ID := uuid.New()
	child2ID := uuid.New()

	st := &graph.SubtreeNode{
		Node: graph.GenericNode{ID: parentID, Type: graph.NodeTypeVision, Title: strPtr("Root")},
		Children: []graph.SubtreeNode{
			{
				Node:     graph.GenericNode{ID: child1ID, Type: graph.NodeTypeGoal, Title: strPtr("Child 1")},
				Children: []graph.SubtreeNode{},
			},
			{
				Node:     graph.GenericNode{ID: child2ID, Type: graph.NodeTypeGoal, Title: strPtr("Child 2")},
				Children: []graph.SubtreeNode{},
			},
		},
	}

	apiTree := subtreeToAPI(st)

	// Root node
	rootVision, err := apiTree.Node.AsVision()
	if err != nil {
		t.Fatalf("root AsVision error: %v", err)
	}
	if rootVision.Title != "Root" {
		t.Errorf("root Title = %q, want %q", rootVision.Title, "Root")
	}

	// Children
	if len(apiTree.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(apiTree.Children))
	}

	c1, err := apiTree.Children[0].Node.AsGoal()
	if err != nil {
		t.Fatalf("child 1 AsGoal error: %v", err)
	}
	if c1.Title != "Child 1" {
		t.Errorf("child 1 Title = %q, want %q", c1.Title, "Child 1")
	}
}

func TestGenericNodeToAPI_JSONRoundTrip(t *testing.T) {
	// Test that the API node serializes and deserializes correctly
	node := graph.GenericNode{
		ID:          uuid.New(),
		Type:        graph.NodeTypeVision,
		Title:       strPtr("JSON Test"),
		Description: strPtr("Testing JSON round-trip"),
	}

	apiNode, err := genericNodeToAPI(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := json.Marshal(apiNode)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored Node
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	vision, err := restored.AsVision()
	if err != nil {
		t.Fatalf("AsVision on restored: %v", err)
	}
	if vision.Title != "JSON Test" {
		t.Errorf("Title = %q, want %q", vision.Title, "JSON Test")
	}
}
