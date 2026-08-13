package web

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/IceShack/job-scout/scraper/internal/model"
	"github.com/IceShack/job-scout/scraper/internal/version"
)

// The OpenAPI document is generated from the same route table that
// registers the handlers, and its schemas are reflected off the structs
// that are actually served — so the description cannot drift from the API.

// route is one endpoint: the entry both registers the handler and
// describes it.
type route struct {
	Method      string
	Path        string // net/http pattern, e.g. "/api/jobs/{id}/status"
	ID          string // operationId
	Summary     string
	Description string
	Params      []param
	Responses   []response
	Handler     http.HandlerFunc
}

type param struct {
	Name        string
	In          string // "path" or "query"
	Description string
	Required    bool
	Schema      map[string]any
}

type response struct {
	Code        int
	Description string
	// Schema nil means no body. MediaType defaults to application/json.
	Schema    map[string]any
	MediaType string
}

func str() map[string]any { return map[string]any{"type": "string"} }

// filterParams are accepted by both the UI and the JSON job list.
func filterParams() []param {
	statusValues := []any{"", "none", "tracked"}
	for _, st := range model.Statuses {
		statusValues = append(statusValues, string(st))
	}
	return []param{
		{Name: "q", In: "query", Description: "Free-text search over title, company and description.", Schema: str()},
		{Name: "source", In: "query", Description: "Keep only jobs from this source.", Schema: str()},
		{Name: "fit", In: "query", Description: "Keep only jobs whose fit starts with this label.", Schema: str()},
		{Name: "status", In: "query",
			Description: "Application status: a status name, \"none\" for untouched jobs, \"tracked\" for anything applied to.",
			Schema:      map[string]any{"type": "string", "enum": statusValues}},
		{Name: "min", In: "query", Description: "Minimum score.", Schema: map[string]any{"type": "integer"}},
		{Name: "hidden", In: "query", Description: "Set to 1 to list the hidden (\"not interested\") jobs instead.",
			Schema: map[string]any{"type": "string", "enum": []any{"", "1"}}},
	}
}

var jobIDParam = param{Name: "id", In: "path", Required: true,
	Description: "Job ID, as returned by /api/jobs.", Schema: str()}

func textResponse(code int, desc string) response {
	return response{Code: code, Description: desc, Schema: str(), MediaType: "text/plain"}
}

// routes is the single source of truth for the HTTP surface.
func (s *Server) routes() []route {
	jobSchema := map[string]any{"$ref": "#/components/schemas/Job"}
	statusValues := []any{""}
	for _, st := range model.Statuses {
		statusValues = append(statusValues, string(st))
	}
	return []route{
		{
			Method: "GET", Path: "/health", ID: "health",
			Summary:     "Liveness probe",
			Description: "Always reachable, even when a password is configured.",
			Responses:   []response{textResponse(200, "The service is up.")},
			Handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("ok"))
			},
		},
		{
			Method: "GET", Path: "/{$}", ID: "index",
			Summary:   "Job list, as HTML",
			Params:    filterParams(),
			Responses: []response{{Code: 200, Description: "The job table.", Schema: str(), MediaType: "text/html"}},
			Handler:   s.handleIndex,
		},
		{
			Method: "GET", Path: "/api/jobs", ID: "listJobs",
			Summary:     "Job list, as JSON",
			Description: "Matched jobs, highest score first, then most recently seen.",
			Params:      filterParams(),
			Responses: []response{{Code: 200, Description: "The matching jobs.",
				Schema: map[string]any{"type": "array", "items": jobSchema}}},
			Handler: s.handleJobs,
		},
		{
			Method: "POST", Path: "/api/jobs/{id}/status", ID: "setJobStatus",
			Summary:     "Set a job's application status",
			Description: "An empty value takes the job back out of the pipeline.",
			Params: []param{jobIDParam, {Name: "value", In: "query", Required: true,
				Description: "The new status.",
				Schema:      map[string]any{"type": "string", "enum": statusValues}}},
			Responses: []response{
				{Code: 204, Description: "Status updated."},
				textResponse(400, "Unknown status value."),
				textResponse(404, "No job with that ID."),
			},
			Handler: s.handleStatus,
		},
		{
			Method: "POST", Path: "/api/jobs/{id}/hide", ID: "hideJob",
			Summary:     "Mark a job \"not interested\"",
			Description: "The entry stays stored so the ad never resurfaces as new, including under a sibling URL.",
			Params:      []param{jobIDParam},
			Responses: []response{
				{Code: 204, Description: "Job hidden."},
				textResponse(404, "No job with that ID."),
			},
			Handler: s.setFlag(s.store.SetHidden, true),
		},
		{
			Method: "POST", Path: "/api/jobs/{id}/unhide", ID: "unhideJob",
			Summary:   "Restore a hidden job",
			Params:    []param{jobIDParam},
			Responses: []response{{Code: 204, Description: "Job restored."}, textResponse(404, "No job with that ID.")},
			Handler:   s.setFlag(s.store.SetHidden, false),
		},
		{
			Method: "POST", Path: "/api/run", ID: "triggerScrape",
			Summary:     "Trigger a scrape",
			Description: "Returns immediately; the run happens in the background and is skipped if one is already going.",
			Responses:   []response{textResponse(202, "Scrape triggered.")},
			Handler: func(w http.ResponseWriter, _ *http.Request) {
				s.trigger()
				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write([]byte("scrape triggered"))
			},
		},
		{
			Method: "GET", Path: "/openapi.json", ID: "openapi",
			Summary:   "This OpenAPI document",
			Responses: []response{{Code: 200, Description: "The OpenAPI 3.1 document.", Schema: map[string]any{"type": "object"}}},
			Handler:   s.handleOpenAPI,
		},
	}
}

