package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/d4nyll/nodoplex/internal/db"
	"github.com/d4nyll/nodoplex/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
)

// The integration tests run the HTTP API and the store against the Neo4j and
// PostgreSQL servers of NEO4J_URI and POSTGRES_URL (by default, those of
// docker-compose.yaml). They run only if NODOPLEX_INTEGRATION=1. Each test
// uses its own PostgreSQL schema and its own slugs. No other Nodoplex server
// may use the same Neo4j server at the same time, because its outbox copier
// would copy the Change Entries of the tests.

type itEnv struct {
	t     *testing.T
	url   string
	store *store.Store
	neo   neo4j.Driver
	pg    *pgxpool.Pool
}

func getenv(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func newEnv(t *testing.T, settings store.Settings) *itEnv { return startEnv(t, settings, true) }

// startEnv starts a server. If run is false, the server runs no background
// loop, so only the writes and the reads copy the outbox.
func startEnv(t *testing.T, settings store.Settings, run bool) *itEnv {
	t.Helper()
	if os.Getenv("NODOPLEX_INTEGRATION") != "1" {
		t.Skip("set NODOPLEX_INTEGRATION=1 to run against Neo4j and PostgreSQL")
	}
	ctx := context.Background()
	neo, err := db.NewNeo4jDriver(getenv("NEO4J_URI", "bolt://localhost:7687"), getenv("NEO4J_USER", "neo4j"), getenv("NEO4J_PASSWORD", "changeme"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { neo.Close(ctx) })
	pgURL := getenv("POSTGRES_URL", "postgres://nodoplex:changeme@localhost:5432/nodoplex?sslmode=disable")
	admin, err := db.NewPostgresPool(ctx, pgURL)
	if err != nil {
		t.Fatal(err)
	}
	schema := "it_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(pgURL)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pg, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pg.Close()
		admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})
	st := store.New(neo, pg, settings)
	if err := st.Start(ctx); err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	stopped := make(chan struct{})
	if run {
		go func() { st.Run(runCtx); close(stopped) }()
	} else {
		close(stopped)
	}
	ts := httptest.NewServer(NewHandler(runCtx, st))
	t.Cleanup(func() { cancel(); ts.Close(); <-stopped })
	return &itEnv{t: t, url: ts.URL, store: st, neo: neo, pg: pg}
}

var defaultSettings = store.Settings{ExposeChangefeed: true, DeleteOldChangeEntries: true,
	ChangeEntryCountTarget: 1000000, ChangeEntrySizeTarget: 1 << 30}

// newProduct creates a Product with a new slug, and deletes its data from
// Neo4j when the test ends.
func (e *itEnv) newProduct() (slug, id string) {
	slug = "it." + strings.ReplaceAll(uuid.NewString(), "-", "") + ".app"
	e.t.Cleanup(func() {
		ctx := context.Background()
		s := e.neo.NewSession(ctx, neo4j.SessionConfig{})
		defer s.Close(ctx)
		for _, q := range []string{
			"MATCH (n:Node {product: $slug}) DETACH DELETE n",
			"MATCH (n:DeletedNode {product: $slug}) DELETE n",
			"MATCH (n:OutboxEntry {slug: $slug}) DELETE n",
			"MATCH (n:Changefeed {slug: $slug}) DELETE n",
		} {
			s.Run(ctx, q, map[string]any{"slug": slug})
		}
	})
	var node map[string]any
	e.must(201, "PUT", "/products/"+slug, `{"fields":{"title":"IT"}}`, &node)
	return slug, node["id"].(string)
}

// call sends a request as alice and decodes the body into out.
func (e *itEnv) call(method, path, body string, out any) (int, string) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.url+path, strings.NewReader(body))
	req.Header.Set("Nodoplex-User-Id", "alice")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if out != nil && len(data) > 0 {
		dec := json.NewDecoder(strings.NewReader(string(data)))
		dec.UseNumber()
		dec.Decode(out)
	}
	return res.StatusCode, res.Header.Get("Nodoplex-Error-Code")
}

func (e *itEnv) must(status int, method, path, body string, out any) {
	e.t.Helper()
	if got, code := e.call(method, path, body, out); got != status {
		e.t.Fatalf("%s %s: got %d %s, want %d", method, path, got, code, status)
	}
}

