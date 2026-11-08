package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/d4nyll/nodoplex/internal/changelog"
	"github.com/d4nyll/nodoplex/internal/config"
	"github.com/d4nyll/nodoplex/internal/db"
	"github.com/d4nyll/nodoplex/internal/events"
	"github.com/d4nyll/nodoplex/internal/graph"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
)

// Integration tests run the full HTTP stack against the Neo4j and PostgreSQL
// instances in NEO4J_URI and POSTGRES_URL, and are skipped unless
// NODOPLEX_INTEGRATION=1. Each test works in fresh random namespaces.

type integrationEnv struct {
	ts     *httptest.Server
	driver neo4j.DriverWithContext
	pool   *pgxpool.Pool
}

func newIntegrationEnv(t *testing.T) *integrationEnv {
	t.Helper()
	if os.Getenv("NODOPLEX_INTEGRATION") != "1" {
		t.Skip("set NODOPLEX_INTEGRATION=1 to run against Neo4j and PostgreSQL")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	ctx := context.Background()
	driver, err := db.NewNeo4jDriver(ctx, cfg.Neo4jURI, cfg.Neo4jUser, cfg.Neo4jPassword)
	if err != nil {
		t.Fatalf("neo4j: %v", err)
	}
	t.Cleanup(func() { driver.Close(ctx) })
	if err := db.RunNeo4jMigrations(ctx, driver); err != nil {
		t.Fatalf("neo4j migrations: %v", err)
	}
	pool, err := db.NewPostgresPool(ctx, cfg.PostgresURL)
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := changelog.RunMigrations(ctx, pool); err != nil {
		t.Fatalf("postgres migrations: %v", err)
	}

	h := NewHandlers(graph.NewService(graph.NewRepository(driver)), changelog.NewStore(pool), events.NewEventBus())
	ts := httptest.NewServer(NewRouter(h, http.NewServeMux()))
	t.Cleanup(ts.Close)
	return &integrationEnv{ts: ts, driver: driver, pool: pool}
}

func newNamespace() string { return "it-" + uuid.NewString() }

// call sends a request and returns the status code and body.
func (e *integrationEnv) call(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.ts.URL+path, r)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func (e *integrationEnv) createNode(t *testing.T, ns string, body map[string]any) string {
	t.Helper()
	status, b := e.call(t, http.MethodPost, "/namespaces/"+ns+"/nodes", body)
	if status != http.StatusCreated {
		t.Fatalf("create node in %s: %d %s", ns, status, b)
	}
	var n struct{ Id string }
	json.Unmarshal(b, &n)
	return n.Id
}

func (e *integrationEnv) createEdge(t *testing.T, ns, source, target, edgeType string) (int, []byte) {
	t.Helper()
	return e.call(t, http.MethodPost, "/namespaces/"+ns+"/edges",
		map[string]string{"sourceId": source, "targetId": target, "type": edgeType})
}

func (e *integrationEnv) mustCreateEdge(t *testing.T, ns, source, target, edgeType string) {
	t.Helper()
	if status, b := e.createEdge(t, ns, source, target, edgeType); status != http.StatusCreated {
		t.Fatalf("create %s edge in %s: %d %s", edgeType, ns, status, b)
	}
}

// nodeIDs returns the ids in a NodeArray or NodeList response body.
func nodeIDs(t *testing.T, body []byte) []string {
	t.Helper()
	var resp struct{ Nodes []struct{ Id string } }
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	ids := []string{}
	for _, n := range resp.Nodes {
		ids = append(ids, n.Id)
	}
	return ids
}

func sameIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]int{}
	for _, id := range got {
		seen[id]++
	}
	for _, id := range want {
		seen[id]--
	}
	for _, c := range seen {
		if c != 0 {
			return false
		}
	}
	return true
}

