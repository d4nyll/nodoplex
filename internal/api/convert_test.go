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
	payload := OutputPayload{"currentRef": "feat/x"}
	req := UpdateNodeRequest{
		Title:              strPtr("New Title"),
		Description:        strPtr("New Description"),
		Priority:           intPtr(2),
		AcceptanceCriteria: strPtr("AC"),
		Status:             &status,
		OutputType:         strPtr("git"),
		Payload:            &payload,
	}

	props, err := updateRequestToProps(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := map[string]any{
		"title":              "New Title",
		"description":        "New Description",
		"priority":           2,
		"acceptanceCriteria": "AC",
		"status":             "in_progress",
		"outputType":         "git",
		"payload":            `{"currentRef":"feat/x"}`,
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
	props, _ := updateRequestToProps(req)
	if len(props) != 0 {
		t.Errorf("expected empty props, got %d: %v", len(props), props)
	}
}

func TestUpdateRequestToProps_PartialFields(t *testing.T) {
	req := UpdateNodeRequest{
		Title: strPtr("Only Title"),
	}
	props, _ := updateRequestToProps(req)
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

func TestUpdateRequestToProps_EmptyOutputType(t *testing.T) {
	if _, err := updateRequestToProps(UpdateNodeRequest{OutputType: strPtr("")}); err == nil {
		t.Error("expected an error for an empty outputType")
	}
}

func TestParseCreateNodeRequest_Output(t *testing.T) {
	var req CreateNodeRequest
	body := `{"type":"output","outputType":"git","description":"OIDC branch","payload":{"repository":"https://github.com/d4nyll/nodoplex.git","baseRef":"main","currentRef":"feat/oidc"}}`
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	nodeType, props, err := parseCreateNodeRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nodeType != "output" {
		t.Errorf("nodeType = %q, want output", nodeType)
	}
	want := map[string]any{
		"outputType":  "git",
		"description": "OIDC branch",
		"payload":     `{"baseRef":"main","currentRef":"feat/oidc","repository":"https://github.com/d4nyll/nodoplex.git"}`,
	}
	if len(props) != len(want) {
		t.Fatalf("props = %v, want %v", props, want)
	}
	for k, v := range want {
		if props[k] != v {
			t.Errorf("props[%q] = %v, want %v", k, props[k], v)
		}
	}
}

func TestParseCreateNodeRequest_OutputInvalid(t *testing.T) {
	for _, body := range []string{
		`{"type":"output"}`,
		`{"type":"output","outputType":""}`,
		`{"type":"output","outputType":"git","payload":["not","an","object"]}`,
		`{"type":"output","outputType":"git","payload":"text"}`,
	} {
		var req CreateNodeRequest
		if err := json.Unmarshal([]byte(body), &req); err != nil {
			t.Fatalf("unmarshal %s: %v", body, err)
		}
		if _, _, err := parseCreateNodeRequest(req); err == nil {
			t.Errorf("%s: expected an error", body)
		}
	}
}

func TestGenericNodeToAPI_Output(t *testing.T) {
	node := graph.GenericNode{
		ID:          uuid.New(),
		Type:        graph.NodeTypeOutput,
		OutputType:  strPtr("git"),
		Description: strPtr("OIDC branch"),
		Payload:     map[string]any{"currentRef": "feat/oidc", "nested": map[string]any{"n": 1.0}},
	}

	apiNode, err := genericNodeToAPI(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, _ := json.Marshal(apiNode)
	want := `{"description":"OIDC branch","id":"` + node.ID.String() + `","outputType":"git","payload":{"currentRef":"feat/oidc","nested":{"n":1}},"type":"output"}`
	if string(b) != want {
		t.Errorf("JSON = %s, want %s", b, want)
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

func TestUpdateRequestToProps_TargetDate(t *testing.T) {
	// Simulate the JSON that the frontend sends: {"targetDate":"2026-03-15"}
	body := `{"targetDate":"2026-03-15"}`

	var req UpdateNodeRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal UpdateNodeRequest: %v", err)
	}

	if req.TargetDate == nil {
		t.Fatal("TargetDate is nil after unmarshalling — this is the bug!")
	}

	props, _ := updateRequestToProps(req)
	if len(props) != 1 {
		t.Fatalf("expected 1 prop, got %d: %v", len(props), props)
	}
	if props["targetDate"] != "2026-03-15" {
		t.Errorf("targetDate = %v, want %q", props["targetDate"], "2026-03-15")
	}
}

func TestGenericNodeToAPI_GoalWithTargetDate(t *testing.T) {
	td := "2026-03-15"
	node := graph.GenericNode{
		ID:         uuid.New(),
		Type:       graph.NodeTypeGoal,
		Title:      strPtr("Goal with date"),
		TargetDate: &td,
	}

	apiNode, err := genericNodeToAPI(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	goal, err := apiNode.AsGoal()
	if err != nil {
		t.Fatalf("AsGoal error: %v", err)
	}
	if goal.TargetDate == nil {
		t.Fatal("TargetDate is nil in API response — conversion bug!")
	}

	// Verify the JSON output includes targetDate
	data, err := json.Marshal(apiNode)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(data, &raw)
	if raw["targetDate"] != "2026-03-15" {
		t.Errorf("JSON targetDate = %v, want %q", raw["targetDate"], "2026-03-15")
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
