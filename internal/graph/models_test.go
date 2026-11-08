package graph

import (
	"testing"

	"github.com/google/uuid"
)

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

func TestNodeTypeToLabel(t *testing.T) {
	tests := []struct {
		nodeType NodeType
		label    string
	}{
		{NodeTypeVision, "Vision"},
		{NodeTypeGoal, "Goal"},
		{NodeTypeFeature, "Feature"},
		{NodeTypeRequirement, "Requirement"},
		{NodeTypeTask, "Task"},
		{NodeTypeOutput, "Output"},
	}
	for _, tt := range tests {
		t.Run(string(tt.nodeType), func(t *testing.T) {
			got, ok := NodeTypeToLabel[tt.nodeType]
			if !ok {
				t.Fatalf("NodeTypeToLabel missing key %q", tt.nodeType)
			}
			if got != tt.label {
				t.Errorf("NodeTypeToLabel[%q] = %q, want %q", tt.nodeType, got, tt.label)
			}
		})
	}
}

func TestLabelToNodeType(t *testing.T) {
	tests := []struct {
		label    string
		nodeType NodeType
	}{
		{"Vision", NodeTypeVision},
		{"Goal", NodeTypeGoal},
		{"Feature", NodeTypeFeature},
		{"Requirement", NodeTypeRequirement},
		{"Task", NodeTypeTask},
		{"Output", NodeTypeOutput},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			got, ok := LabelToNodeType[tt.label]
			if !ok {
				t.Fatalf("LabelToNodeType missing key %q", tt.label)
			}
			if got != tt.nodeType {
				t.Errorf("LabelToNodeType[%q] = %q, want %q", tt.label, got, tt.nodeType)
			}
		})
	}
}

func TestLabelMapsAreInverse(t *testing.T) {
	for nt, label := range NodeTypeToLabel {
		got, ok := LabelToNodeType[label]
		if !ok {
			t.Errorf("LabelToNodeType missing label %q for NodeType %q", label, nt)
			continue
		}
		if got != nt {
			t.Errorf("round-trip failed: NodeType %q -> label %q -> NodeType %q", nt, label, got)
		}
	}
	for label, nt := range LabelToNodeType {
		got, ok := NodeTypeToLabel[nt]
		if !ok {
			t.Errorf("NodeTypeToLabel missing NodeType %q for label %q", nt, label)
			continue
		}
		if got != label {
			t.Errorf("round-trip failed: label %q -> NodeType %q -> label %q", label, nt, got)
		}
	}
}

func TestAllowedEdges(t *testing.T) {
	// BREAKS_DOWN_TO edges
	bdt := AllowedEdges[EdgeTypeBreaksDownTo]
	if bdt == nil {
		t.Fatal("AllowedEdges missing BREAKS_DOWN_TO")
	}
	expectedBDT := map[NodeType]NodeType{
		NodeTypeVision:      NodeTypeGoal,
		NodeTypeGoal:        NodeTypeFeature,
		NodeTypeFeature:     NodeTypeRequirement,
		NodeTypeRequirement: NodeTypeTask,
	}
	for src, tgt := range expectedBDT {
		got, ok := bdt[src]
		if !ok {
			t.Errorf("AllowedEdges[BREAKS_DOWN_TO] missing source %q", src)
			continue
		}
		if got != tgt {
			t.Errorf("AllowedEdges[BREAKS_DOWN_TO][%q] = %q, want %q", src, got, tgt)
		}
	}

	// IMPLEMENTED_BY edges
	ib := AllowedEdges[EdgeTypeImplementedBy]
	if ib == nil {
		t.Fatal("AllowedEdges missing IMPLEMENTED_BY")
	}
	if got := ib[NodeTypeTask]; got != NodeTypeOutput {
		t.Errorf("AllowedEdges[IMPLEMENTED_BY][task] = %q, want %q", got, NodeTypeOutput)
	}
}

func TestToProps_AllFieldsSet(t *testing.T) {
	id := uuid.New()
	n := GenericNode{
		ID:                 id,
		Type:               NodeTypeTask,
		Title:              strPtr("My Task"),
		Description:        strPtr("Description"),
		Priority:           intPtr(1),
		AcceptanceCriteria: strPtr("AC"),
		Status:             strPtr("backlog"),
		OutputType:         strPtr("git"),
		Payload:            map[string]any{"currentRef": "abc123"},
	}

	props := n.ToProps()

	if props["id"] != id.String() {
		t.Errorf("id = %v, want %v", props["id"], id.String())
	}
	// Type is NOT included in props (it's represented by the Neo4j label)
	if _, ok := props["type"]; ok {
		t.Error("type should not be in props")
	}

	checks := map[string]string{
		"title":              "My Task",
		"description":        "Description",
		"acceptanceCriteria": "AC",
		"status":             "backlog",
		"outputType":         "git",
		"payload":            `{"currentRef":"abc123"}`,
	}
	for key, want := range checks {
		got, ok := props[key]
		if !ok {
			t.Errorf("props missing key %q", key)
			continue
		}
		if got != want {
			t.Errorf("props[%q] = %v, want %v", key, got, want)
		}
	}
	if got, ok := props["priority"]; !ok || got != 1 {
		t.Errorf("props[priority] = %v, want 1", got)
	}
}