func (e *itEnv) refused(code, method, path, body string) {
	e.t.Helper()
	if _, got := e.call(method, path, body, nil); got != code {
		e.t.Fatalf("%s %s: got code %q, want %q", method, path, got, code)
	}
}

func (e *itEnv) createNode(slug, nodeType, parent, title string) string {
	e.t.Helper()
	var node map[string]any
	e.must(201, "POST", "/products/"+slug+"/nodes",
		fmt.Sprintf(`{"type":%q,"parents":[{"id":%q}],"fields":{"title":%q}}`, nodeType, parent, title), &node)
	return node["id"].(string)
}

func (e *itEnv) version(slug, id string) string {
	e.t.Helper()
	var node map[string]any
	e.must(200, "GET", "/products/"+slug+"/nodes/"+id+"?includeArchived=true", "", &node)
	return string(node["version"].(json.Number))
}

type changePage struct {
	ChangeEntries  []json.RawMessage `json:"changeEntries"`
	LatestChangeID int64             `json:"latestChangeId"`
}

func (e *itEnv) poll(slug string, start int64, filterGroups string) changePage {
	e.t.Helper()
	var page changePage
	path := fmt.Sprintf("/products/%s/poll?startChangeId=%d&limit=1000", slug, start)
	if filterGroups != "" {
		path += "&filterGroups=" + url.QueryEscape(filterGroups)
	}
	e.must(200, "GET", path, "", &page)
	return page
}

func changeIDs(page changePage) []int64 {
	var ids []int64
	for _, raw := range page.ChangeEntries {
		var e struct{ ChangeID int64 }
		json.Unmarshal(raw, &e)
		ids = append(ids, e.ChangeID)
	}
	return ids
}

