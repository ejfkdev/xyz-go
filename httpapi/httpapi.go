// Package httpapi is the HTTP frontend, implemented on the standard library
// only (net/http with method-pattern routing). It turns the registry's HTTP
// hints into routes:
//
//   - HTTPHints.Method + Path define the route; path placeholders {name}
//     map to fields tagged http:"path" (via r.PathValue).
//   - 未标注 http 位置或标注 http:"query" 的字段默认从 query 绑定；
//     http:"header"（httpName）从请求头绑定；JSON body 合并为入参基底。
//   - Responses are bare JSON (no envelope), the same shape as CLI --json;
//     errors map to HTTP status codes through the shared error taxonomy.
//   - GET /openapi.json exposes an OpenAPI 3 document generated from the
//     same InputSchema the MCP frontend uses.
//
// Entries without HTTP hints are not routed. This package pulls zero
// third-party dependencies.
package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"

	errs "github.com/ejfkdev/xyz-go/errors"
	"github.com/ejfkdev/xyz-go/registry"
	"github.com/ejfkdev/xyz-go/spec"
)

const maxBodyBytes = 1 << 20 // 请求体上限 1MiB

// Handler builds the router for registered HTTP routes plus /openapi.json.
// Conflicting method+path registrations are errors.
func Handler(reg *registry.Registry) (http.Handler, error) {
	return HandlerWith(reg, nil)
}

// HandlerWith 是带通道级默认参数的 Handler（serve --default k=v 注入：
// 缺席键补上、显式入参优先）。
func HandlerWith(reg *registry.Registry, defaults map[string]string) (http.Handler, error) {
	if reg == nil {
		return nil, fmt.Errorf("httpapi: nil registry")
	}
	mux := http.NewServeMux()
	for _, e := range reg.All() {
		if e.HTTP.Skip || e.CLI.Daemon {
			continue // 通道层面整体移除；Daemon 只属于 CLI
		}
		if e.HTTP.Method == "" || e.HTTP.Path == "" {
			continue // 该命令没有声明 HTTP 路由（CLI/MCP 专用）
		}
		if err := registerSafe(mux, e, defaults); err != nil {
			return nil, err
		}
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
	})
	registerOpenAPI(mux, reg)
	return mux, nil
}

// registerSafe 用 recover 把标准库 mux 的路由冲突 panic 转成注册期错误。
func registerSafe(mux *http.ServeMux, e *spec.Entry, defaults map[string]string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("httpapi: route %q %q conflicts with an existing route", e.HTTP.Method, e.HTTP.Path)
		}
	}()
	mux.HandleFunc(e.HTTP.Method+" "+e.HTTP.Path, makeHTTPHandler(e, defaults))
	return nil
}

func makeHTTPHandler(e *spec.Entry, defaults map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m := map[string]any{}
		// 铺底：HTTP 专属默认值（全局默认由 Invoke 补齐）。
		for k, v := range e.HTTPDefaults() {
			m[k] = v
		}
		// 通道级默认参数（serve --default k=v）：只补缺席键。
		for k, v := range defaults {
			if _, ok := m[k]; !ok {
				m[k] = v
			}
		}
		// JSON body 作为基础入参（非 GET/HEAD 且带体时解析）；
		// 读出的字节同时服务于后文的 form 绑定（避免二读 body）。
		var bodyBytes []byte
		if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
			if body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes)); err == nil && len(body) > 0 {
				bodyBytes = body
				var parsed map[string]any
				jsonDeclared := strings.Contains(r.Header.Get("Content-Type"), "application/json")
				switch {
				case json.Unmarshal(body, &parsed) == nil:
					for k, v := range parsed {
						m[k] = v
					}
				case jsonDeclared:
					// 显式声明 JSON 却解析失败：报 400。
					writeError(w, http.StatusBadRequest, "invalid JSON body")
					return
				default:
					// 非 JSON 声明且解析失败（如表单体）：交给后续 form 绑定，
					// 不在此处提前判死。
				}
			}
		}
		for _, f := range e.Root.Fields {
			if f.Skip {
				// json:"-" 的注入字段：header 值以 Go 字段名为键送达。
				if f.HTTP.Location == "header" {
					if v := r.Header.Get(httpName(f)); v != "" {
						m[f.Name] = v
					}
				}
				continue
			}
			// 未标注 http 位置的普通字段默认从 query 绑定（GET 命令的自然语义）。
			switch f.HTTP.Location {
			case "query", "":
				if vs, ok := r.URL.Query()[f.JSONName]; ok && len(vs) > 0 {
					if isStringSlice(f) {
						m[f.JSONName] = vs
					} else {
						m[f.JSONName] = vs[0]
					}
				}
			case "header":
				if v := r.Header.Get(httpName(f)); v != "" {
					m[f.JSONName] = v
				}
			case "path":
				if v := r.PathValue(httpName(f)); v != "" {
					m[f.JSONName] = v
				}
			case "form":
				if formVals, err := url.ParseQuery(string(bodyBytes)); err == nil {
					if vs, ok := formVals[f.JSONName]; ok && len(vs) > 0 {
						m[f.JSONName] = vs[0]
					}
				}
			}
		}
		out, err := e.Invoke(r.Context(), m)
		if err != nil {
			writeError(w, errs.HTTPStatus(errs.Classify(err)), causeMessage(err))
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
	}
}

// httpName 计算线上名：httpName 覆盖 > JSON 名 > Go 字段名。
func httpName(f *spec.FieldMeta) string {
	if f.HTTP.Name != "" {
		return f.HTTP.Name
	}
	if f.JSONName != "" {
		return f.JSONName
	}
	return f.Name
}

func isStringSlice(f *spec.FieldMeta) bool {
	return f.Kind == reflect.Slice && f.Type != reflect.TypeOf([]byte(nil))
}

func causeMessage(err error) string {
	if cause := errs.Cause(err); cause != nil {
		return cause.Error()
	}
	return err.Error()
}

func writeError(w http.ResponseWriter, status int, msg string) {
	if msg == "" {
		msg = http.StatusText(status)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// registerOpenAPI 暴露一份由同源 InputSchema 生成的 OpenAPI 3 文档。

// HandlerFor returns the standalone handler for one registered entry: the
// full binding (query/path/header/body) and error mapping, without routing.
// Mount it onto any router (gin.WrapH, echo, chi) or wrap it with your own
// middleware; Handler below composes all routed entries plus /healthz and
// /openapi.json.
func HandlerFor(e *spec.Entry) http.HandlerFunc {
	return HandlerForWith(e, nil)
}

// HandlerForWith 是带通道级默认参数的 HandlerFor。
func HandlerForWith(e *spec.Entry, defaults map[string]string) http.HandlerFunc {
	if e == nil {
		return func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusNotFound, "not found")
		}
	}
	return makeHTTPHandler(e, defaults)
}
