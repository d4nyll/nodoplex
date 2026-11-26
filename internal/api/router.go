// Package api serves the operations over HTTP, as the extended specification
// gives. generated.go holds the models and the embedded copy of
// api/openapi.yaml that oapi-codegen writes. The tests check the routes
// against that copy.
package api

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"slices"
	"strings"

	"github.com/d4nyll/nodoplex/internal/graph"
	"github.com/d4nyll/nodoplex/internal/store"
)

// operation is one operation, at its method and its path. query names the
// members that the server reads from the query (I3), and arrays names those
// that the query gives one time for each value (I7). body names the body
// members (I4). health is true for a health check (L1 to L4).
type operation struct {
	name   string
	method string
	path   string
	query  []string
	arrays []string
	body   []string
	health bool
	handle func(h *Handler, rq *request) (status int, out any, err error)
}

var listFilters = []string{"type", "status", "assignee", "watcher", "archived", "blocked",
	"childrenOf", "descendantsOf", "parentsOf", "ancestorsOf", "dependentsOf", "prerequisitesOf"}

var operations = []*operation{
	{name: "CreateProduct", method: http.MethodPut, path: "/products/{slug}", body: []string{"fields"}, handle: createProduct},
	{name: "ListProducts", method: http.MethodGet, path: "/products", query: []string{"archived"}, arrays: []string{"archived"}, handle: listProducts},
	{name: "CreateNode", method: http.MethodPost, path: "/products/{slug}/nodes", body: []string{"type", "parents", "fields"}, handle: createNode},
	{name: "GetNode", method: http.MethodGet, path: "/products/{slug}/nodes/{id}", query: []string{"includeArchived"}, handle: getNode},
	{name: "ListNodes", method: http.MethodGet, path: "/products/{slug}/nodes",
		query:  append(slices.Clone(listFilters), "search", "sort", "direction", "startAfterKey", "startAfterId", "limit"),
		arrays: listFilters, handle: listNodes},
	{name: "GetNextWork", method: http.MethodGet, path: "/products/{slug}/next-work", handle: getNextWork},
	{name: "UpdateNode", method: http.MethodPatch, path: "/products/{slug}/nodes/{id}", body: []string{"version", "fields"}, handle: updateNode},
	{name: "ArchiveNode", method: http.MethodPost, path: "/products/{slug}/nodes/{id}/archive", body: []string{"version"}, handle: archiveNode(true)},
	{name: "UnarchiveNode", method: http.MethodPost, path: "/products/{slug}/nodes/{id}/unarchive", body: []string{"version"}, handle: archiveNode(false)},
	{name: "DeleteNode", method: http.MethodDelete, path: "/products/{slug}/nodes/{id}", query: []string{"version"}, handle: deleteNode},
	{name: "WatchNode", method: http.MethodPost, path: "/products/{slug}/nodes/{id}/watch", handle: watchNode(true)},
	{name: "UnwatchNode", method: http.MethodPost, path: "/products/{slug}/nodes/{id}/unwatch", handle: watchNode(false)},
	{name: "CreateEdge", method: http.MethodPost, path: "/products/{slug}/edges", body: []string{"kind", "tailId", "headId", "priority", "version"}, handle: createEdge},
	{name: "GetEdge", method: http.MethodGet, path: "/products/{slug}/edges/{id}", query: []string{"includeArchived"}, handle: getEdge},
	{name: "GetEdgesFrom", method: http.MethodGet, path: "/products/{slug}/nodes/{id}/edges-from", query: []string{"includeArchived"}, handle: getEdges(true)},
	{name: "GetEdgesTo", method: http.MethodGet, path: "/products/{slug}/nodes/{id}/edges-to", query: []string{"includeArchived"}, handle: getEdges(false)},
	{name: "GetHeadNode", method: http.MethodGet, path: "/products/{slug}/edges/{id}/head", query: []string{"includeArchived"}, handle: getEdgeEnd(true)},
	{name: "GetTailNode", method: http.MethodGet, path: "/products/{slug}/edges/{id}/tail", query: []string{"includeArchived"}, handle: getEdgeEnd(false)},
	{name: "UpdateEdge", method: http.MethodPatch, path: "/products/{slug}/edges/{id}", body: []string{"priority", "version"}, handle: updateEdge},
	{name: "DeleteEdge", method: http.MethodDelete, path: "/products/{slug}/edges/{id}", query: []string{"version"}, handle: deleteEdge},
	{name: "GetLatestChangeId", method: http.MethodGet, path: "/products/{slug}/latest-change-id", handle: getLatestChangeID},
	{name: "Poll", method: http.MethodGet, path: "/products/{slug}/poll", query: []string{"startChangeId", "limit", "filterGroups"}, handle: poll},
	{name: "Subscribe", method: http.MethodGet, path: "/products/{slug}/subscribe", query: []string{"startChangeId", "filterGroups", "userId"}, handle: subscribe},
	{name: "Livez", method: http.MethodGet, path: "/livez", health: true, handle: livez},
	{name: "Startupz", method: http.MethodGet, path: "/startupz", health: true, handle: startupz},
	{name: "Readyz", method: http.MethodGet, path: "/readyz", health: true, handle: readyz},
}

