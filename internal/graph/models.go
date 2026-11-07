package graph

import (
	"errors"
	"fmt"
	"strconv"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// MaxNamespaceLength is the maximum length of a namespace in characters,
// matching maxLength in the OpenAPI spec.
const MaxNamespaceLength = 256

// ValidateNamespace checks that ns is usable as a namespace: 1 to
// MaxNamespaceLength ASCII letters, digits, hyphens, underscores, colons and
// periods, with no period at the start or end, matching the pattern of the
// Namespace parameter in the OpenAPI spec. A period can't start or end a
// namespace, so "." and ".." (which the HTTP router resolves as path
// segments) are invalid. The error says which rule ns breaks.
func ValidateNamespace(ns string) error {
	if ns == "" {
		return fmt.Errorf("namespace is empty; it must be 1 to %d characters long", MaxNamespaceLength)
	}
	for i, r := range ns {
		if !isNamespaceChar(r) {
			return fmt.Errorf("namespace contains %s, which is not allowed; allowed characters are ASCII letters, digits, '-', '_', ':' and '.'", quoteChar(ns[i:]))
		}
	}
	switch {
	case ns[0] == '.':
		return errors.New("namespace starts with '.', which is not allowed")
	case ns[len(ns)-1] == '.':
		return errors.New("namespace ends with '.', which is not allowed")
	case len(ns) > MaxNamespaceLength:
		return fmt.Errorf("namespace is %d characters long; the maximum is %d", len(ns), MaxNamespaceLength)
	}
	return nil
}

func isNamespaceChar(r rune) bool {
	return 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' ||
		r == '-' || r == '_' || r == ':' || r == '.'
}

// quoteChar quotes the first character of s for an error message. A
// printable character appears as is, in single quotes; anything else, such
// as a newline or a byte that isn't valid UTF-8, appears as a Go escape
// sequence in double quotes, so the message can't inject lines into a log.
func quoteChar(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError && size == 1 || !unicode.IsPrint(r) {
		return strconv.Quote(s[:size])
	}
	return "'" + s[:size] + "'"
}

// NodeType enumerates the allowed node types.
type NodeType string

const (
	NodeTypeVision      NodeType = "vision"
	NodeTypeGoal        NodeType = "goal"
	NodeTypeFeature     NodeType = "feature"
	NodeTypeRequirement NodeType = "requirement"
	NodeTypeTask        NodeType = "task"
	NodeTypeGitRef      NodeType = "gitref"
)

// NodeTypeToLabel maps API node types to Neo4j labels.
var NodeTypeToLabel = map[NodeType]string{
	NodeTypeVision:      "Vision",
	NodeTypeGoal:        "Goal",
	NodeTypeFeature:     "Feature",
	NodeTypeRequirement: "Requirement",
	NodeTypeTask:        "Task",
	NodeTypeGitRef:      "GitRef",
}

// LabelToNodeType maps Neo4j labels to API node types.
var LabelToNodeType = map[string]NodeType{
	"Vision":      NodeTypeVision,
	"Goal":        NodeTypeGoal,
	"Feature":     NodeTypeFeature,
	"Requirement": NodeTypeRequirement,
	"Task":        NodeTypeTask,
	"GitRef":      NodeTypeGitRef,
}

// EdgeType enumerates the allowed edge types.
type EdgeType string

const (
	EdgeTypeBreaksDownTo  EdgeType = "BREAKS_DOWN_TO"
	EdgeTypeImplementedBy EdgeType = "IMPLEMENTED_BY"
	// EdgeTypeDependsOn means the source node depends on the target node.
	// Both nodes have the same type. Traversals of the breakdown tree
	// (children, parents, subtree, lineage) ignore it.
	EdgeTypeDependsOn EdgeType = "DEPENDS_ON"
)

// AllowedEdges defines which source->target node type pairs are valid for each edge type.
var AllowedEdges = map[EdgeType]map[NodeType]NodeType{
	EdgeTypeBreaksDownTo: {
		NodeTypeVision:      NodeTypeGoal,
		NodeTypeGoal:        NodeTypeFeature,
		NodeTypeFeature:     NodeTypeRequirement,
		NodeTypeRequirement: NodeTypeTask,
	},
	EdgeTypeImplementedBy: {
		NodeTypeTask: NodeTypeGitRef,
	},
	EdgeTypeDependsOn: {
		NodeTypeVision:      NodeTypeVision,
		NodeTypeGoal:        NodeTypeGoal,
		NodeTypeFeature:     NodeTypeFeature,
		NodeTypeRequirement: NodeTypeRequirement,
		NodeTypeTask:        NodeTypeTask,
		NodeTypeGitRef:      NodeTypeGitRef,
	},
}

// GenericNode is a flat map representation of a node with all possible fields.
type GenericNode struct {
	ID                 uuid.UUID `json:"id"`
	Type               NodeType  `json:"type"`
	Title              *string   `json:"title,omitempty"`
	Description        *string   `json:"description,omitempty"`
	Priority           *int      `json:"priority,omitempty"`
	AcceptanceCriteria *string   `json:"acceptanceCriteria,omitempty"`
	Status             *string   `json:"status,omitempty"`
	TargetDate         *string   `json:"targetDate,omitempty"` // Goal: YYYY-MM-DD
	Repo               *string   `json:"repo,omitempty"`
	RefType            *string   `json:"refType,omitempty"`
	Ref                *string   `json:"ref,omitempty"`
	URL                *string   `json:"url,omitempty"`
	Message            *string   `json:"message,omitempty"`
}

// ToProps converts a GenericNode to a map suitable for Neo4j properties.
func (n *GenericNode) ToProps() map[string]any {
	props := map[string]any{
		"id": n.ID.String(),
	}
	if n.Title != nil {
		props["title"] = *n.Title
	}
	if n.Description != nil {
		props["description"] = *n.Description
	}
	if n.Priority != nil {
		props["priority"] = *n.Priority
	}
	if n.AcceptanceCriteria != nil {
		props["acceptanceCriteria"] = *n.AcceptanceCriteria
	}
	if n.Status != nil {
		props["status"] = *n.Status
	}
	if n.TargetDate != nil {
		props["targetDate"] = *n.TargetDate
	}
	if n.Repo != nil {
		props["repo"] = *n.Repo
	}
	if n.RefType != nil {
		props["refType"] = *n.RefType
	}
	if n.Ref != nil {
		props["ref"] = *n.Ref
	}
	if n.URL != nil {
		props["url"] = *n.URL
	}
	if n.Message != nil {
		props["message"] = *n.Message
	}
	return props
}

// NodeFromProps reconstructs a GenericNode from Neo4j properties and label.
func NodeFromProps(props map[string]any, label string) GenericNode {
	n := GenericNode{}

	if id, ok := props["id"].(string); ok {
		n.ID = uuid.MustParse(id)
	}
	if nt, ok := LabelToNodeType[label]; ok {
		n.Type = nt
	}
	if v, ok := props["title"].(string); ok {
		n.Title = &v
	}
	if v, ok := props["description"].(string); ok {
		n.Description = &v
	}
	if v, ok := props["priority"].(int64); ok {
		i := int(v)
		n.Priority = &i
	}
	if v, ok := props["acceptanceCriteria"].(string); ok {
		n.AcceptanceCriteria = &v
	}
	if v, ok := props["status"].(string); ok {
		n.Status = &v
	}
	if v, ok := props["targetDate"].(string); ok {
		n.TargetDate = &v
	}
	if v, ok := props["repo"].(string); ok {
		n.Repo = &v
	}
	if v, ok := props["refType"].(string); ok {
		n.RefType = &v
	}
	if v, ok := props["ref"].(string); ok {
		n.Ref = &v
	}
	if v, ok := props["url"].(string); ok {
		n.URL = &v
	}
	if v, ok := props["message"].(string); ok {
		n.Message = &v
	}
	return n
}