// outboxSize counts the Change Entries of the slug that Neo4j still holds.
func (e *itEnv) outboxSize(slug string) int64 {
	ctx := context.Background()
	s := e.neo.NewSession(ctx, neo4j.SessionConfig{})
	defer s.Close(ctx)
	res, err := s.Run(ctx, "MATCH (o:OutboxEntry {slug: $slug}) RETURN count(o)", map[string]any{"slug": slug})
	if err != nil {
		e.t.Fatal(err)
	}
	rec, err := res.Single(ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	return rec.Values[0].(int64)
}

// retained returns the number and the total size of the Change Entries of
// the slug, in PostgreSQL and in the Neo4j outbox (B13).
func (e *itEnv) retained(slug string) (count, size int64) {
	e.t.Helper()
	ctx := context.Background()
	if err := e.pg.QueryRow(ctx, "SELECT count(*), coalesce(sum(size), 0) FROM change_entry WHERE slug = $1", slug).Scan(&count, &size); err != nil {
		e.t.Fatal(err)
	}
	s := e.neo.NewSession(ctx, neo4j.SessionConfig{})
	defer s.Close(ctx)
	res, err := s.Run(ctx, "MATCH (o:OutboxEntry {slug: $slug}) RETURN o.json", map[string]any{"slug": slug})
	if err != nil {
		e.t.Fatal(err)
	}
	recs, err := res.Collect(ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, r := range recs {
		count, size = count+1, size+int64(len(r.Values[0].(string)))
	}
	return count, size
}

// sse is an open subscription over HTTP.
type sse struct {
	r      *bufio.Reader
	cancel func()
}

func (e *itEnv) subscribe(slug, query string, header map[string]string) *sse {
	e.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", e.url+"/products/"+slug+"/subscribe?"+query, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 200 || res.Header.Get("Content-Type") != "text/event-stream" {
		e.t.Fatalf("subscribe: %v %v", err, res)
	}
	e.t.Cleanup(cancel)
	return &sse{r: bufio.NewReader(res.Body), cancel: cancel}
}

// next reads one message: its id, its event type, and its data.
func (s *sse) next(t *testing.T) (id, event, data string) {
	t.Helper()
	done := make(chan struct{})
	timer := time.AfterFunc(10*time.Second, func() { s.cancel() })
	defer func() { timer.Stop(); close(done) }()
	for {
		line, err := s.r.ReadString('\n')
		if err != nil {
			t.Fatalf("read event stream: %v (id %q event %q data %q)", err, id, event, data)
		}
		line = strings.TrimSuffix(line, "\n")
		switch {
		case line == "" && (id != "" || data != ""):
			return id, event, data
		case strings.HasPrefix(line, "id: "):
			id = line[4:]
		case strings.HasPrefix(line, "event: "):
			event = line[7:]
		case strings.HasPrefix(line, "data: "):
			data = line[6:]
		}
	}
}

// waitFor polls cond until it is true, or fails the test after 10 s.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// ── Tests ───────────────────────────────────────────────────────────────────

func TestIntegrationWorkflow(t *testing.T) {
	e := newEnv(t, defaultSettings)
	slug, product := e.newProduct()
	v := e.createNode(slug, "vision", product, "V")
	g := e.createNode(slug, "goal", v, "G")
	f := e.createNode(slug, "feature", g, "F")
	r := e.createNode(slug, "requirement", f, "R")
	a := e.createNode(slug, "task", r, "A")
	b := e.createNode(slug, "task", r, "B")
	z := e.createNode(slug, "subtask", a, "Z")

	e.must(201, "POST", "/products/"+slug+"/edges",
		fmt.Sprintf(`{"kind":"dependency","tailId":%q,"headId":%q,"version":%s}`, b, a, e.version(slug, b)), nil)
	e.refused("done_leaf_blocked", "PATCH", "/products/"+slug+"/nodes/"+b, `{"version":`+e.version(slug, b)+`,"fields":{"status":"done"}}`)
	e.must(200, "PATCH", "/products/"+slug+"/nodes/"+z, `{"version":`+e.version(slug, z)+`,"fields":{"status":"done"}}`, nil)
	var node map[string]any
	e.must(200, "GET", "/products/"+slug+"/nodes/"+r, "", &node)
	if node["fields"].(map[string]any)["status"] != "in_progress" {
		t.Errorf("R has a done Task and a not_started Task, so it is in_progress: %v", node["fields"])
	}
	e.refused("version_conflict", "PATCH", "/products/"+slug+"/nodes/"+z, `{"version":1,"fields":{"title":"Z2"}}`)

	// The payload keeps each number exactly.
	e.must(200, "PATCH", "/products/"+slug+"/nodes/"+a,
		`{"version":`+e.version(slug, a)+`,"fields":{"outputs":[{"outputType":"git","payload":{"n":1.10,"big":123456789012345678901234567890}}]}}`, nil)
	req, _ := http.NewRequest("GET", e.url+"/products/"+slug+"/nodes/"+a, nil)
	req.Header.Set("Nodoplex-User-Id", "alice")
	res, _ := http.DefaultClient.Do(req)
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(raw), `"payload":{"big":123456789012345678901234567890,"n":1.10}`) {
		t.Errorf("the payload must keep its numbers: %s", raw)
	}

	// B8: a poll right after the writes holds each Change Entry, and the
	// server has copied them all out of Neo4j.
	page := e.poll(slug, 0, "")
	ids := changeIDs(page)
	if int64(len(ids)) != page.LatestChangeID || ids[0] != 1 {
		t.Fatalf("Change IDs %v, latest %d", ids, page.LatestChangeID)
	}
	for i := range ids {
		if ids[i] != int64(i+1) {
			t.Fatalf("Change IDs must increase by 1: %v", ids)
		}
	}
	if n := e.outboxSize(slug); n != 0 {
		t.Errorf("the outbox still holds %d Change Entries", n)
	}
	var prev string
	for _, raw := range page.ChangeEntries {
		var entry struct{ Time string }
		json.Unmarshal(raw, &entry)
		if entry.Time < prev {
			t.Errorf("time decreased: %s after %s", entry.Time, prev)
		}
		prev = entry.Time
	}

	// A filter group: Changes after which a Task is done.
	done := e.poll(slug, 0, `[{"type":["task"],"status":{"check":"after","values":["done"]}}]`)
	if got := changeIDs(done); len(got) != 2 {
		t.Errorf("Z done makes A done, and the payload update keeps A done: got %v", got)
	}

	// Watchers of a deleted Node: the watcher filter checks the watchers that
	// the Node had when it was deleted (Q4).
	e.must(200, "POST", "/products/"+slug+"/nodes/"+z+"/watch", "", nil)
	e.must(204, "DELETE", "/products/"+slug+"/nodes/"+z+"?version="+e.version(slug, z), "", nil)
	watched := changeIDs(e.poll(slug, 0, `[{"watcher":["alice"]}]`))
	if len(watched) == 0 || watched[len(watched)-1] != page.LatestChangeID+1 {
		t.Errorf("the deletion of a watched Node matches the watcher filter: %v", watched)
	}
}