// Handler serves the operations.
type Handler struct {
	store *store.Store
	// ctx ends each open subscription when the server shuts down.
	ctx context.Context
}

func NewHandler(ctx context.Context, s *store.Store) *Handler { return &Handler{store: s, ctx: ctx} }

// request holds the input of a request after the checks of the extended
// specification, and the checks before step 1 that the base specification
// gives for the user ID and the slug.
type request struct {
	r      *http.Request
	w      http.ResponseWriter
	slug   string
	id     string
	query  map[string][]string
	body   map[string]any
	userID string
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Expose-Headers", "Nodoplex-Error-Code")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, POST, PATCH, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Last-Event-ID, Nodoplex-User-Id")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	segments := strings.Split(strings.TrimPrefix(rawPath(r), "/"), "/")
	var matches []*operation
	var params map[string]string
	for _, op := range operations {
		if p, ok := matchPath(op.path, segments); ok {
			matches, params = append(matches, op), p
		}
	}
	if len(matches) == 0 {
		writeError(w, graph.Refuse(graph.CodeNotFound, "no operation has the path of the request"))
		return
	}
	method := r.Method
	if method == http.MethodHead {
		method = http.MethodGet
	}
	i := slices.IndexFunc(matches, func(op *operation) bool { return op.method == method })
	if i < 0 {
		var allow []string
		for _, op := range matches {
			allow = append(allow, op.method)
			if op.method == http.MethodGet {
				allow = append(allow, http.MethodHead)
			}
		}
		w.Header().Set("Allow", strings.Join(allow, ", "))
		writeError(w, graph.Refuse(graph.CodeMethodNotAllowed, "the path doesn't serve the method %s", r.Method))
		return
	}
	op := matches[i]
	rq := &request{r: r, w: w}
	if !op.health { // L4: a health check skips the other checks, and ignores the query
		var err error
		if rq, err = checkRequest(op, w, r, params); err != nil {
			writeError(w, err)
			return
		}
	}
	status, out, err := op.handle(h, rq)
	switch {
	case err != nil:
		writeError(w, err)
	case status == 0: // the handler wrote the response
	case out == nil:
		w.WriteHeader(status)
	default:
		writeJSON(w, status, out)
	}
}

// rawPath returns the path of the request as the client sent it, before
// percent-decoding (H3).
func rawPath(r *http.Request) string {
	if strings.HasPrefix(r.RequestURI, "/") {
		path, _, _ := strings.Cut(r.RequestURI, "?")
		return path
	}
	return r.URL.EscapedPath()
}

// matchPath compares the path of an operation with the segments of a request
// one segment at a time. A path parameter matches any one segment (H4).
func matchPath(path string, segments []string) (map[string]string, bool) {
	pattern := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(pattern) != len(segments) {
		return nil, false
	}
	params := map[string]string{}
	for i, p := range pattern {
		if strings.HasPrefix(p, "{") {
			params[strings.Trim(p, "{}")] = segments[i]
		} else if p != segments[i] {
			return nil, false
		}
	}
	return params, true
}

