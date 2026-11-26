// Package graph holds the graph of one Product in memory, or the part of it
// that an operation reads (see NewPartialGraph), and runs the steps and the
// rules of the base specification on it. It doesn't read or write a
// database.
package graph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/google/uuid"
)

type NodeType string

const (
	TypeProduct     NodeType = "product"
	TypeVision      NodeType = "vision"
	TypeGoal        NodeType = "goal"
	TypeFeature     NodeType = "feature"
	TypeRequirement NodeType = "requirement"
	TypeTask        NodeType = "task"
	TypeSubtask     NodeType = "subtask"
)

// parentType gives the Parent Type of each Node Type. A Product has none.
var parentType = map[NodeType]NodeType{
	TypeVision:      TypeProduct,
	TypeGoal:        TypeVision,
	TypeFeature:     TypeGoal,
	TypeRequirement: TypeFeature,
	TypeTask:        TypeRequirement,
	TypeSubtask:     TypeTask,
}

func (t NodeType) Valid() bool { return t == TypeProduct || parentType[t] != "" }

type Status string

const (
	NotStarted Status = "not_started"
	InProgress Status = "in_progress"
	Done       Status = "done"
)

func (s Status) Valid() bool { return s == NotStarted || s == InProgress || s == Done }

type EdgeKind string

const (
	Hierarchical EdgeKind = "hierarchical"
	Dependency   EdgeKind = "dependency"
)

// Output is a result of the work that a Node represents. Payload is the JSON
// object that the client gave, as encoding/json decodes it with UseNumber, so
// that each number keeps its exact text.
type Output struct {
	OutputType string  `json:"outputType"`
	Title      *string `json:"title,omitempty"`
	Payload    any     `json:"payload,omitempty"`
}

// Fields holds the Fields of a Node. A nil pointer is a Field with no value.
// Slug is set only on a Product Node.
type Fields struct {
	Product     string   `json:"product"`
	Title       string   `json:"title"`
	Description *string  `json:"description,omitempty"`
	Outputs     []Output `json:"outputs"`
	Status      Status   `json:"status"`
	Assignee    *string  `json:"assignee,omitempty"`
	Slug        *string  `json:"slug,omitempty"`
}

type Node struct {
	ID           uuid.UUID
	Type         NodeType
	Version      int64
	Fields       Fields
	Watchers     []string // in code point order, each one time
	ArchivedMark bool
}

type Edge struct {
	ID       uuid.UUID
	Kind     EdgeKind
	TailID   uuid.UUID
	HeadID   uuid.UUID
	Priority int64 // only for a Hierarchical Edge
}

// Graph holds the Nodes and the Edges of one Product, or a part of them (see
// NewPartialGraph).
type Graph struct {
	Slug  string
	Nodes map[uuid.UUID]*Node
	Edges map[uuid.UUID]*Edge
	// part is nil if the Graph holds the whole graph of the Product.
	part *partial
}

func NewGraph(slug string) *Graph {
	return &Graph{Slug: slug, Nodes: map[uuid.UUID]*Node{}, Edges: map[uuid.UUID]*Edge{}}
}

// Clone returns a copy that a write can change without changing g. Payloads
// are shared, because nothing changes a payload in place. A copy of a partial
// Graph shares what g holds and what g misses.
func (g *Graph) Clone() *Graph {
	c := NewGraph(g.Slug)
	c.part = g.part
	for id, n := range g.Nodes {
		cn := *n
		cn.Fields.Outputs = slices.Clone(n.Fields.Outputs)
		cn.Watchers = slices.Clone(n.Watchers)
		c.Nodes[id] = &cn
	}
	for id, e := range g.Edges {
		ce := *e
		c.Edges[id] = &ce
	}
	return c
}

// ProductNode returns the Product Node, or nil if the graph has none.
func (g *Graph) ProductNode() *Node {
	for _, n := range g.Nodes {
		if n.Type == TypeProduct {
			return n
		}
	}
	if g.part != nil {
		g.part.miss.Product = true
	}
	return nil
}

// idCmp compares IDs in id order: ascending, each read as a 128-bit unsigned
// integer.
func idCmp(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) }

func sortedNodeIDs(nodes map[uuid.UUID]*Node) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, idCmp)
	return ids
}

// ── JSON forms ──────────────────────────────────────────────────────────────

// NodeJSON is the JSON form of a Node, as the extended specification gives.
type NodeJSON struct {
	ID           uuid.UUID `json:"id"`
	Type         NodeType  `json:"type"`
	Version      int64     `json:"version"`
	Fields       Fields    `json:"fields"`
	Watchers     []string  `json:"watchers"`
	ArchivedMark bool      `json:"archivedMark"`
	Archived     bool      `json:"archived"`
}

func nodeJSON(n *Node, archived bool) NodeJSON {
	f := n.Fields
	if f.Outputs == nil {
		f.Outputs = []Output{}
	}
	w := n.Watchers
	if w == nil {
		w = []string{}
	}
	return NodeJSON{ID: n.ID, Type: n.Type, Version: n.Version, Fields: f, Watchers: w, ArchivedMark: n.ArchivedMark, Archived: archived}
}

// MarshalJSON writes an Edge with a priority only if it is a Hierarchical Edge.
func (e Edge) MarshalJSON() ([]byte, error) {
	type edgeJSON struct {
		ID       uuid.UUID `json:"id"`
		Kind     EdgeKind  `json:"kind"`
		TailID   uuid.UUID `json:"tailId"`
		HeadID   uuid.UUID `json:"headId"`
		Priority *int64    `json:"priority,omitempty"`
	}
	out := edgeJSON{ID: e.ID, Kind: e.Kind, TailID: e.TailID, HeadID: e.HeadID}
	if e.Kind == Hierarchical {
		out.Priority = &e.Priority
	}
	return Marshal(out)
}

// Marshal writes v as JSON with no whitespace outside strings, and without
// the HTML escapes of json.Marshal.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// DecodeFields reads the Fields of a Node from the JSON that Marshal wrote.
func DecodeFields(data []byte) (Fields, error) {
	var f Fields
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&f); err != nil {
		return Fields{}, fmt.Errorf("decode fields: %w", err)
	}
	if f.Outputs == nil {
		f.Outputs = []Output{}
	}
	return f, nil
}