func TestToProps_NilFields(t *testing.T) {
	id := uuid.New()
	n := GenericNode{
		ID:   id,
		Type: NodeTypeVision,
	}

	props := n.ToProps()

	if len(props) != 1 {
		t.Errorf("expected 1 prop (id), got %d: %v", len(props), props)
	}
	if props["id"] != id.String() {
		t.Errorf("id = %v, want %v", props["id"], id.String())
	}
}

func TestNodeFromProps_AllFields(t *testing.T) {
	id := uuid.New()
	props := map[string]any{
		"id":                 id.String(),
		"title":              "Test Title",
		"description":        "Test Description",
		"priority":           int64(3),
		"acceptanceCriteria": "Must pass",
		"status":             "done",
		"outputType":         "git",
		"payload":            `{"repository":"https://github.com/d4nyll/nodoplex.git","currentRef":"main"}`,
	}

	n := NodeFromProps(props, "Feature")

	if n.ID != id {
		t.Errorf("ID = %v, want %v", n.ID, id)
	}
	if n.Type != NodeTypeFeature {
		t.Errorf("Type = %v, want %v", n.Type, NodeTypeFeature)
	}
	if n.Title == nil || *n.Title != "Test Title" {
		t.Errorf("Title = %v, want %q", n.Title, "Test Title")
	}
	if n.Description == nil || *n.Description != "Test Description" {
		t.Errorf("Description = %v, want %q", n.Description, "Test Description")
	}
	if n.Priority == nil || *n.Priority != 3 {
		t.Errorf("Priority = %v, want 3", n.Priority)
	}
	if n.AcceptanceCriteria == nil || *n.AcceptanceCriteria != "Must pass" {
		t.Errorf("AcceptanceCriteria = %v, want %q", n.AcceptanceCriteria, "Must pass")
	}
	if n.Status == nil || *n.Status != "done" {
		t.Errorf("Status = %v, want %q", n.Status, "done")
	}
	if n.OutputType == nil || *n.OutputType != "git" {
		t.Errorf("OutputType = %v, want %q", n.OutputType, "git")
	}
	if len(n.Payload) != 2 || n.Payload["repository"] != "https://github.com/d4nyll/nodoplex.git" || n.Payload["currentRef"] != "main" {
		t.Errorf("Payload = %v, want repository and currentRef", n.Payload)
	}
}

func TestNodeFromProps_EmptyProps(t *testing.T) {
	n := NodeFromProps(map[string]any{}, "")

	if n.ID != uuid.Nil {
		t.Errorf("ID should be zero-value UUID, got %v", n.ID)
	}
	if n.Type != "" {
		t.Errorf("Type should be empty, got %q", n.Type)
	}
	if n.Title != nil {
		t.Errorf("Title should be nil, got %v", n.Title)
	}
}

func TestNodeFromProps_UnknownLabel(t *testing.T) {
	n := NodeFromProps(map[string]any{"id": uuid.New().String()}, "UnknownLabel")
	if n.Type != "" {
		t.Errorf("Type should be empty for unknown label, got %q", n.Type)
	}
}

func TestToPropsAndBack_RoundTrip(t *testing.T) {
	id := uuid.New()
	original := GenericNode{
		ID:          id,
		Type:        NodeTypeRequirement,
		Title:       strPtr("Round Trip"),
		Description: strPtr("Testing round-trip conversion"),
		Priority:    intPtr(5),
	}

	props := original.ToProps()
	// Simulate Neo4j returning int64 for priority
	if p, ok := props["priority"]; ok {
		props["priority"] = int64(p.(int))
	}

	restored := NodeFromProps(props, "Requirement")

	if restored.ID != original.ID {
		t.Errorf("ID mismatch: %v != %v", restored.ID, original.ID)
	}
	if restored.Type != original.Type {
		t.Errorf("Type mismatch: %v != %v", restored.Type, original.Type)
	}
	if *restored.Title != *original.Title {
		t.Errorf("Title mismatch: %q != %q", *restored.Title, *original.Title)
	}
	if *restored.Description != *original.Description {
		t.Errorf("Description mismatch: %q != %q", *restored.Description, *original.Description)
	}
	if *restored.Priority != *original.Priority {
		t.Errorf("Priority mismatch: %d != %d", *restored.Priority, *original.Priority)
	}
}

func TestAllowedEdges_DependsOnRequiresSameType(t *testing.T) {
	deps := AllowedEdges[EdgeTypeDependsOn]
	if len(deps) != len(NodeTypeToLabel) {
		t.Errorf("AllowedEdges[DEPENDS_ON] has %d source types, want %d", len(deps), len(NodeTypeToLabel))
	}
	for nt := range NodeTypeToLabel {
		if got := deps[nt]; got != nt {
			t.Errorf("AllowedEdges[DEPENDS_ON][%q] = %q, want %q", nt, got, nt)
		}
	}
}