// TestIntegration_NamespaceIsolation checks that every endpoint treats a node
// in another namespace exactly like a node that doesn't exist.
func TestIntegration_NamespaceIsolation(t *testing.T) {
	e := newIntegrationEnv(t)
	nsA, nsB := newNamespace(), newNamespace()

	vision := e.createNode(t, nsA, map[string]any{"type": "vision", "title": "A vision"})
	goal := e.createNode(t, nsA, map[string]any{"type": "goal", "title": "A goal"})
	e.mustCreateEdge(t, nsA, vision, goal, "BREAKS_DOWN_TO")
	visionB := e.createNode(t, nsB, map[string]any{"type": "vision", "title": "B vision"})
	goalB := e.createNode(t, nsB, map[string]any{"type": "goal", "title": "B goal"})

	// Each request is sent in namespace B twice: once with A's ids and once
	// with random ids. Status and body must match.
	type req struct {
		method, path string
		body         func(source, target string) any
	}
	nodeReqs := []req{
		{http.MethodGet, "/nodes/%s", nil},
		{http.MethodPatch, "/nodes/%s", func(string, string) any { return map[string]string{"title": "hijacked"} }},
		{http.MethodGet, "/nodes/%s/children", nil},
		{http.MethodGet, "/nodes/%s/parents", nil},
		{http.MethodGet, "/nodes/%s/subtree", nil},
		{http.MethodGet, "/nodes/%s/lineage", nil},
		{http.MethodDelete, "/nodes/%s", nil},
	}
	for _, id := range []string{vision, goal} {
		for _, r := range nodeReqs {
			var body any
			if r.body != nil {
				body = r.body("", "")
			}
			path := "/namespaces/" + nsB + strings.Replace(r.path, "%s", id, 1)
			gotStatus, gotBody := e.call(t, r.method, path, body)
			wantStatus, wantBody := e.call(t, r.method, "/namespaces/"+nsB+strings.Replace(r.path, "%s", uuid.NewString(), 1), body)
			if gotStatus != http.StatusNotFound || gotStatus != wantStatus || !bytes.Equal(gotBody, wantBody) {
				t.Errorf("%s %s: got %d %s, want %d %s", r.method, path, gotStatus, gotBody, wantStatus, wantBody)
			}
		}
	}
	edgeCases := []struct {
		method string
		body   map[string]string
	}{
		{http.MethodPost, map[string]string{"sourceId": vision, "targetId": goal, "type": "BREAKS_DOWN_TO"}},
		{http.MethodDelete, map[string]string{"sourceId": vision, "targetId": goal, "type": "BREAKS_DOWN_TO"}},
	}
	for _, c := range edgeCases {
		gotStatus, gotBody := e.call(t, c.method, "/namespaces/"+nsB+"/edges", c.body)
		random := map[string]string{"sourceId": uuid.NewString(), "targetId": uuid.NewString(), "type": "BREAKS_DOWN_TO"}
		wantStatus, wantBody := e.call(t, c.method, "/namespaces/"+nsB+"/edges", random)
		if gotStatus != http.StatusNotFound || gotStatus != wantStatus || !bytes.Equal(gotBody, wantBody) {
			t.Errorf("%s /edges in B with A's ids: got %d %s, want %d %s", c.method, gotStatus, gotBody, wantStatus, wantBody)
		}
	}

	// Lists only contain the namespace's own nodes.
	_, b := e.call(t, http.MethodGet, "/namespaces/"+nsB+"/nodes", nil)
	if got := nodeIDs(t, b); !sameIDs(got, []string{visionB, goalB}) || !strings.Contains(string(b), `"total":2`) {
		t.Errorf("list B = %s, want only %s and %s", b, visionB, goalB)
	}
	_, b = e.call(t, http.MethodGet, "/namespaces/"+nsB+"/nodes?type=vision", nil)
	if got := nodeIDs(t, b); !sameIDs(got, []string{visionB}) {
		t.Errorf("list B visions = %v, want [%s]", got, visionB)
	}

	// Namespace A is untouched by everything sent to B.
	status, b := e.call(t, http.MethodGet, "/namespaces/"+nsA+"/nodes/"+vision, nil)
	if status != http.StatusOK || !strings.Contains(string(b), `"A vision"`) {
		t.Errorf("A vision after B requests: %d %s", status, b)
	}
	_, b = e.call(t, http.MethodGet, "/namespaces/"+nsA+"/nodes/"+vision+"/children", nil)
	if got := nodeIDs(t, b); !sameIDs(got, []string{goal}) {
		t.Errorf("A vision children = %v, want [%s]", got, goal)
	}
}

