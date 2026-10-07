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

func registerOpenAPI(mux *http.ServeMux, reg *registry.Registry, meta ResponseMeta) {
	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		paths := map[string]any{}
		type opKey struct{ path, method string }
		var order []opKey
		for _, e := range reg.All() {
			if e.HTTP.Skip || e.CLI.Daemon {
				continue
			}
			for _, m := range httpMethods(e) {
				order = append(order, opKey{e.HTTP.Path, m})
			}
		}
		sort.Slice(order, func(i, j int) bool {
			if order[i].path != order[j].path {
				return order[i].path < order[j].path
			}
			return order[i].method < order[j].method
		})
		for _, k := range order {
			e, ok := entryFor(reg, k.path, k.method)
			if !ok {
				continue
			}
			op, _ := paths[k.path].(map[string]any)
			if op == nil {
				op = map[string]any{}
				paths[k.path] = op
			}
			opMethod := map[string]any{}
			if e.Summary != "" {
				opMethod["summary"] = e.Summary
			}
			if e.Description != "" {
				opMethod["description"] = e.Description
			}
			// 参数：path/query/header 字段，复用 InputSchema 的富逐字段 schema
			//（类型/描述/enum/default/format），而非裸类型。未标注 http 位置
			// 的字段默认 query（与 §11.2 绑定一致）。
			props := map[string]*spec.Schema{}
			if e.InputSchema != nil && e.InputSchema.Properties != nil {
				props = e.InputSchema.Properties
			}
			var params []any
			for _, f := range e.Root.Fields {
				if f.Skip {
					continue
				}
				loc := f.HTTP.Location
				if loc == "" {
					loc = "query"
				}
				if loc != "path" && loc != "query" && loc != "header" {
					continue
				}
				p := map[string]any{
					"name":     httpName(f),
					"in":       loc,
					"required": f.Required || loc == "path",
				}
				sch := props[f.JSONName]
				desc := f.Description
				if sch != nil {
					p["schema"] = sch
					if sch.Description != "" {
						desc = sch.Description
					}
				} else {
					p["schema"] = map[string]any{"type": schemaType(f)}
				}
				if desc != "" {
					p["description"] = desc
				}
				params = append(params, p)
			}
			if len(params) > 0 {
				opMethod["parameters"] = params
			}
			switch k.method {
			case http.MethodPost, http.MethodPut, http.MethodPatch:
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
			op[strings.ToLower(k.method)] = opMethod
		}
		// info 用应用身份（与 X-App-Name/X-App-Version 同源）；未设置回退参考值。
		title := meta.AppName
		if title == "" {
			title = "example service"
		}
		version := meta.AppVersion
		if version == "" {
			version = "1"
		}
		doc := map[string]any{
			"openapi": "3.0.3",
			"info":    map[string]any{"title": title, "version": version},
			"paths":   paths,
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(doc)
	})
}

// entryFor 按 path + method 找到入口；method 匹配该入口的 httpMethods 集合
//（含默认 GET+POST）。
func entryFor(reg *registry.Registry, path, method string) (*spec.Entry, bool) {
	for _, e := range reg.All() {
		if e.HTTP.Skip || e.CLI.Daemon || e.HTTP.Path != path {
			continue
		}
		for _, m := range httpMethods(e) {
			if m == method {
				return e, true
			}
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
