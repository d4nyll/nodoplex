package api

import (
	"log"
	"net/http"

	"github.com/d4nyll/nodoplex/internal/changefeed"
	"github.com/d4nyll/nodoplex/internal/graph"
	"github.com/google/uuid"
)

// This file reads the members of the input of each operation, with the
// checks before step 1 of the base specification (checks 3 to 5), and runs
// the operation.

// ── Reading members ─────────────────────────────────────────────────────────

// queryValue returns the one value of a query parameter that isn't an array,
// or nil.
func (rq *request) queryValue(name string) *string {
	if v, ok := rq.query[name]; ok {
		return &v[0]
	}
	return nil
}

func (rq *request) queryBool(name string) (bool, error) {
	s := rq.queryValue(name)
	if s == nil {
		return false, nil
	}
	b, ok := textBool(*s)
	if !ok {
		return false, invalid("%s must be true or false", name)
	}
	return b, nil
}

// queryInt returns the value of an integer query parameter, or nil.
func (rq *request) queryInt(name string, lo, hi int64) (*int64, error) {
	s := rq.queryValue(name)
	if s == nil {
		return nil, nil
	}
	i, ok := textInt(*s)
	if !ok || i < lo || i > hi {
		return nil, invalid("%s must be an integer from %d to %d", name, lo, hi)
	}
	return &i, nil
}

const maxInt = 1<<63 - 1
const minInt = -1 << 63

// requiredQueryInt returns the value of a required integer query parameter.
func (rq *request) requiredQueryInt(name string, lo, hi int64) (int64, error) {
	v, err := rq.queryInt(name, lo, hi)
	return required(name, v, err)
}

// requiredBodyInt returns the value of a required integer body member.
func (rq *request) requiredBodyInt(name string) (int64, error) {
	v, err := rq.bodyInt(name)
	return required(name, v, err)
}

func required(name string, v *int64, err error) (int64, error) {
	if err == nil && v == nil {
		err = invalid("%s is required", name)
	}
	if err != nil {
		return 0, err
	}
	return *v, nil
}

// queryList reads an array query parameter. It returns nil if the query
// doesn't give the parameter.
func queryList[T any](rq *request, name string, parse func(string) (T, bool)) ([]T, error) {
	values, ok := rq.query[name]
	if !ok {
		return nil, nil
	}
	out := make([]T, len(values))
	for i, s := range values {
		if out[i], ok = parse(s); !ok {
			return nil, invalid("the query parameter %s has the value %q, which isn't valid", name, s)
		}
	}
	return out, nil
}

// optUserID reads a (UserId, nullable): the empty text is no value.
func optUserID(s string) (*string, bool) {
	if s == "" {
		return nil, true
	}
	return &s, true
}

func (rq *request) pathID() (uuid.UUID, error) {
	id, ok := parseUUID(rq.id)
	if !ok {
		return uuid.Nil, invalid("%q isn't a UUID", rq.id)
	}
	return id, nil
}

// bodyInt returns the value of an integer body member, or nil.
func (rq *request) bodyInt(name string) (*int64, error) {
	v, ok := rq.body[name]
	if !ok {
		return nil, nil
	}
	i, ok := jsonInt(v)
	if !ok {
		return nil, invalid("%s must be an integer", name)
	}
	return &i, nil
}

func (rq *request) bodyFields(atLeastOne bool) (graph.FieldsInput, error) {
	v, ok := rq.body["fields"]
	if !ok {
		return nil, invalid("fields is required")
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, invalid("fields must be an object")
	}
	if atLeastOne && len(obj) == 0 {
		return nil, invalid("fields must name at least 1 Field")
	}
	return graph.FieldsInput(obj), nil
}

func (rq *request) filterGroups() ([]changefeed.Group, error) {
	s := rq.queryValue("filterGroups")
	if s == nil {
		return nil, nil
	}
	v, err := decodeJSON([]byte(*s))
	if err != nil {
		return nil, err
	}
	return changefeed.ParseGroups(v)
}

// ── Products ────────────────────────────────────────────────────────────────

func createProduct(h *Handler, rq *request) (int, any, error) {
	fields, err := rq.bodyFields(false)
	if err != nil {
		return 0, nil, err
	}
	node, err := h.store.CreateProduct(rq.r.Context(), rq.slug, rq.userID, fields)
	return http.StatusCreated, node, err
}

func listProducts(h *Handler, rq *request) (int, any, error) {
	archived, err := queryList(rq, "archived", textBool)
	if err != nil {
		return 0, nil, err
	}
	if archived == nil {
		archived = []bool{false}
	}
	products, err := h.store.ListProducts(rq.r.Context(), archived)
	return http.StatusOK, map[string]any{"products": products}, err
}

