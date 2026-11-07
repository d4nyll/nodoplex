package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/d4nyll/nodoplex/internal/changelog"
	"github.com/d4nyll/nodoplex/internal/events"
	"github.com/d4nyll/nodoplex/internal/graph"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

const keepaliveInterval = 30 * time.Second

// replayPageSize is the number of changelog entries Subscribe reads per query
// when it replays the changes a reconnecting client missed.
const replayPageSize = 1000

// Handlers implements the generated ServerInterface.
type Handlers struct {
	graphSvc *graph.Service
	clStore  *changelog.Store
	bus      *events.EventBus
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(graphSvc *graph.Service, clStore *changelog.Store, bus *events.EventBus) *Handlers {
	return &Handlers{
		graphSvc: graphSvc,
		clStore:  clStore,
		bus:      bus,
	}
}

// NewRouter registers the generated API routes on mux and returns the handler
// to serve. Every API route lives under /namespaces/{namespace}; the
// namespace is validated before any handler runs. Requests to the unprefixed
// paths, or whose namespace segment is empty, "." or "..", are rejected with
// 400 rather than falling through to 404 or a redirect.
func NewRouter(h *Handlers, mux *http.ServeMux) http.Handler {
	HandlerWithOptions(h, StdHTTPServerOptions{
		BaseRouter:  mux,
		Middlewares: []MiddlewareFunc{validateNamespace},
	})
	for _, p := range []string{"/nodes", "/nodes/", "/edges", "/changes", "/subscribe"} {
		mux.HandleFunc(p, missingNamespace)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ServeMux would redirect /namespaces//nodes, /namespaces/./nodes and
		// /namespaces/../nodes to a cleaned path.
		if rest, ok := strings.CutPrefix(r.URL.Path, "/namespaces/"); ok {
			if seg, _, _ := strings.Cut(rest, "/"); seg == "" || seg == "." || seg == ".." {
				writeError(w, http.StatusBadRequest, graph.ValidateNamespace(seg).Error())
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

func validateNamespace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := graph.ValidateNamespace(r.PathValue("namespace")); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		next.ServeHTTP(w, r)
	})
}

func missingNamespace(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusBadRequest, "namespace required: API paths start with /namespaces/{namespace}/")
}

// ── Node CRUD ────────────────────────────────────────────────────────────────

func (h *Handlers) CreateNode(w http.ResponseWriter, r *http.Request, namespace Namespace) {
	var req CreateNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	nodeType, props, err := parseCreateNodeRequest(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	node, err := h.graphSvc.CreateNode(r.Context(), namespace, graph.NodeType(nodeType), props)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	apiNode, err := genericNodeToAPI(node)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Persist change and publish event
	if err := h.publishChange(r, namespace, events.ActionCreated, node, nil); err != nil {
		writeChangeNotRecorded(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, apiNode)
}

func (h *Handlers) ListNodes(w http.ResponseWriter, r *http.Request, namespace Namespace, params ListNodesParams) {
	var nodeTypeStr *string
	if params.Type != nil {
		s := string(*params.Type)
		nodeTypeStr = &s
	}
	var statusStr *string
	if params.Status != nil {
		s := string(*params.Status)
		statusStr = &s
	}

	offset := 0
	if params.Offset != nil {
		offset = *params.Offset
	}
	limit := 50
	if params.Limit != nil {
		limit = *params.Limit
	}

	nodes, total, err := h.graphSvc.ListNodes(r.Context(), namespace, nodeTypeStr, statusStr, params.Search, offset, limit)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	apiNodes := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		an, err := genericNodeToAPI(n)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		apiNodes = append(apiNodes, an)
	}

	writeJSON(w, http.StatusOK, NodeList{Nodes: apiNodes, Total: total})
}

func (h *Handlers) GetNode(w http.ResponseWriter, r *http.Request, namespace Namespace, id NodeId) {
	node, err := h.graphSvc.GetNode(r.Context(), namespace, uuid.UUID(id))
	if err != nil {
		if errors.Is(err, graph.ErrNotFound) {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	children, err := h.graphSvc.GetChildren(r.Context(), namespace, uuid.UUID(id))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	parents, err := h.graphSvc.GetParents(r.Context(), namespace, uuid.UUID(id))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	apiNode, _ := genericNodeToAPI(node)
	apiChildren := genericNodesToAPI(children)
	apiParents := genericNodesToAPI(parents)

	writeJSON(w, http.StatusOK, NodeWithRelationships{
		Node:     apiNode,
		Children: apiChildren,
		Parents:  apiParents,
	})
}

func (h *Handlers) UpdateNode(w http.ResponseWriter, r *http.Request, namespace Namespace, id NodeId) {
	var req UpdateNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	props := updateRequestToProps(req)
	if len(props) == 0 {
		writeError(w, http.StatusBadRequest, "no fields to update")
		return
	}

	before, after, err := h.graphSvc.UpdateNode(r.Context(), namespace, uuid.UUID(id), props)
	if err != nil {
		if errors.Is(err, graph.ErrNotFound) {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	apiNode, _ := genericNodeToAPI(after)

	// Persist change and publish event
	if err := h.publishChange(r, namespace, events.ActionUpdated, after, &before); err != nil {
		writeChangeNotRecorded(w, err)
		return
	}

	writeJSON(w, http.StatusOK, apiNode)
}

func (h *Handlers) DeleteNode(w http.ResponseWriter, r *http.Request, namespace Namespace, id NodeId) {
	deleted, err := h.graphSvc.DeleteNode(r.Context(), namespace, uuid.UUID(id))
	if err != nil {
		if errors.Is(err, graph.ErrNotFound) {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Persist change and publish event
	if err := h.publishChange(r, namespace, events.ActionDeleted, graph.GenericNode{ID: deleted.ID, Type: deleted.Type}, &deleted); err != nil {
		writeChangeNotRecorded(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ── Edges ────────────────────────────────────────────────────────────────────

func (h *Handlers) CreateEdge(w http.ResponseWriter, r *http.Request, namespace Namespace) {
	var req CreateEdgeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	err := h.graphSvc.CreateEdge(
		r.Context(),
		namespace,
		uuid.UUID(req.SourceId),
		uuid.UUID(req.TargetId),
		graph.EdgeType(req.Type),
	)
	if err != nil {
		if errors.Is(err, graph.ErrNotFound) {
			writeError(w, http.StatusNotFound, "source or target node not found")
			return
		}
		if errors.Is(err, graph.ErrDependencyCycle) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, Edge{
		SourceId: req.SourceId,
		TargetId: req.TargetId,
		Type:     req.Type,
	})
}

func (h *Handlers) DeleteEdge(w http.ResponseWriter, r *http.Request, namespace Namespace) {
	var req DeleteEdgeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	err := h.graphSvc.DeleteEdge(
		r.Context(),
		namespace,
		uuid.UUID(req.SourceId),
		uuid.UUID(req.TargetId),
		graph.EdgeType(req.Type),
	)
	if err != nil {
		if errors.Is(err, graph.ErrNotFound) {
			writeError(w, http.StatusNotFound, "edge not found")
			return
		}
		if errors.Is(err, graph.ErrUnknownEdgeType) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ── Graph Traversal ──────────────────────────────────────────────────────────

func (h *Handlers) GetChildren(w http.ResponseWriter, r *http.Request, namespace Namespace, id NodeId) {
	children, err := h.graphSvc.GetChildren(r.Context(), namespace, uuid.UUID(id))
	if err != nil {
		if errors.Is(err, graph.ErrNotFound) {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, NodeArray{Nodes: genericNodesToAPI(children)})
}

func (h *Handlers) GetParents(w http.ResponseWriter, r *http.Request, namespace Namespace, id NodeId) {
	parents, err := h.graphSvc.GetParents(r.Context(), namespace, uuid.UUID(id))
	if err != nil {
		if errors.Is(err, graph.ErrNotFound) {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, NodeArray{Nodes: genericNodesToAPI(parents)})
}

func (h *Handlers) GetDependencies(w http.ResponseWriter, r *http.Request, namespace Namespace, id NodeId) {
	nodes, err := h.graphSvc.GetDependencies(r.Context(), namespace, uuid.UUID(id))
	if err != nil {
		if errors.Is(err, graph.ErrNotFound) {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, NodeArray{Nodes: genericNodesToAPI(nodes)})
}

func (h *Handlers) GetDependents(w http.ResponseWriter, r *http.Request, namespace Namespace, id NodeId) {
	nodes, err := h.graphSvc.GetDependents(r.Context(), namespace, uuid.UUID(id))
	if err != nil {
		if errors.Is(err, graph.ErrNotFound) {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, NodeArray{Nodes: genericNodesToAPI(nodes)})
}

func (h *Handlers) GetSubtree(w http.ResponseWriter, r *http.Request, namespace Namespace, id NodeId) {
	tree, err := h.graphSvc.GetSubtree(r.Context(), namespace, uuid.UUID(id))
	if err != nil {
		if errors.Is(err, graph.ErrNotFound) {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	apiTree := subtreeToAPI(tree)
	writeJSON(w, http.StatusOK, apiTree)
}

func (h *Handlers) GetLineage(w http.ResponseWriter, r *http.Request, namespace Namespace, id NodeId) {
	lineage, err := h.graphSvc.GetLineage(r.Context(), namespace, uuid.UUID(id))
	if err != nil {
		if errors.Is(err, graph.ErrNotFound) {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, NodeArray{Nodes: genericNodesToAPI(lineage)})
}

// ── Changefeed ───────────────────────────────────────────────────────────────

func (h *Handlers) ListChanges(w http.ResponseWriter, r *http.Request, namespace Namespace, params ListChangesParams) {
	if !params.Type.Valid() {
		writeError(w, http.StatusBadRequest, "invalid type")
		return
	}
	sinceID := int64(0)
	if params.Since != nil {
		sinceID = *params.Since
	}
	limit := 100
	if params.Limit != nil {
		limit = *params.Limit
	}

	entries, err := h.clStore.ListSince(r.Context(), namespace, []string{string(params.Type)}, sinceID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	changes := make([]ChangeEntry, 0, len(entries))
	for _, e := range entries {
		ce := ChangeEntry{
			ChangeId:  e.ChangeID,
			Action:    ChangeAction(e.Action),
			NodeType:  NodeType(e.NodeType),
			NodeId:    openapi_types.UUID(uuid.MustParse(e.NodeID)),
			Timestamp: e.Timestamp,
		}
		if e.Node != nil {
			var n Node
			if err := json.Unmarshal(e.Node, &n); err == nil {
				ce.Node = &n
			}
		}
		if e.Previous != nil {
			var n Node
			if err := json.Unmarshal(e.Previous, &n); err == nil {
				ce.Previous = &n
			}
		}
		changes = append(changes, ce)
	}

	resp := ChangeList{Changes: changes}
	if len(changes) > 0 {
		last := changes[len(changes)-1].ChangeId
		resp.NextSince = &last
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handlers) Subscribe(w http.ResponseWriter, r *http.Request, namespace Namespace, params SubscribeParams) {
	// Parse node type filters
	var nodeTypes []string
	if params.Type != nil {
		for _, t := range *params.Type {
			nodeTypes = append(nodeTypes, string(t))
		}
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ctx := r.Context()

	send := func(id int64, action string, v any) {
		data, err := json.Marshal(v)
		if err != nil {
			return
		}
		_, _ = w.Write([]byte("id: " + itoa64(id) + "\n"))
		_, _ = w.Write([]byte("event: node." + action + "\n"))
		_, _ = w.Write([]byte("data: " + string(data) + "\n\n"))
		flusher.Flush()
	}

	// replay sends the changelog entries after sinceID, adds their ids to sent
	// and returns the last id it sent (sinceID if it sent none).
	sent := map[int64]bool{}
	replay := func(sinceID int64) (int64, error) {
		for {
			entries, err := h.clStore.ListSince(ctx, namespace, nodeTypes, sinceID, replayPageSize)
			if err != nil {
				log.Printf("subscribe: replay in namespace %q after change %d: %v", namespace, sinceID, err)
				return sinceID, err
			}
			for _, e := range entries {
				send(e.ChangeID, e.Action, e)
				sent[e.ChangeID] = true
				sinceID = e.ChangeID
			}
			if len(entries) < replayPageSize {
				return sinceID, nil
			}
		}
	}

	// A client that reconnects with Last-Event-ID first gets the changes it
	// missed. The first replay runs before subscribing, so a long replay
	// can't overflow the subscription's buffer. The second replay sends the
	// changes recorded before the subscription started, and the live loop
	// skips every change a replay sent.
	resume := params.LastEventID != nil && h.clStore != nil
	var sinceID int64
	if resume {
		var err error
		if sinceID, err = replay(*params.LastEventID); err != nil {
			return
		}
	}
	ch, unsub := h.bus.Subscribe(namespace, nodeTypes)
	defer unsub()
	if resume {
		if _, err := replay(sinceID); err != nil {
			return
		}
	}
	// Send the headers (with status 200) if no replay has, only now that the
	// subscription exists, so a client that has the response gets every
	// change made after it.
	flusher.Flush()

	// Send keepalive ticker
	keepalive := make(chan struct{})
	go func() {
		ticker := time.NewTicker(keepaliveInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				select {
				case keepalive <- struct{}{}:
				default:
				}
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			if !sent[event.ChangeID] {
				send(event.ChangeID, string(event.Action), event)
			}
		case <-keepalive:
			_, _ = w.Write([]byte(": keepalive\n\n"))
			flusher.Flush()
		}
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// publishChange persists a change to PostgreSQL and publishes it to the event
// bus. If the changelog insert fails, it logs the change it couldn't record,
// publishes nothing and returns the error.
func (h *Handlers) publishChange(r *http.Request, namespace string, action events.ChangeAction, node graph.GenericNode, previous *graph.GenericNode) error {
	nodeJSON, _ := json.Marshal(nodeToJSONMap(node))
	var prevJSON json.RawMessage
	if previous != nil {
		prevJSON, _ = json.Marshal(nodeToJSONMap(*previous))
	}

	// For deletes, node JSON is nil
	var nodeData json.RawMessage
	if action != events.ActionDeleted {
		nodeData = nodeJSON
	}

	now := time.Now()

	entry := changelog.Entry{
		Namespace: namespace,
		Action:    string(action),
		NodeType:  string(node.Type),
		NodeID:    node.ID.String(),
		Node:      nodeData,
		Previous:  prevJSON,
	}

	var changeID int64
	if h.clStore != nil {
		var err error
		// The node change is already saved, so a client that disconnects now
		// must not cancel the insert that records it.
		changeID, err = h.clStore.Append(context.WithoutCancel(r.Context()), entry)
		if err != nil {
			log.Printf("changelog: %s of %s %s in namespace %q not recorded: %v", action, node.Type, node.ID, namespace, err)
			return err
		}
	}

	h.bus.Publish(events.ChangeEvent{
		ChangeID:  changeID,
		Namespace: namespace,
		Action:    action,
		NodeType:  string(node.Type),
		NodeID:    node.ID,
		Node:      nodeData,
		Previous:  prevJSON,
		Timestamp: now,
	})
	return nil
}

// writeChangeNotRecorded responds 500 to a write whose node change succeeded
// in Neo4j but whose changelog insert failed.
func writeChangeNotRecorded(w http.ResponseWriter, err error) {
	writeError(w, http.StatusInternalServerError, "the node change was saved but not recorded in the changelog: "+err.Error())
}

func nodeToJSONMap(n graph.GenericNode) map[string]any {
	m := map[string]any{
		"id":   n.ID.String(),
		"type": string(n.Type),
	}
	if n.Title != nil {
		m["title"] = *n.Title
	}
	if n.Description != nil {
		m["description"] = *n.Description
	}
	if n.Priority != nil {
		m["priority"] = *n.Priority
	}
	if n.TargetDate != nil {
		m["targetDate"] = *n.TargetDate
	}
	if n.AcceptanceCriteria != nil {
		m["acceptanceCriteria"] = *n.AcceptanceCriteria
	}
	if n.Status != nil {
		m["status"] = *n.Status
	}
	if n.Repo != nil {
		m["repo"] = *n.Repo
	}
	if n.RefType != nil {
		m["refType"] = *n.RefType
	}
	if n.Ref != nil {
		m["ref"] = *n.Ref
	}
	if n.URL != nil {
		m["url"] = *n.URL
	}
	if n.Message != nil {
		m["message"] = *n.Message
	}
	return m
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, Error{Message: message})
}

func itoa64(i int64) string {
	return fmt.Sprintf("%d", i)
}
