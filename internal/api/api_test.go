package api

import (
	"context"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// TestRoutesMatchOpenAPI checks that the server serves each operation of
// api/openapi.yaml at its method and its path, with the same query
// parameters, and no other operation.
func TestRoutesMatchOpenAPI(t *testing.T) {
	spec, err := GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{}
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			var query []string
			for _, p := range append(item.Parameters, op.Parameters...) {
				if p.Value.In == "query" {
					query = append(query, p.Value.Name)
				}
			}
			slices.Sort(query)
			want[method+" "+path] = query
		}
	}
	got := map[string][]string{}
	for _, op := range operations {
		query := slices.Clone(op.query)
		slices.Sort(query)
		got[op.method+" "+op.path] = query
	}
	for key, q := range want {
		if g, ok := got[key]; !ok || !slices.Equal(g, q) {
			t.Errorf("%s: the spec has the query parameters %v, the server %v (served %v)", key, q, g, ok)
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			t.Errorf("the server serves %s, which the spec doesn't have", key)
		}
	}
}

// serve sends a request to a Handler with no store. Each request in these
// tests fails a check before the operation reads the store.
func serve(method, target, body string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	NewHandler(context.Background(), nil).ServeHTTP(rec, req)
	return rec
}

var alice = map[string]string{"Nodoplex-User-Id": "alice"}