// checkRequest makes checks 4 to 13 of the extended specification, and then
// the checks of the user ID and of the slug (checks 1 and 2 of the base
// specification). Each check refuses with invalid_request, so their order
// changes only the message.
func checkRequest(op *operation, w http.ResponseWriter, r *http.Request, params map[string]string) (*request, error) {
	rq := &request{r: r, w: w}
	var ok bool
	if rq.slug, ok = percentDecode(params["slug"]); !ok {
		return nil, invalid("the slug in the path isn't valid percent-encoding")
	}
	if rq.id, ok = percentDecode(params["id"]); !ok {
		return nil, invalid("the id in the path isn't valid percent-encoding")
	}
	query, err := parseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, err
	}
	for name, values := range query {
		if !slices.Contains(op.query, name) {
			return nil, invalid("the operation %s has no query parameter %q", op.name, name)
		}
		if len(values) > 1 && !slices.Contains(op.arrays, name) {
			return nil, invalid("the query parameter %q occurs more than one time", name)
		}
	}
	rq.query = query
	headerUserID, hasHeader := r.Header["Nodoplex-User-Id"]
	if hasHeader {
		if rq.userID, ok = percentDecode(headerUserID[0]); !ok {
			return nil, invalid("the header Nodoplex-User-Id isn't valid percent-encoding")
		}
	}
	if values, ok := query["filterGroups"]; ok && len(values[0]) > maxInputBytes {
		return nil, invalid("filterGroups is larger than %d bytes", maxInputBytes)
	}
	if op.method == http.MethodPut || op.method == http.MethodPost || op.method == http.MethodPatch {
		data, err := io.ReadAll(io.LimitReader(r.Body, maxInputBytes+1))
		if err != nil {
			return nil, invalid("the server can't read the body")
		}
		if len(data) > maxInputBytes {
			return nil, invalid("the body is larger than %d bytes", maxInputBytes)
		}
		if len(data) == 0 {
			data = []byte("{}")
		}
		v, err := decodeJSON(data)
		if err != nil {
			return nil, err
		}
		if rq.body, ok = v.(map[string]any); !ok {
			return nil, invalid("the body must be a JSON object")
		}
		for name := range rq.body {
			if !slices.Contains(op.body, name) {
				return nil, invalid("the operation %s has no body member %q", op.name, name)
			}
		}
	}
	if !hasHeader && op.name == "Subscribe" && query["userId"] != nil {
		rq.userID = query["userId"][0]
	}
	if !graph.ValidUserID(rq.userID) {
		return nil, invalid("the request must give a user ID of at least 1 character in the header Nodoplex-User-Id")
	}
	if strings.Contains(op.path, "{slug}") && !validSlug(rq.slug) {
		return nil, invalid("%q isn't a valid slug", rq.slug)
	}
	return rq, nil
}

// ── Responses ───────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := graph.Marshal(v)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(body)
}

var statusOf = map[string]int{
	graph.CodeInvalidRequest:   http.StatusBadRequest,
	graph.CodeNotFound:         http.StatusNotFound,
	graph.CodeMethodNotAllowed: http.StatusMethodNotAllowed,
	graph.CodeUnavailable:      http.StatusServiceUnavailable,
	graph.CodeInternalError:    http.StatusInternalServerError,
}

// asRefusal returns the refusal that err holds, or internal_error. It logs an
// error that has no code, and keeps its text out of the refusal (E2).
func asRefusal(err error) *graph.Error {
	var refusal *graph.Error
	if errors.As(err, &refusal) {
		return refusal
	}
	log.Printf("internal error: %v", err)
	return graph.Refuse(graph.CodeInternalError, "the server can't complete the operation")
}

func writeError(w http.ResponseWriter, err error) {
	refusal := asRefusal(err)
	status, ok := statusOf[refusal.Code]
	if !ok {
		status = http.StatusConflict
	}
	w.Header().Set("Nodoplex-Error-Code", refusal.Code)
	writeJSON(w, status, refusal)
}