// TestIntegration_CrossNamespaceEdgeRejected checks that an edge between
// nodes in different namespaces can't be created from either namespace.
func TestIntegration_CrossNamespaceEdgeRejected(t *testing.T) {
	e := newIntegrationEnv(t)
	nsA, nsB := newNamespace(), newNamespace()
	visionA := e.createNode(t, nsA, map[string]any{"type": "vision", "title": "A vision"})
	goalB := e.createNode(t, nsB, map[string]any{"type": "goal", "title": "B goal"})

	for _, ns := range []string{nsA, nsB} {
		if status, b := e.createEdge(t, ns, visionA, goalB, "BREAKS_DOWN_TO"); status != http.StatusNotFound {
			t.Errorf("cross-namespace edge via %s: %d %s, want 404", ns, status, b)
		}
	}

	ctx := context.Background()
	res, err := neo4j.ExecuteQuery(ctx, e.driver,
		"MATCH (a {id: $a})-[r]-(b {id: $b}) RETURN count(r) AS c",
		map[string]any{"a": visionA, "b": goalB}, neo4j.EagerResultTransformer)
	if err != nil {
		t.Fatalf("count edges: %v", err)
	}
	if c, _ := res.Records[0].Get("c"); c.(int64) != 0 {
		t.Errorf("found %d edges between namespaces, want 0", c)
	}
}

// TestIntegration_ChangefeedScoping checks that /changes and /subscribe only
// return changes made in their own namespace.
func TestIntegration_ChangefeedScoping(t *testing.T) {
	e := newIntegrationEnv(t)
	nsA, nsB := newNamespace(), newNamespace()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, e.ts.URL+"/namespaces/"+nsB+"/subscribe", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("subscribe status = %d", resp.StatusCode)
	}
	lines := make(chan string, 100)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()

	// A's change is published first, so if it leaked it would arrive first.
	nodeA := e.createNode(t, nsA, map[string]any{"type": "vision", "title": "A event"})
	nodeB := e.createNode(t, nsB, map[string]any{"type": "vision", "title": "B event"})

	timeout := time.After(5 * time.Second)
	var data []string
wait:
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				break wait
			}
			if strings.HasPrefix(l, "data: ") {
				data = append(data, l)
				if strings.Contains(l, nodeB) {
					break wait
				}
			}
		case <-timeout:
			t.Fatalf("no SSE event for B's node; got %v", data)
		}
	}
	for _, d := range data {
		if strings.Contains(d, nodeA) {
			t.Errorf("subscriber in B received A's change: %s", d)
		}
	}
	if len(data) != 1 {
		t.Errorf("subscriber in B got %d events, want 1: %v", len(data), data)
	}

	for ns, want := range map[string]string{nsA: nodeA, nsB: nodeB} {
		_, b := e.call(t, http.MethodGet, "/namespaces/"+ns+"/changes?type=vision", nil)
		var list ChangeList
		json.Unmarshal(b, &list)
		if len(list.Changes) != 1 || list.Changes[0].NodeId.String() != want {
			t.Errorf("changes in %s = %s, want one change for %s", ns, b, want)
		}
	}
}