func TestRequestChecks(t *testing.T) {
	cases := []struct {
		name, method, target, body string
		header                     map[string]string
		status                     int
		code                       string
	}{
		{"no operation has the path", "GET", "/nope", "", alice, 404, "not_found"},
		{"no operation has the method", "POST", "/products", "", alice, 405, "method_not_allowed"},
		{"a path with an empty segment matches", "GET", "/products//nodes", "", alice, 400, "invalid_request"},
		{"no user ID", "GET", "/products", "", nil, 400, "invalid_request"},
		{"an empty user ID", "GET", "/products", "", map[string]string{"Nodoplex-User-Id": ""}, 400, "invalid_request"},
		{"a user ID that isn't valid percent-encoding", "GET", "/products", "", map[string]string{"Nodoplex-User-Id": "%zz"}, 400, "invalid_request"},
		{"a user ID that isn't UTF-8", "GET", "/products", "", map[string]string{"Nodoplex-User-Id": "%C3%28"}, 400, "invalid_request"},
		{"a slug that isn't valid", "GET", "/products/com.example/nodes", "", alice, 400, "invalid_request"},
		{"a slug with an uppercase letter", "PUT", "/products/com.Example.app", `{"fields":{"title":"x"}}`, alice, 400, "invalid_request"},
		{"a path parameter that isn't UTF-8", "GET", "/products/a%C3%28.b.c/nodes", "", alice, 400, "invalid_request"},
		{"an unknown query parameter", "GET", "/products?colour=red", "", alice, 400, "invalid_request"},
		{"a query parameter on a POST", "POST", "/products/com.example.app/nodes?type=task", "{}", alice, 400, "invalid_request"},
		{"a scalar query parameter 2 times", "GET", "/products/com.example.app/nodes?limit=1&limit=2", "", alice, 400, "invalid_request"},
		{"a query value that isn't valid percent-encoding", "GET", "/products?archived=%zz", "", alice, 400, "invalid_request"},
		{"userId outside Subscribe", "GET", "/products?userId=alice", "", alice, 400, "invalid_request"},
		{"a body that isn't JSON", "PUT", "/products/com.example.app", "{", alice, 400, "invalid_request"},
		{"a body that isn't an object", "PUT", "/products/com.example.app", "[]", alice, 400, "invalid_request"},
		{"2 JSON values", "PUT", "/products/com.example.app", "{} {}", alice, 400, "invalid_request"},
		{"a member name 2 times", "PUT", "/products/com.example.app", `{"fields":{"title":"a","title":"b"}}`, alice, 400, "invalid_request"},
		{"a member name 2 times, escaped", "PUT", "/products/com.example.app", `{"fields":{"title":"a","t\u0069tle":"b"}}`, alice, 400, "invalid_request"},
		{"a lone surrogate", "PUT", "/products/com.example.app", `{"fields":{"title":"\ud800"}}`, alice, 400, "invalid_request"},
		{"a lone low surrogate", "PUT", "/products/com.example.app", `{"fields":{"title":"\udc00x"}}`, alice, 400, "invalid_request"},
		{"an unknown body member", "PUT", "/products/com.example.app", `{"fields":{"title":"a"},"x":1}`, alice, 400, "invalid_request"},
		{"a body nested too deep", "PUT", "/products/com.example.app", strings.Repeat("[", 100) + strings.Repeat("]", 100), alice, 400, "invalid_request"},
		{"filterGroups that isn't JSON", "GET", "/products/com.example.app/poll?startChangeId=0&limit=1&filterGroups=%5B", "", alice, 400, "invalid_request"},
		{"a Last-Event-ID that isn't an integer", "GET", "/products/com.example.app/subscribe?userId=alice", "", map[string]string{"Last-Event-ID": "x"}, 400, "invalid_request"},
		{"a filter group with no member", "GET", "/products/com.example.app/subscribe?userId=alice&filterGroups=%5B%7B%7D%5D", "", nil, 400, "invalid_request"},
		{"a version with a fraction part", "POST", "/products/com.example.app/nodes/00000000-0000-0000-0000-000000000001/archive", `{"version":1.0}`, alice, 400, "invalid_request"},
		{"a version with an exponent part", "POST", "/products/com.example.app/nodes/00000000-0000-0000-0000-000000000001/archive", `{"version":1e2}`, alice, 400, "invalid_request"},
		{"a version of null", "POST", "/products/com.example.app/nodes/00000000-0000-0000-0000-000000000001/archive", `{"version":null}`, alice, 400, "invalid_request"},
		{"no version", "POST", "/products/com.example.app/nodes/00000000-0000-0000-0000-000000000001/archive", ``, alice, 400, "invalid_request"},
		{"an id that isn't a UUID", "GET", "/products/com.example.app/nodes/{00000000-0000-0000-0000-000000000001}", "", alice, 400, "invalid_request"},
		{"a limit above 200", "GET", "/products/com.example.app/nodes?limit=201", "", alice, 400, "invalid_request"},
		{"a limit with a plus sign", "GET", "/products/com.example.app/poll?startChangeId=0&limit=%2B5", "", alice, 400, "invalid_request"},
		{"no limit", "GET", "/products/com.example.app/poll?startChangeId=0", "", alice, 400, "invalid_request"},
		{"a ParentRef with an unknown member", "POST", "/products/com.example.app/nodes", `{"type":"task","parents":[{"id":"00000000-0000-0000-0000-000000000001","x":1}],"fields":{"title":"t"}}`, alice, 400, "invalid_request"},
		{"no parents", "POST", "/products/com.example.app/nodes", `{"type":"task","parents":[],"fields":{"title":"t"}}`, alice, 400, "invalid_request"},
		{"UpdateNode with empty fields", "PATCH", "/products/com.example.app/nodes/00000000-0000-0000-0000-000000000001", `{"version":1,"fields":{}}`, alice, 400, "invalid_request"},
		{"WatchNode with a body member", "POST", "/products/com.example.app/nodes/00000000-0000-0000-0000-000000000001/watch", `{"version":1}`, alice, 400, "invalid_request"},
		{"an operation before the server has started", "GET", "/products", "", alice, 500, "internal_error"},
		{"startupz before the server has started", "GET", "/startupz", "", nil, 503, "unavailable"},
		{"readyz before the server has started", "GET", "/readyz?x=%zz", "", nil, 503, "unavailable"},
		{"a health check with a method other than GET", "POST", "/livez", "", nil, 405, "method_not_allowed"},
	}
	for _, c := range cases {
		rec := serve(c.method, c.target, c.body, c.header)
		if rec.Code != c.status || rec.Header().Get("Nodoplex-Error-Code") != c.code {
			t.Errorf("%s: got %d %s, want %d %s (%s)", c.name, rec.Code, rec.Header().Get("Nodoplex-Error-Code"), c.status, c.code, rec.Body)
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "*" || rec.Header().Get("Content-Type") != "application/json" ||
			!strings.Contains(rec.Body.String(), `"code":"`+c.code+`"`) {
			t.Errorf("%s: a refusal carries the CORS headers and an Error body, got %v %s", c.name, rec.Header(), rec.Body)
		}
	}
}

func TestOptionsAndHead(t *testing.T) {
	rec := serve("OPTIONS", "/anything", "", nil)
	if rec.Code != 204 || rec.Header().Get("Access-Control-Allow-Headers") != "Content-Type, Last-Event-ID, Nodoplex-User-Id" ||
		rec.Header().Get("Access-Control-Allow-Methods") != "GET, HEAD, PUT, POST, PATCH, DELETE" || rec.Body.Len() != 0 {
		t.Errorf("OPTIONS: %d %v", rec.Code, rec.Header())
	}
	rec = serve("POST", "/products/com.example.app/next-work", "", alice)
	if rec.Code != 405 || rec.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("405 must give Allow: %d %v", rec.Code, rec.Header())
	}
	rec = serve("HEAD", "/products/com.example.app/nodes?limit=0", "", alice)
	if rec.Code != 400 || rec.Header().Get("Nodoplex-Error-Code") != "invalid_request" {
		t.Errorf("HEAD runs the checks of GET: %d %v", rec.Code, rec.Header())
	}
}