// ── Nodes ───────────────────────────────────────────────────────────────────

func createNode(h *Handler, rq *request) (int, any, error) {
	s, _ := rq.body["type"].(string)
	t := graph.NodeType(s)
	if _, ok := rq.body["type"]; ok && !t.Valid() {
		return 0, nil, invalid("type must be a Node Type")
	}
	var parents []graph.ParentRef
	if v, ok := rq.body["parents"]; ok {
		list, ok := v.([]any)
		if !ok || len(list) == 0 {
			return 0, nil, invalid("parents must be an array of at least 1 ParentRef")
		}
		for _, item := range list {
			obj, ok := item.(map[string]any)
			if !ok {
				return 0, nil, invalid("each value of parents must be a ParentRef object")
			}
			var p graph.ParentRef
			hasID := false
			for name, mv := range obj {
				switch name {
				case "id":
					s, _ := mv.(string)
					if p.ID, hasID = parseUUID(s); !hasID {
						return 0, nil, invalid("the id of a ParentRef must be a UUID")
					}
				case "priority":
					i, ok := jsonInt(mv)
					if !ok {
						return 0, nil, invalid("the priority of a ParentRef must be an integer")
					}
					p.Priority = &i
				default:
					return 0, nil, invalid("a ParentRef has no member %q", name)
				}
			}
			if !hasID {
				return 0, nil, invalid("each ParentRef must give id")
			}
			parents = append(parents, p)
		}
	}
	fields, err := rq.bodyFields(false)
	if err != nil {
		return 0, nil, err
	}
	if t == "" || parents == nil {
		return 0, nil, invalid("type and parents are required")
	}
	var id uuid.UUID
	tx, err := h.store.Write(rq.r.Context(), rq.slug, rq.userID, func(tx *graph.Tx) (err error) {
		id, err = graph.CreateNode(tx, t, parents, fields)
		return err
	})
	if err != nil {
		return 0, nil, err
	}
	return http.StatusCreated, tx.View.NodeJSON(id), nil
}

func getNode(h *Handler, rq *request) (int, any, error) {
	id, err := rq.pathID()
	if err != nil {
		return 0, nil, err
	}
	include, err := rq.queryBool("includeArchived")
	if err != nil {
		return 0, nil, err
	}
	var node graph.NodeJSON
	err = h.store.Read(rq.r.Context(), rq.slug, func(v *graph.View) (err error) {
		node, err = graph.GetNode(v, id, include)
		return err
	})
	return http.StatusOK, node, err
}

func listNodes(h *Handler, rq *request) (int, any, error) {
	var in graph.ListInput
	var err error
	nodeType := func(s string) (graph.NodeType, bool) { return graph.NodeType(s), graph.NodeType(s).Valid() }
	status := func(s string) (graph.Status, bool) { return graph.Status(s), graph.Status(s).Valid() }
	ids := []struct {
		name string
		dest *[]uuid.UUID
	}{
		{"childrenOf", &in.ChildrenOf}, {"descendantsOf", &in.DescendantsOf}, {"parentsOf", &in.ParentsOf},
		{"ancestorsOf", &in.AncestorsOf}, {"dependentsOf", &in.DependentsOf}, {"prerequisitesOf", &in.PrerequisitesOf},
	}
	if in.Types, err = queryList(rq, "type", nodeType); err != nil {
		return 0, nil, err
	}
	if in.Statuses, err = queryList(rq, "status", status); err != nil {
		return 0, nil, err
	}
	if in.Assignees, err = queryList(rq, "assignee", optUserID); err != nil {
		return 0, nil, err
	}
	if in.Watchers, err = queryList(rq, "watcher", optUserID); err != nil {
		return 0, nil, err
	}
	if in.Archived, err = queryList(rq, "archived", textBool); err != nil {
		return 0, nil, err
	}
	if in.Blocked, err = queryList(rq, "blocked", textBool); err != nil {
		return 0, nil, err
	}
	for _, f := range ids {
		if *f.dest, err = queryList(rq, f.name, parseUUID); err != nil {
			return 0, nil, err
		}
	}
	if s := rq.queryValue("search"); s != nil {
		if *s == "" {
			return 0, nil, invalid("search must have at least 1 character")
		}
		in.Search = s
	}
	if s := rq.queryValue("sort"); s != nil {
		if *s != "lastUpdated" && *s != "priority" {
			return 0, nil, invalid("sort must be lastUpdated or priority")
		}
		in.SortPriority = *s == "priority"
	}
	if s := rq.queryValue("direction"); s != nil {
		if *s != "descending" && *s != "ascending" {
			return 0, nil, invalid("direction must be descending or ascending")
		}
		in.Direction = *s
	}
	if in.StartAfterKey, err = rq.queryInt("startAfterKey", minInt, maxInt); err != nil {
		return 0, nil, err
	}
	if s := rq.queryValue("startAfterId"); s != nil {
		id, ok := parseUUID(*s)
		if !ok {
			return 0, nil, invalid("startAfterId must be a UUID")
		}
		in.StartAfterID = &id
	}
	limit, err := rq.queryInt("limit", 1, 200)
	if err != nil {
		return 0, nil, err
	}
	if limit != nil {
		in.Limit = int(*limit)
	}
	var page graph.NodePage
	err = h.store.Read(rq.r.Context(), rq.slug, func(v *graph.View) (err error) {
		page, err = graph.ListNodes(v, in)
		return err
	})
	return http.StatusOK, page, err
}