// TestIntegration_AssignNamespace runs the one-off migration on nodes and
// changelog rows written without a namespace, as the pre-namespace code did.
// The migration moves every such node and row, so the test skips itself if
// the database already holds some.
func TestIntegration_AssignNamespace(t *testing.T) {
	e := newIntegrationEnv(t)
	ctx := context.Background()
	legacyNodes, err := db.CountNodesWithoutNamespace(ctx, e.driver)
	if err != nil {
		t.Fatalf("CountNodesWithoutNamespace: %v", err)
	}
	var legacyRows int64
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM changelog WHERE namespace IS NULL`).Scan(&legacyRows); err != nil {
		t.Fatalf("count changelog rows without namespace: %v", err)
	}
	if legacyNodes > 0 || legacyRows > 0 {
		t.Skipf("database has %d nodes and %d changelog rows without a namespace; the migration would move them, so run it against a database without any", legacyNodes, legacyRows)
	}
	vision, goal := uuid.NewString(), uuid.NewString()
	_, err = neo4j.ExecuteQuery(ctx, e.driver,
		`CREATE (v:Vision {id: $v, title: 'legacy vision'}), (g:Goal {id: $g, title: 'legacy goal'}),
		        (v)-[:BREAKS_DOWN_TO]->(g)`,
		map[string]any{"v": vision, "g": goal}, neo4j.EagerResultTransformer)
	if err != nil {
		t.Fatalf("create legacy nodes: %v", err)
	}
	_, err = e.pool.Exec(ctx,
		`INSERT INTO changelog (action, node_type, node_id, node) VALUES ('created', 'vision', $1, '{}')`, vision)
	if err != nil {
		t.Fatalf("insert legacy changelog row: %v", err)
	}

	count, err := db.CountNodesWithoutNamespace(ctx, e.driver)
	if err != nil || count != 2 {
		t.Fatalf("CountNodesWithoutNamespace = %d, %v; want 2", count, err)
	}
	probe := newNamespace()
	if status, b := e.call(t, http.MethodGet, "/namespaces/"+probe+"/nodes/"+vision, nil); status != http.StatusNotFound {
		t.Errorf("legacy node before migration: %d %s, want 404", status, b)
	}

	ns := newNamespace()
	nodes, err := db.AssignNamespace(ctx, e.driver, ns)
	if err != nil || nodes != 2 {
		t.Fatalf("AssignNamespace nodes = %d, %v; want 2", nodes, err)
	}
	entries, err := changelog.AssignNamespace(ctx, e.pool, ns)
	if err != nil || entries != 1 {
		t.Fatalf("AssignNamespace entries = %d, %v; want 1", entries, err)
	}

	_, b := e.call(t, http.MethodGet, "/namespaces/"+ns+"/nodes/"+goal+"/lineage", nil)
	if got := nodeIDs(t, b); !sameIDs(got, []string{vision, goal}) {
		t.Errorf("lineage after migration = %s, want %s and %s", b, vision, goal)
	}
	_, b = e.call(t, http.MethodGet, "/namespaces/"+ns+"/changes?type=vision", nil)
	if !strings.Contains(string(b), vision) {
		t.Errorf("changes after migration = %s, want an entry for %s", b, vision)
	}
	if status, _ := e.call(t, http.MethodGet, "/namespaces/"+probe+"/nodes/"+vision, nil); status != http.StatusNotFound {
		t.Errorf("migrated node visible in another namespace: %d", status)
	}

	// Running it again changes nothing, whatever namespace it's given.
	if n, err := db.AssignNamespace(ctx, e.driver, newNamespace()); err != nil || n != 0 {
		t.Errorf("second AssignNamespace nodes = %d, %v; want 0", n, err)
	}
	if n, err := changelog.AssignNamespace(ctx, e.pool, newNamespace()); err != nil || n != 0 {
		t.Errorf("second AssignNamespace entries = %d, %v; want 0", n, err)
	}
	if count, err := db.CountNodesWithoutNamespace(ctx, e.driver); err != nil || count != 0 {
		t.Errorf("CountNodesWithoutNamespace after migration = %d, %v; want 0", count, err)
	}
}

func (e *integrationEnv) countDependsOn(t *testing.T, source, target string) int64 {
	t.Helper()
	res, err := neo4j.ExecuteQuery(context.Background(), e.driver,
		"MATCH ({id: $s})-[r:DEPENDS_ON]->({id: $t}) RETURN count(r) AS c",
		map[string]any{"s": source, "t": target}, neo4j.EagerResultTransformer)
	if err != nil {
		t.Fatalf("count DEPENDS_ON: %v", err)
	}
	c, _ := res.Records[0].Get("c")
	return c.(int64)
}

func (e *integrationEnv) ids(t *testing.T, ns, path string) []string {
	t.Helper()
	status, b := e.call(t, http.MethodGet, "/namespaces/"+ns+path, nil)
	if status != http.StatusOK {
		t.Fatalf("GET %s in %s: %d %s", path, ns, status, b)
	}
	return nodeIDs(t, b)
}

// TestIntegration_Dependencies covers creating, validating, reading and
// deleting DEPENDS_ON edges over HTTP.
func TestIntegration_Dependencies(t *testing.T) {
	e := newIntegrationEnv(t)
	ns := newNamespace()
	feature := e.createNode(t, ns, map[string]any{"type": "feature", "title": "F"})
	r1 := e.createNode(t, ns, map[string]any{"type": "requirement", "title": "R1"})
	r2 := e.createNode(t, ns, map[string]any{"type": "requirement", "title": "R2"})
	r3 := e.createNode(t, ns, map[string]any{"type": "requirement", "title": "R3"})
	task := e.createNode(t, ns, map[string]any{"type": "task", "title": "T", "status": "backlog"})
	e.mustCreateEdge(t, ns, feature, r1, "BREAKS_DOWN_TO")
	e.mustCreateEdge(t, ns, r1, task, "BREAKS_DOWN_TO")

	// R2 depends on R1; creating it again is a no-op.
	e.mustCreateEdge(t, ns, r2, r1, "DEPENDS_ON")
	e.mustCreateEdge(t, ns, r2, r1, "DEPENDS_ON")
	if c := e.countDependsOn(t, r2, r1); c != 1 {
		t.Errorf("R2->R1 DEPENDS_ON edges = %d, want 1", c)
	}

	rejected := []struct {
		name           string
		source, target string
		want           int
	}{
		{"self", r1, r1, http.StatusBadRequest},
		{"different types", r1, task, http.StatusBadRequest},
		{"different types reversed", task, r1, http.StatusBadRequest},
		{"direct cycle", r1, r2, http.StatusConflict},
		{"missing target", r1, uuid.NewString(), http.StatusNotFound},
	}
	for _, c := range rejected {
		if status, b := e.createEdge(t, ns, c.source, c.target, "DEPENDS_ON"); status != c.want {
			t.Errorf("%s: %d %s, want %d", c.name, status, b, c.want)
		}
	}
	// R3 depends on R2, so R1 depending on R3 would close R1->R3->R2->R1.
	e.mustCreateEdge(t, ns, r3, r2, "DEPENDS_ON")
	if status, b := e.createEdge(t, ns, r1, r3, "DEPENDS_ON"); status != http.StatusConflict {
		t.Errorf("transitive cycle: %d %s, want 409", status, b)
	}

	reads := []struct {
		path string
		want []string
	}{
		{"/nodes/" + r2 + "/dependencies", []string{r1}},
		{"/nodes/" + r1 + "/dependents", []string{r2}},
		{"/nodes/" + r2 + "/dependents", []string{r3}},
		{"/nodes/" + r1 + "/dependencies", []string{}},
		// Traversals follow the breakdown tree only.
		{"/nodes/" + r1 + "/children", []string{task}},
		{"/nodes/" + r1 + "/parents", []string{feature}},
		{"/nodes/" + r2 + "/children", []string{}},
		{"/nodes/" + r2 + "/parents", []string{}},
		{"/nodes/" + r1 + "/lineage", []string{feature, r1}},
		{"/nodes/" + r2 + "/lineage", []string{}},
	}
	for _, rd := range reads {
		if got := e.ids(t, ns, rd.path); !sameIDs(got, rd.want) {
			t.Errorf("GET %s = %v, want %v", rd.path, got, rd.want)
		}
	}
	// R2 depends on R1 and R3 depends on R2, so a subtree that followed
	// DEPENDS_ON would contain R1 under R2 and R2 under R3.
	for _, c := range []struct{ root, hidden string }{{r2, r1}, {r3, r2}, {feature, r2}} {
		_, b := e.call(t, http.MethodGet, "/namespaces/"+ns+"/nodes/"+c.root+"/subtree", nil)
		if strings.Contains(string(b), c.hidden) {
			t.Errorf("subtree of %s contains %s, which is only reachable through DEPENDS_ON: %s", c.root, c.hidden, b)
		}
	}
	_, b := e.call(t, http.MethodGet, "/namespaces/"+ns+"/nodes/"+r1, nil)
	var rel struct{ Children, Parents []struct{ Id string } }
	json.Unmarshal(b, &rel)
	if len(rel.Children) != 1 || rel.Children[0].Id != task || len(rel.Parents) != 1 || rel.Parents[0].Id != feature {
		t.Errorf("GET R1 relationships = %s, want only T as child and F as parent", b)
	}

	// Deleting R3->R2 removes the path, so R1 may now depend on R3.
	status, b := e.call(t, http.MethodDelete, "/namespaces/"+ns+"/edges",
		map[string]string{"sourceId": r3, "targetId": r2, "type": "DEPENDS_ON"})
	if status != http.StatusNoContent {
		t.Fatalf("delete R3->R2: %d %s", status, b)
	}
	e.mustCreateEdge(t, ns, r1, r3, "DEPENDS_ON")

	// Deleting R1 removes its DEPENDS_ON edges in both directions.
	if status, b := e.call(t, http.MethodDelete, "/namespaces/"+ns+"/nodes/"+r1, nil); status != http.StatusNoContent {
		t.Fatalf("delete R1: %d %s", status, b)
	}
	if got := e.ids(t, ns, "/nodes/"+r2+"/dependencies"); len(got) != 0 {
		t.Errorf("R2 dependencies after deleting R1 = %v, want none", got)
	}
	if got := e.ids(t, ns, "/nodes/"+r3+"/dependents"); len(got) != 0 {
		t.Errorf("R3 dependents after deleting R1 = %v, want none", got)
	}
	if c := e.countDependsOn(t, r2, r1) + e.countDependsOn(t, r1, r3); c != 0 {
		t.Errorf("%d DEPENDS_ON edges still touch R1", c)
	}
}

// TestIntegration_DependenciesNamespaceIsolation checks the dependency
// endpoints and DEPENDS_ON edges against nodes in another namespace.
func TestIntegration_DependenciesNamespaceIsolation(t *testing.T) {
	e := newIntegrationEnv(t)
	nsA, nsB := newNamespace(), newNamespace()
	a1 := e.createNode(t, nsA, map[string]any{"type": "task", "title": "A1", "status": "backlog"})
	a2 := e.createNode(t, nsA, map[string]any{"type": "task", "title": "A2", "status": "backlog"})
	e.mustCreateEdge(t, nsA, a1, a2, "DEPENDS_ON")
	b1 := e.createNode(t, nsB, map[string]any{"type": "task", "title": "B1", "status": "backlog"})

	for _, path := range []string{"/dependencies", "/dependents"} {
		gotStatus, gotBody := e.call(t, http.MethodGet, "/namespaces/"+nsB+"/nodes/"+a1+path, nil)
		wantStatus, wantBody := e.call(t, http.MethodGet, "/namespaces/"+nsB+"/nodes/"+uuid.NewString()+path, nil)
		if gotStatus != http.StatusNotFound || gotStatus != wantStatus || !bytes.Equal(gotBody, wantBody) {
			t.Errorf("GET %s of A's node in B: %d %s, want %d %s", path, gotStatus, gotBody, wantStatus, wantBody)
		}
	}

	// A's nodes can't be linked from B, and B's node can't be linked to A's.
	for _, c := range []struct{ ns, source, target string }{
		{nsB, a2, a1}, {nsA, a1, b1}, {nsA, b1, a1}, {nsB, b1, a1}, {nsB, a1, b1},
	} {
		if status, b := e.createEdge(t, c.ns, c.source, c.target, "DEPENDS_ON"); status != http.StatusNotFound {
			t.Errorf("DEPENDS_ON %s->%s via %s: %d %s, want 404", c.source, c.target, c.ns, status, b)
		}
	}
	status, b := e.call(t, http.MethodDelete, "/namespaces/"+nsB+"/edges",
		map[string]string{"sourceId": a1, "targetId": a2, "type": "DEPENDS_ON"})
	if status != http.StatusNotFound {
		t.Errorf("delete A's DEPENDS_ON from B: %d %s, want 404", status, b)
	}
	if c := e.countDependsOn(t, a1, a2); c != 1 {
		t.Errorf("A1->A2 DEPENDS_ON edges = %d, want 1", c)
	}
	if c := e.countDependsOn(t, a1, b1) + e.countDependsOn(t, b1, a1); c != 0 {
		t.Errorf("found %d DEPENDS_ON edges between namespaces, want 0", c)
	}
}

// TestIntegration_Outputs creates an Output under a task and checks that its
// payload comes back unchanged from every read, that only a task can be its
// parent, and that its changes reach /changes and /subscribe.
func TestIntegration_Outputs(t *testing.T) {
	e := newIntegrationEnv(t)
	ns := newNamespace()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, e.ts.URL+"/namespaces/"+ns+"/subscribe?type=output", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer resp.Body.Close()
	events := make(chan string, 100)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if l := sc.Text(); strings.HasPrefix(l, "data: ") {
				events <- strings.TrimPrefix(l, "data: ")
			}
		}
	}()

	task := e.createNode(t, ns, map[string]any{"type": "task", "title": "T", "status": "backlog"})
	vision := e.createNode(t, ns, map[string]any{"type": "vision", "title": "V"})
	payload := map[string]any{
		"repository": "https://github.com/d4nyll/nodoplex.git",
		"baseRef":    "main",
		"currentRef": "feat/oidc",
		"extra":      map[string]any{"n": 1.5, "list": []any{"a", true, nil}},
	}
	output := e.createNode(t, ns, map[string]any{
		"type": "output", "outputType": "git", "description": "OIDC branch", "payload": payload,
	})
	e.mustCreateEdge(t, ns, task, output, "IMPLEMENTED_BY")

	want := map[string]any{
		"id": output, "type": "output", "outputType": "git", "description": "OIDC branch", "payload": payload,
	}
	sameJSON := func(t *testing.T, what string, got any) {
		t.Helper()
		g, _ := json.Marshal(got)
		w, _ := json.Marshal(want)
		if !bytes.Equal(g, w) {
			t.Errorf("%s = %s, want %s", what, g, w)
		}
	}

	_, b := e.call(t, http.MethodGet, "/namespaces/"+ns+"/nodes/"+output, nil)
	var withRels struct {
		Node    map[string]any
		Parents []map[string]any
	}
	json.Unmarshal(b, &withRels)
	sameJSON(t, "GET output", withRels.Node)
	if len(withRels.Parents) != 1 || withRels.Parents[0]["id"] != task {
		t.Errorf("parents of output = %v, want the task", withRels.Parents)
	}
	var children struct{ Nodes []map[string]any }
	_, b = e.call(t, http.MethodGet, "/namespaces/"+ns+"/nodes/"+task+"/children", nil)
	json.Unmarshal(b, &children)
	if len(children.Nodes) != 1 {
		t.Fatalf("children of task = %s, want the output", b)
	}
	sameJSON(t, "child of task", children.Nodes[0])
	if got := e.ids(t, ns, "/nodes?type=output"); !sameIDs(got, []string{output}) {
		t.Errorf("list outputs = %v, want %s", got, output)
	}

	for _, c := range []struct{ name, source, target, edgeType string }{
		{"output to task", output, task, "IMPLEMENTED_BY"},
		{"vision to output", vision, output, "IMPLEMENTED_BY"},
		{"task to output via BREAKS_DOWN_TO", task, output, "BREAKS_DOWN_TO"},
	} {
		if status, b := e.createEdge(t, ns, c.source, c.target, c.edgeType); status != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", c.name, status, b)
		}
	}

	// PATCH replaces the whole payload.
	status, b := e.call(t, http.MethodPatch, "/namespaces/"+ns+"/nodes/"+output,
		map[string]any{"payload": map[string]any{"currentRef": "abc123"}})
	if status != http.StatusOK {
		t.Fatalf("patch payload: %d %s", status, b)
	}
	want["payload"] = map[string]any{"currentRef": "abc123"}
	var patched map[string]any
	json.Unmarshal(b, &patched)
	sameJSON(t, "PATCH response", patched)

	_, b = e.call(t, http.MethodGet, "/namespaces/"+ns+"/changes?type=output", nil)
	var list struct {
		Changes []struct {
			Action   string
			NodeType string
			Node     map[string]any
			Previous map[string]any
		}
	}
	json.Unmarshal(b, &list)
	if len(list.Changes) != 2 || list.Changes[0].Action != "created" || list.Changes[1].Action != "updated" {
		t.Fatalf("changes = %s, want created and updated", b)
	}
	sameJSON(t, "changelog node after update", list.Changes[1].Node)
	if list.Changes[1].Previous["payload"].(map[string]any)["baseRef"] != "main" {
		t.Errorf("changelog previous = %v, want the original payload", list.Changes[1].Previous)
	}

	for _, action := range []string{"created", "updated"} {
		select {
		case ev := <-events:
			if !strings.Contains(ev, `"action":"`+action+`"`) || !strings.Contains(ev, `"outputType":"git"`) {
				t.Errorf("SSE event = %s, want %s output", ev, action)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no SSE event for the %s output", action)
		}
	}
}