// TestLivez checks that /livez needs no user ID, ignores its query (L4), and
// sends status 200 and no body (L1).
func TestLivez(t *testing.T) {
	for _, method := range []string{"GET", "HEAD"} {
		rec := serve(method, "/livez?x=%zz", "", nil)
		if rec.Code != 200 || rec.Body.Len() != 0 {
			t.Errorf("%s /livez: %d %q", method, rec.Code, rec.Body)
		}
	}
}

func TestInputHelpers(t *testing.T) {
	for in, want := range map[string]bool{
		"com.example.app": true, "a.b.c": true, "com.example": false, "com.-x.app": false, "com.x-.app": false,
		"com..app": false, "com.example.App": false, "com.ex_ample.app": false, strings.Repeat("a", 64) + ".b.c": false,
		strings.Repeat("a.", 126) + "a": true, strings.Repeat("a.", 127) + "a": false,
	} {
		if validSlug(in) != want {
			t.Errorf("validSlug(%q) = %v", in, !want)
		}
	}
	for in, want := range map[string]bool{
		"0190e2a1-7c3b-7d4e-8f00-0123456789ab": true, "0190E2A1-7C3B-7D4E-8F00-0123456789AB": true,
		"0190e2a17c3b7d4e8f000123456789ab": false, "{0190e2a1-7c3b-7d4e-8f00-0123456789ab}": false,
		"urn:uuid:0190e2a1-7c3b-7d4e-8f00-0123456789ab": false,
	} {
		if _, ok := parseUUID(in); ok != want {
			t.Errorf("parseUUID(%q) = %v", in, ok)
		}
	}
	if s, ok := percentDecode("%20alice%C3%A9"); !ok || s != " aliceé" {
		t.Errorf("percentDecode: %q %v", s, ok)
	}
	q, err := parseQuery("a=1+2&&b&c=%26&a=x%3Dy")
	if err != nil || !slices.Equal(q["a"], []string{"1 2", "x=y"}) || q["b"][0] != "" || q["c"][0] != "&" {
		t.Errorf("parseQuery: %v %v", q, err)
	}
	if _, err := decodeJSON([]byte(`{"a":"😀"}`)); err != nil {
		t.Errorf("a surrogate pair is valid: %v", err)
	}
}