func getNextWork(h *Handler, rq *request) (int, any, error) {
	var next *graph.NodeJSON
	err := h.store.Read(rq.r.Context(), rq.slug, func(v *graph.View) error {
		next = graph.NextWork(v)
		return nil
	})
	return http.StatusOK, struct {
		Node *graph.NodeJSON `json:"node,omitempty"`
	}{next}, err
}

// writeNode runs a write to the Node in the path, and returns the Node.
func writeNode(h *Handler, rq *request, id uuid.UUID, steps func(tx *graph.Tx) error) (int, any, error) {
	tx, err := h.store.Write(rq.r.Context(), rq.slug, rq.userID, steps)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, tx.View.NodeJSON(id), nil
}

func updateNode(h *Handler, rq *request) (int, any, error) {
	id, err := rq.pathID()
	if err != nil {
		return 0, nil, err
	}
	version, err := rq.bodyInt("version")
	if err != nil {
		return 0, nil, err
	}
	fields, err := rq.bodyFields(true)
	if err != nil {
		return 0, nil, err
	}
	return writeNode(h, rq, id, func(tx *graph.Tx) error { return graph.UpdateNode(tx, id, version, fields) })
}

func archiveNode(mark bool) func(h *Handler, rq *request) (int, any, error) {
	return func(h *Handler, rq *request) (int, any, error) {
		id, err := rq.pathID()
		if err != nil {
			return 0, nil, err
		}
		version, err := rq.requiredBodyInt("version")
		if err != nil {
			return 0, nil, err
		}
		return writeNode(h, rq, id, func(tx *graph.Tx) error { return graph.SetArchivedMark(tx, id, version, mark) })
	}
}

func deleteNode(h *Handler, rq *request) (int, any, error) {
	id, err := rq.pathID()
	if err != nil {
		return 0, nil, err
	}
	version, err := rq.requiredQueryInt("version", minInt, maxInt)
	if err != nil {
		return 0, nil, err
	}
	_, err = h.store.Write(rq.r.Context(), rq.slug, rq.userID, func(tx *graph.Tx) error { return graph.DeleteNode(tx, id, version) })
	return http.StatusNoContent, nil, err
}

func watchNode(watch bool) func(h *Handler, rq *request) (int, any, error) {
	return func(h *Handler, rq *request) (int, any, error) {
		id, err := rq.pathID()
		if err != nil {
			return 0, nil, err
		}
		return writeNode(h, rq, id, func(tx *graph.Tx) error { return graph.SetWatcher(tx, id, rq.userID, watch) })
	}
}

// ── Edges ───────────────────────────────────────────────────────────────────

func createEdge(h *Handler, rq *request) (int, any, error) {
	var in graph.CreateEdgeInput
	kind, _ := rq.body["kind"].(string)
	in.Kind = graph.EdgeKind(kind)
	if _, ok := rq.body["kind"]; ok && in.Kind != graph.Hierarchical && in.Kind != graph.Dependency {
		return 0, nil, invalid("kind must be hierarchical or dependency")
	}
	for _, m := range []struct {
		name string
		dest *uuid.UUID
	}{{"tailId", &in.TailID}, {"headId", &in.HeadID}} {
		v, ok := rq.body[m.name]
		if !ok {
			return 0, nil, invalid("%s is required", m.name)
		}
		s, _ := v.(string)
		if *m.dest, ok = parseUUID(s); !ok {
			return 0, nil, invalid("%s must be a UUID", m.name)
		}
	}
	var err error
	if in.Priority, err = rq.bodyInt("priority"); err != nil {
		return 0, nil, err
	}
	if in.Version, err = rq.requiredBodyInt("version"); err != nil {
		return 0, nil, err
	}
	if in.Kind == "" {
		return 0, nil, invalid("kind is required")
	}
	var id uuid.UUID
	tx, err := h.store.Write(rq.r.Context(), rq.slug, rq.userID, func(tx *graph.Tx) (err error) {
		id, err = graph.CreateEdge(tx, in)
		return err
	})
	if err != nil {
		return 0, nil, err
	}
	return http.StatusCreated, *tx.G.Edges[id], nil
}