func (s *Server) handleOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", " ")
	_ = enc.Encode(s.Spec())
}

// Spec builds the OpenAPI 3.1 document describing this server.
func (s *Server) Spec() map[string]any {
	g := &schemaGen{named: map[string]map[string]any{}}
	paths := map[string]any{}
	for _, rt := range s.routes() {
		op := map[string]any{
			"operationId": rt.ID,
			"summary":     rt.Summary,
			"responses":   responsesSpec(rt.Responses),
		}
		if rt.Description != "" {
			op["description"] = rt.Description
		}
		if params := paramsSpec(rt.Params); params != nil {
			op["parameters"] = params
		}
		path := strings.TrimSuffix(rt.Path, "{$}")
		if path == "" {
			path = "/"
		}
		methods, _ := paths[path].(map[string]any)
		if methods == nil {
			methods = map[string]any{}
			paths[path] = methods
		}
		methods[strings.ToLower(rt.Method)] = op
	}

	// Registering Job pulls in Status and every field type with it.
	g.register("Job", reflect.TypeOf(model.Job{}))

	spec := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title": s.title + " API",
			// The API is versioned with the release; there is one number.
			"version":     version.Version,
			"description": "Self-hosted job-search scraper: the matched jobs, their application status, and a manual scrape trigger.",
			"license":     map[string]any{"name": "MIT", "identifier": "MIT"},
		},
		"servers":    []any{map[string]any{"url": "/", "description": "This instance."}},
		"paths":      paths,
		"components": map[string]any{"schemas": g.named},
	}

	if s.password != "" {
		spec["components"].(map[string]any)["securitySchemes"] = map[string]any{
			"session": map[string]any{
				"type": "apiKey", "in": "cookie", "name": authCookie,
				"description": "Set by POST /login with the shared site password. /health is exempt.",
			},
		}
		spec["security"] = []any{map[string]any{"session": []any{}}}
		// /login is served by the auth middleware rather than the mux, so
		// it is described here instead of in the route table.
		paths["/login"] = map[string]any{"post": map[string]any{
			"operationId": "login",
			"summary":     "Exchange the site password for a session cookie",
			"security":    []any{},
			"requestBody": map[string]any{"required": true, "content": map[string]any{
				"application/x-www-form-urlencoded": map[string]any{"schema": map[string]any{
					"type":       "object",
					"required":   []any{"password"},
					"properties": map[string]any{"password": str()},
				}},
			}},
			"responses": map[string]any{
				"303": map[string]any{"description": "Signed in; the session cookie is set."},
				"401": map[string]any{"description": "Wrong password."},
			},
		}}
	}
	return spec
}

func paramsSpec(params []param) []any {
	if len(params) == 0 {
		return nil
	}
	out := make([]any, 0, len(params))
	for _, p := range params {
		out = append(out, map[string]any{
			"name": p.Name, "in": p.In, "required": p.Required,
			"description": p.Description, "schema": p.Schema,
		})
	}
	return out
}

func responsesSpec(responses []response) map[string]any {
	out := map[string]any{}
	for _, r := range responses {
		spec := map[string]any{"description": r.Description}
		if r.Schema != nil {
			media := r.MediaType
			if media == "" {
				media = "application/json"
			}
			spec["content"] = map[string]any{media: map[string]any{"schema": r.Schema}}
		}
		out[strconv.Itoa(r.Code)] = spec
	}
	return out
}

// schemaGen reflects Go types into JSON Schema, so the documented shape of
// a job is the shape the server actually marshals.
type schemaGen struct {
	named map[string]map[string]any
}

var (
	timeType   = reflect.TypeOf(time.Time{})
	statusType = reflect.TypeOf(model.Status(""))
)

func (g *schemaGen) register(name string, t reflect.Type) map[string]any {
	if _, seen := g.named[name]; !seen {
		g.named[name] = nil // placeholder: breaks self-referencing types
		g.named[name] = g.schema(t)
	}
	return map[string]any{"$ref": "#/components/schemas/" + name}
}

func (g *schemaGen) schema(t reflect.Type) map[string]any {
	switch t {
	case timeType:
		return map[string]any{"type": "string", "format": "date-time"}
	case statusType:
		values := []any{""}
		for _, st := range model.Statuses {
			values = append(values, string(st))
		}
		return map[string]any{"type": "string", "enum": values,
			"description": "How far the application has got; empty means nothing was sent."}
	}
	switch t.Kind() {
	case reflect.Pointer:
		return g.schema(t.Elem())
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.String:
		return str()
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": g.schema(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": g.schema(t.Elem())}
	case reflect.Struct:
		return g.structSchema(t)
	default:
		return map[string]any{}
	}
}

func (g *schemaGen) structSchema(t reflect.Type) map[string]any {
	props := map[string]any{}
	var required []any
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		if f.Type == statusType {
			props[name] = g.register("Status", f.Type)
		} else {
			props[name] = g.schema(f.Type)
		}
		// A field the server always emits is a field clients can rely on.
		if !strings.Contains(opts, "omitempty") && !strings.Contains(opts, "omitzero") {
			required = append(required, name)
		}
	}
	schema := map[string]any{"type": "object", "properties": props}
	if required != nil {
		schema["required"] = required
	}
	return schema
}