func TestIntegrationSubscribe(t *testing.T) {
	e := newEnv(t, defaultSettings)
	slug, product := e.newProduct()
	live := e.subscribe(slug, "userId=bob&filterGroups="+url.QueryEscape(`[{"type":["goal"]}]`), nil)
	replay := e.subscribe(slug, "", map[string]string{"Nodoplex-User-Id": "bob", "Last-Event-ID": "0"})

	// The Change Entry of the Vision matches no filter group, so the first
	// message holds the Goal.
	v := e.createNode(slug, "vision", product, "V")
	g := e.createNode(slug, "goal", v, "G")
	id, event, data := live.next(t)
	if id != "3" || event != "" || !strings.Contains(data, g) {
		t.Fatalf("the subscription pushes the Change Entry of the Goal: %q %q %q", id, event, data)
	}
	// The pushed text is the JSON text that the poll returns (B10).
	page := e.poll(slug, 2, "")
	if string(page.ChangeEntries[0]) != data {
		t.Errorf("poll and subscription differ:\n%s\n%s", page.ChangeEntries[0], data)
	}
	for _, want := range []string{"1", "2", "3"} {
		if id, _, _ := replay.next(t); id != want {
			t.Errorf("Last-Event-ID 0 replays each Change Entry: got %s, want %s", id, want)
		}
	}
}

// TestIntegrationRetention checks the deletions for the Change Entry Count
// Target, which the background loop runs (R2).
func TestIntegrationRetention(t *testing.T) {
	settings := defaultSettings
	settings.ChangeEntryCountTarget = 3
	e := newEnv(t, settings)
	slug, product := e.newProduct()
	sub := e.store.Subscribe(slug, 1, nil) // a subscription at position 1 that reads nothing yet
	defer sub.Close()
	for i := 0; i < 5; i++ {
		e.createNode(slug, "vision", product, fmt.Sprintf("V%d", i))
	}
	waitFor(t, "R2 to keep 3 Change Entries", func() bool { return len(e.poll(slug, 0, "").ChangeEntries) == 3 })
	if ids := changeIDs(e.poll(slug, 0, "")); ids[0] != 4 || ids[2] != 6 {
		t.Fatalf("R2 deletes the oldest Change Entries: %v", ids)
	}
	e.refused("change_entries_deleted", "GET", "/products/"+slug+"/poll?startChangeId=1&limit=10", "")
	e.refused("change_entries_deleted", "GET", "/products/"+slug+"/subscribe?startChangeId=2", "")
	if ids := changeIDs(e.poll(slug, 3, "")); len(ids) != 3 {
		t.Errorf("a poll after the deleted Change Entries succeeds: %v", ids)
	}
	if _, _, err := sub.Next(context.Background()); err == nil || !strings.Contains(err.Error(), "change_entries_deleted") {
		t.Errorf("an open subscription before the deleted Change Entries ends with change_entries_deleted: %v", err)
	}
	// The server never gives a Change ID again (C2).
	var node map[string]any
	e.must(201, "POST", "/products/"+slug+"/nodes", fmt.Sprintf(`{"type":"vision","parents":[{"id":%q}],"fields":{"title":"V5"}}`, product), &node)
	if node["version"].(json.Number) != "7" {
		t.Errorf("the next Change ID is 7, got %v", node["version"])
	}
}

// TestIntegrationRetentionSize checks the deletions for the Change Entry
// Size Target.
func TestIntegrationRetentionSize(t *testing.T) {
	settings := defaultSettings
	settings.ChangeEntrySizeTarget = 2000
	e := newEnv(t, settings)
	slug, product := e.newProduct()
	for i := 0; i < 6; i++ {
		e.createNode(slug, "vision", product, fmt.Sprintf("V%d", i))
	}
	waitFor(t, "R2 to keep at most 2000 bytes", func() bool {
		e.poll(slug, 0, "") // copies the outbox
		_, size := e.retained(slug)
		return size <= 2000
	})
	ids := changeIDs(e.poll(slug, 0, ""))
	if len(ids) == 0 || len(ids) == 7 || ids[len(ids)-1] != 7 || ids[len(ids)-1]-ids[0] != int64(len(ids)-1) {
		t.Errorf("R2 deletes the oldest Change Entries, and keeps the rest: %v", ids)
	}
}