func getEdge(h *Handler, rq *request) (int, any, error) {
	id, err := rq.pathID()
	if err != nil {
		return 0, nil, err
	}
	include, err := rq.queryBool("includeArchived")
	if err != nil {
		return 0, nil, err
	}
	var edge graph.Edge
	err = h.store.Read(rq.r.Context(), rq.slug, func(v *graph.View) error {
		e, err := graph.GetEdge(v, id, include)
		if err == nil {
			edge = *e
		}
		return err
	})
	return http.StatusOK, edge, err
}

func getEdges(from bool) func(h *Handler, rq *request) (int, any, error) {
	return func(h *Handler, rq *request) (int, any, error) {
		id, err := rq.pathID()
		if err != nil {
			return 0, nil, err
		}
		include, err := rq.queryBool("includeArchived")
		if err != nil {
			return 0, nil, err
		}
		var edges []graph.Edge
		err = h.store.Read(rq.r.Context(), rq.slug, func(v *graph.View) (err error) {
			edges, err = graph.GetEdges(v, id, include, from)
			return err
		})
		return http.StatusOK, map[string]any{"edges": edges}, err
	}
}

func getEdgeEnd(head bool) func(h *Handler, rq *request) (int, any, error) {
	return func(h *Handler, rq *request) (int, any, error) {
		id, err := rq.pathID()
		if err != nil {
			return 0, nil, err
		}
		include, err := rq.queryBool("includeArchived")
		if err != nil {
			return 0, nil, err
		}
		var node graph.NodeJSON
		err = h.store.Read(rq.r.Context(), rq.slug, func(v *graph.View) error {
			e, err := graph.GetEdge(v, id, include)
			if err != nil {
				return err
			}
			end := e.TailID
			if head {
				end = e.HeadID
			}
			node = v.NodeJSON(end)
			return nil
		})
		return http.StatusOK, node, err
	}
}

func updateEdge(h *Handler, rq *request) (int, any, error) {
	id, err := rq.pathID()
	if err != nil {
		return 0, nil, err
	}
	priority, err := rq.requiredBodyInt("priority")
	if err != nil {
		return 0, nil, err
	}
	version, err := rq.requiredBodyInt("version")
	if err != nil {
		return 0, nil, err
	}
	tx, err := h.store.Write(rq.r.Context(), rq.slug, rq.userID, func(tx *graph.Tx) error { return graph.UpdateEdge(tx, id, priority, version) })
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, *tx.G.Edges[id], nil
}

func deleteEdge(h *Handler, rq *request) (int, any, error) {
	id, err := rq.pathID()
	if err != nil {
		return 0, nil, err
	}
	version, err := rq.requiredQueryInt("version", minInt, maxInt)
	if err != nil {
		return 0, nil, err
	}
	_, err = h.store.Write(rq.r.Context(), rq.slug, rq.userID, func(tx *graph.Tx) error { return graph.DeleteEdge(tx, id, version) })
	return http.StatusNoContent, nil, err
}

// ── Changefeed ──────────────────────────────────────────────────────────────

func getLatestChangeID(h *Handler, rq *request) (int, any, error) {
	latest, err := h.store.LatestChangeID(rq.r.Context(), rq.slug)
	return http.StatusOK, map[string]int64{"latestChangeId": latest}, err
}

func poll(h *Handler, rq *request) (int, any, error) {
	start, err := rq.requiredQueryInt("startChangeId", 0, maxInt)
	if err != nil {
		return 0, nil, err
	}
	limit, err := rq.requiredQueryInt("limit", 1, 1000)
	if err != nil {
		return 0, nil, err
	}
	groups, err := rq.filterGroups()
	if err != nil {
		return 0, nil, err
	}
	entries, latest, err := h.store.Poll(rq.r.Context(), rq.slug, start, int(limit), groups)
	return http.StatusOK, map[string]any{"changeEntries": entries, "latestChangeId": latest}, err
}

// ── Health checks ───────────────────────────────────────────────────────────

func livez(h *Handler, rq *request) (int, any, error) { return http.StatusOK, nil, nil }

func startupz(h *Handler, rq *request) (int, any, error) {
	if !h.store.Started() {
		return 0, nil, graph.Refuse(graph.CodeUnavailable, "the server hasn't finished starting")
	}
	return http.StatusOK, nil, nil
}

func readyz(h *Handler, rq *request) (int, any, error) {
	if err := h.store.Ready(rq.r.Context()); err != nil {
		log.Printf("readiness: %v", err)
		return 0, nil, graph.Refuse(graph.CodeUnavailable, "the server isn't ready")
	}
	return http.StatusOK, nil, nil
}
