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
