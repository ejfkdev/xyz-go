package httpapi

import (
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"time"

	errs "github.com/ejfkdev/xyz-go/errors"
	"github.com/ejfkdev/xyz-go/registry"
	"github.com/ejfkdev/xyz-go/spec"
)

func registerOpenAPI(mux *http.ServeMux, reg *registry.Registry) {
	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		paths := map[string]any{}
		var order []string
		for _, e := range reg.All() {
			if e.HTTP.Method == "" || e.HTTP.Path == "" {
				continue
			}
			order = append(order, e.HTTP.Path+" "+e.HTTP.Method)
		}
		sort.Strings(order)
		for _, key := range order {
			path, method, _ := strings.Cut(key, " ")
			e, _ := entryFor(reg, path, method)
			op, ok := paths[path].(map[string]any)
			if !ok {
				op = map[string]any{}
				paths[path] = op
			}
			opMethod := map[string]any{}
			if e.Summary != "" {
				opMethod["summary"] = e.Summary
			}
			var params []any
			for _, f := range e.Root.Fields {
				if f.Skip || (f.HTTP.Location != "path" && f.HTTP.Location != "query") {
					continue
				}
				params = append(params, map[string]any{
					"name":     httpName(f),
					"in":       f.HTTP.Location,
					"required": f.Required,
					"schema":   map[string]any{"type": schemaType(f)},
				})
			}
			if len(params) > 0 {
				opMethod["parameters"] = params
			}
			switch {
			case method == http.MethodGet || method == http.MethodHead || method == http.MethodDelete:
				// 无请求体
			case method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch:
				opMethod["requestBody"] = map[string]any{
					"content": map[string]any{
						"application/json": map[string]any{"schema": json.RawMessage(schemaJSON(e))},
					},
				}
			}
			okResp := map[string]any{"description": "ok"}
			if e.OutputSchema != nil {
				if outJSON, err := json.Marshal(e.OutputSchema); err == nil {
					okResp["content"] = map[string]any{
						"application/json": map[string]any{"schema": json.RawMessage(outJSON)},
					}
				}
			}
			opMethod["responses"] = map[string]any{
				"200": okResp,
				"400": map[string]any{"description": errs.KindInvalidInput},
				"404": map[string]any{"description": errs.KindNotFound},
				"500": map[string]any{"description": errs.KindInternal},
			}
			op[strings.ToLower(method)] = opMethod
		}
		doc := map[string]any{
			"openapi": "3.0.3",
			"info":    map[string]any{"title": "example service", "version": "1"},
			"paths":   paths,
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(doc)
	})
}

func entryFor(reg *registry.Registry, path, method string) (*spec.Entry, bool) {
	for _, e := range reg.All() {
		if e.HTTP.Path == path && e.HTTP.Method == method {
			return e, true
		}
	}
	return nil, false
}

func schemaJSON(e *spec.Entry) []byte {
	b, err := json.Marshal(e.InputSchema)
	if err != nil {
		return []byte(`{}`)
	}
	return b
}

func schemaType(f *spec.FieldMeta) string {
	if f.Type == reflect.TypeOf([]byte(nil)) || f.Type == reflect.TypeOf(time.Time{}) ||
		f.Type == reflect.TypeOf(time.Duration(0)) {
		return "string"
	}
	switch f.Kind {
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice:
		return "array"
	case reflect.Struct:
		return "object"
	default:
		return "string"
	}
}

// Bearer wraps h with Bearer-token verification: the Authorization header
// must be "Bearer <token>" where token is one of tokens. An empty token
// list means no authentication (the handler is returned unchanged).