func TestIntegrationHealth(t *testing.T) {
	e := newEnv(t, defaultSettings)
	get := func(path string) (int, string, string) {
		res, err := http.Get(e.url + path) // no header Nodoplex-User-Id
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, res.Header.Get("Nodoplex-Error-Code"), string(body)
	}
	for _, path := range []string{"/livez", "/startupz", "/readyz"} {
		if status, _, body := get(path); status != 200 || body != "" {
			t.Errorf("%s after the server has started: %d %q", path, status, body)
		}
	}
	// L3: a query to PostgreSQL fails.
	e.pg.Close()
	if status, code, _ := get("/readyz"); status != 503 || code != "unavailable" {
		t.Errorf("/readyz with no PostgreSQL: %d %q", status, code)
	}
	for _, path := range []string{"/livez", "/startupz"} {
		if status, _, _ := get(path); status != 200 {
			t.Errorf("%s with no PostgreSQL: %d", path, status)
		}
	}
}

func TestIntegrationDeletedProduct(t *testing.T) {
	e := newEnv(t, defaultSettings)
	slug, product := e.newProduct()
	v := e.createNode(slug, "vision", product, "V")
	e.must(204, "DELETE", "/products/"+slug+"/nodes/"+v+"?version="+e.version(slug, v), "", nil)
	sub := e.subscribe(slug, "", map[string]string{"Nodoplex-User-Id": "bob"})
	e.must(204, "DELETE", "/products/"+slug+"/nodes/"+product+"?version="+e.version(slug, product), "", nil)
	if id, _, _ := sub.next(t); id != "4" {
		t.Fatalf("the subscription pushes the deletion of the Product: %s", id)
	}

	// P7: for 30 days, Poll, Subscribe, and GetLatestChangeId run.
	if ids := changeIDs(e.poll(slug, 0, "")); len(ids) != 4 {
		t.Errorf("the Changefeed of a deleted Product is kept: %v", ids)
	}
	e.must(200, "GET", "/products/"+slug+"/latest-change-id", "", nil)
	e.refused("not_found", "GET", "/products/"+slug+"/nodes", "")
	e.refused("not_found", "GET", "/products/"+slug+"/next-work", "")
	e.refused("slug_taken", "PUT", "/products/"+slug, `{"fields":{"title":"again"}}`)
	var list struct {
		Products []struct{ Fields struct{ Slug string } }
	}
	e.must(200, "GET", "/products?archived=true&archived=false", "", &list)
	for _, p := range list.Products {
		if p.Fields.Slug == slug {
			t.Error("ListProducts leaves out a deleted Product")
		}
	}

	if err := e.store.PurgeExpired(context.Background(), time.Now().Add(29*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	e.must(200, "GET", "/products/"+slug+"/latest-change-id", "", nil)

	// P8: after 30 days, the server deletes the Changefeed and ends each
	// subscription with not_found.
	if err := e.store.PurgeExpired(context.Background(), time.Now().Add(31*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, event, data := sub.next(t); event != "end" || !strings.Contains(data, `"code":"not_found"`) {
		t.Errorf("the subscription ends with not_found: %q %q", event, data)
	}
	e.refused("not_found", "GET", "/products/"+slug+"/poll?startChangeId=0&limit=10", "")
	e.refused("not_found", "GET", "/products/"+slug+"/latest-change-id", "")
	e.refused("slug_taken", "PUT", "/products/"+slug, `{"fields":{"title":"again"}}`)
	var count int64
	e.pg.QueryRow(context.Background(), "SELECT count(*) FROM change_entry WHERE slug = $1", slug).Scan(&count)
	if count != 0 {
		t.Errorf("PostgreSQL still holds %d Change Entries of the deleted Product", count)
	}
}

func TestIntegrationExposeChangefeedOff(t *testing.T) {
	settings := defaultSettings
	settings.ExposeChangefeed = false
	e := newEnv(t, settings)
	slug, product := e.newProduct()
	v := e.createNode(slug, "vision", product, "V")
	e.refused("changefeed_disabled", "GET", "/products/"+slug+"/poll?startChangeId=0&limit=10", "")
	e.refused("changefeed_disabled", "GET", "/products/"+slug+"/subscribe", "")
	var latest map[string]any
	e.must(200, "GET", "/products/"+slug+"/latest-change-id", "", &latest)
	if latest["latestChangeId"].(json.Number) != "2" || e.version(slug, v) != "2" {
		t.Errorf("the server records Change Entries and gives versions: %v", latest)
	}
}
