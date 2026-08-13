package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/IceShack/job-scout/scraper/internal/model"
	"github.com/IceShack/job-scout/scraper/internal/store"
)

func testServer(t *testing.T, password string) *Server {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(Options{
		Title: "test-scout", Store: st, Password: password,
		Run: func() {}, LastRun: func() time.Time { return time.Time{} },
	})
}

func spec(t *testing.T, s *Server) map[string]any {
	t.Helper()
	// Round-trip through JSON: the document is only useful if it marshals.
	data, err := json.Marshal(s.Spec())
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func paths(t *testing.T, s *Server) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for path, ops := range spec(t, s)["paths"].(map[string]any) {
		out[path] = map[string]any{}
		for method, op := range ops.(map[string]any) {
			out[path][method] = op
		}
	}
	return out
}

// The route table feeds both the mux and the document, so every served
// route must be documented — this is what stops the two drifting.
func TestSpecDocumentsEveryRoute(t *testing.T) {
	s := testServer(t, "")
	documented := paths(t, s)
	for _, rt := range s.routes() {
		path := strings.TrimSuffix(rt.Path, "{$}")
		if path == "" {
			path = "/"
		}
		ops, ok := documented[path]
		if !ok {
			t.Errorf("route %s %s is not in the document", rt.Method, rt.Path)
			continue
		}
		if _, ok := ops[strings.ToLower(rt.Method)]; !ok {
			t.Errorf("route %s %s: method missing from %s", rt.Method, rt.Path, path)
		}
		delete(ops, strings.ToLower(rt.Method))
		if len(ops) == 0 {
			delete(documented, path)
		}
	}
	for path := range documented {
		t.Errorf("document describes %s, which no route serves", path)
	}
}

// Everything the document promises must actually answer.
func TestHandlerServesEveryDocumentedRoute(t *testing.T) {
	s := testServer(t, "")
	h := s.Handler()
	for _, rt := range s.routes() {
		path := strings.NewReplacer("{$}", "", "{id}", "nope").Replace(rt.Path)
		req := httptest.NewRequest(rt.Method, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound && !strings.Contains(rt.Path, "{id}") {
			t.Errorf("%s %s: not registered (404)", rt.Method, rt.Path)
		}
		// The documented status codes must be the ones actually used.
		codes := map[int]bool{}
		for _, r := range rt.Responses {
			codes[r.Code] = true
		}
		if !codes[rec.Code] {
			t.Errorf("%s %s: answered %d, which the document does not list", rt.Method, rt.Path, rec.Code)
		}
	}
}

// The Job schema is reflected off the struct; if a field is added without
// a json tag, or the reflection misses one, this catches it.
func TestJobSchemaMatchesTheStruct(t *testing.T) {
	schemas := spec(t, testServer(t, ""))["components"].(map[string]any)["schemas"].(map[string]any)
	job := schemas["Job"].(map[string]any)
	props := job["properties"].(map[string]any)

	jobType := reflect.TypeOf(model.Job{})
	for i := range jobType.NumField() {
		f := jobType.Field(i)
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" || !f.IsExported() {
			continue
		}
		if _, ok := props[name]; !ok {
			t.Errorf("field %s (json %q) is missing from the Job schema", f.Name, name)
		}
		required := !strings.Contains(opts, "omitempty") && !strings.Contains(opts, "omitzero")
		listed := false
		for _, r := range job["required"].([]any) {
			if r == name {
				listed = true
			}
		}
		if required != listed {
			t.Errorf("field %s: required=%v in the struct tags, %v in the schema", name, required, listed)
		}
	}
	if len(props) != jobType.NumField() {
		t.Errorf("schema has %d properties, struct has %d fields", len(props), jobType.NumField())
	}

	// Status is a named schema so generators emit an enum type for it.
	status := schemas["Status"].(map[string]any)
	if got := len(status["enum"].([]any)); got != len(model.Statuses)+1 {
		t.Errorf("Status enum has %d values, want %d statuses plus the empty one", got, len(model.Statuses))
	}
	if ref := props["status"].(map[string]any)["$ref"]; ref != "#/components/schemas/Status" {
		t.Errorf("job.status should reference the Status schema, got %v", ref)
	}
}

func TestOpenAPIEndpoint(t *testing.T) {
	rec := httptest.NewRecorder()
	testServer(t, "").Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/openapi.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("content-type = %q", got)
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("served document is not JSON: %v", err)
	}
	if doc["openapi"] != "3.1.0" {
		t.Errorf("openapi = %v", doc["openapi"])
	}
	if title := doc["info"].(map[string]any)["title"]; title != "test-scout API" {
		t.Errorf("title = %v, want the app title", title)
	}
}

// Auth is part of the API surface: it is only described when it is on.
func TestSecurityIsDocumentedOnlyWithAPassword(t *testing.T) {
	open := spec(t, testServer(t, ""))
	if _, ok := open["security"]; ok {
		t.Error("an unauthenticated server should not declare security")
	}
	if _, ok := open["paths"].(map[string]any)["/login"]; ok {
		t.Error("an unauthenticated server should not document /login")
	}

	guarded := spec(t, testServer(t, "hunter2"))
	if _, ok := guarded["paths"].(map[string]any)["/login"]; !ok {
		t.Error("/login is missing from a password-protected server")
	}
	schemes := guarded["components"].(map[string]any)["securitySchemes"].(map[string]any)
	session := schemes["session"].(map[string]any)
	if session["name"] != authCookie || session["in"] != "cookie" {
		t.Errorf("session scheme = %v, want the auth cookie", session)
	}
}

// Response codes are keyed by string in OpenAPI, not by number.
func TestResponseCodesAreStringKeys(t *testing.T) {
	for path, ops := range paths(t, testServer(t, "")) {
		for method, op := range ops {
			for code := range op.(map[string]any)["responses"].(map[string]any) {
				if _, err := strconv.Atoi(code); err != nil {
					t.Errorf("%s %s: response key %q is not a status code", method, path, code)
				}
			}
		}
	}
}
