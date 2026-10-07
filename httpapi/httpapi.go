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
	"strconv"
	"strings"
	"time"

	errs "github.com/ejfkdev/xyz-go/errors"
	"github.com/ejfkdev/xyz-go/langx"
	"github.com/ejfkdev/xyz-go/registry"
	"github.com/ejfkdev/xyz-go/spec"
)

const maxBodyBytes = 1 << 20 // 请求体上限 1MiB

// 服务器上下文响应头（xyz-spec §11.4）。默认开启；NoAutoHeaders 关闭自动头，
// Headers 里的自定义头始终写入（它们是显式配置，不受开关影响）。
// X-App-* 报告*应用程序*身份，X-XYZ-* 报告 xyz 库自身与本次调用的上下文。
const (
	HeaderAppName    = "X-App-Name"
	HeaderAppVersion = "X-App-Version"
	HeaderSDKVersion = "X-XYZ-Version"
	HeaderCommand    = "X-XYZ-Command"
	HeaderDuration   = "X-XYZ-Duration-Ms"
)

// ResponseMeta 描述 HTTP 响应附带的服务器上下文：应用身份（名字/版本）、
// xyz 库版本、命令名、处理耗时与一批用户自定义静态头。零值即「不带任何
// 自动头、无自定义头」。
type ResponseMeta struct {
	// AppName 写入 X-App-Name；空则不写该头。
	AppName string
	// AppVersion 写入 X-App-Version（应用程序自己的版本）；空则不写。
	AppVersion string
	// SDKVersion 写入 X-XYZ-Version（xyz 库自身版本）；空则不写。
	SDKVersion string
	// Headers 是附加到每个响应的静态头（原样写入，键名不做规范化）。
	Headers map[string]string
	// NoAutoHeaders 置 true 时不写 X-App-*/X-XYZ-* 五个自动头；
	// Headers 自定义头不受影响。
	NoAutoHeaders bool
}

// autoHeaders 报告是否应写自动上下文头。
func (m ResponseMeta) autoHeaders() bool { return !m.NoAutoHeaders }

// hasAutoContent 报告是否有任何自动头内容可写（用于零开销直通判断）。
func (m ResponseMeta) hasAutoContent() bool {
	return m.AppName != "" || m.AppVersion != "" || m.SDKVersion != ""
}

// Handler builds the router for registered HTTP routes plus /openapi.json.
// Conflicting method+path registrations are errors.
func Handler(reg *registry.Registry) (http.Handler, error) {
	return HandlerWith(reg, nil)
}

// HandlerWith 是带通道级默认参数的 Handler（serve --default k=v 注入：
// 缺席键补上、显式入参优先）。
func HandlerWith(reg *registry.Registry, defaults map[string]string) (http.Handler, error) {
	return HandlerWithMeta(reg, defaults, ResponseMeta{})
}

// HandlerWithMeta 是带通道级默认参数与服务器上下文头的 Handler。
func HandlerWithMeta(reg *registry.Registry, defaults map[string]string, meta ResponseMeta) (http.Handler, error) {
	if reg == nil {
		return nil, fmt.Errorf("httpapi: nil registry")
	}
	mux := http.NewServeMux()
	for _, e := range reg.All() {
		if e.HTTP.Skip || e.CLI.Daemon {
			continue // 通道层面整体移除；Daemon 只属于 CLI
		}
		methods := httpMethods(e)
		if len(methods) == 0 {
			continue // 无 HTTP path：该命令不路由（CLI/MCP 专用）
		}
		for _, m := range methods {
			if err := registerSafe(mux, e, m, defaults, meta); err != nil {
				return nil, err
			}
		}
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
	})
	registerOpenAPI(mux, reg, meta)
	return mux, nil
}

// httpMethods 返回该入口路由的 HTTP 方法集：解析 HTTPHints.Method（逗号
// 分隔、大写归一），空则默认 GET+POST（xyz-spec §11.1）；无 Path 返回 nil
//（不路由）。
func httpMethods(e *spec.Entry) []string {
	if e.HTTP.Path == "" {
		return nil
	}
	if raw := strings.TrimSpace(e.HTTP.Method); raw != "" {
		var out []string
		for _, m := range strings.Split(raw, ",") {
			if m = strings.ToUpper(strings.TrimSpace(m)); m != "" {
				out = append(out, m)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return []string{http.MethodGet, http.MethodPost}
}

// registerSafe 用 recover 把标准库 mux 的路由冲突 panic 转成注册期错误。
func registerSafe(mux *http.ServeMux, e *spec.Entry, method string, defaults map[string]string, meta ResponseMeta) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("httpapi: route %q %q conflicts with an existing route", method, e.HTTP.Path)
		}
	}()
	mux.HandleFunc(method+" "+e.HTTP.Path, makeHTTPHandler(e, defaults, meta))
	return nil
}

func makeHTTPHandler(e *spec.Entry, defaults map[string]string, meta ResponseMeta) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// 逐请求语言：Accept-Language 命中受支持语言则用之，否则回退进程默认
		// 语言；写入请求 ctx，供 Invoke 管线、handler 与本地化框架消息共用
		//（xyz-spec §11.7）。
		lang := langx.Lang()
		if al := r.Header.Get("Accept-Language"); al != "" {
			if l, ok := langx.ParseAcceptLanguage(al); ok {
				lang = l
			}
		}
		ctx := langx.WithLang(r.Context(), lang)
		// 路由级上下文头：命令名先写（耗时在 Invoke 之后补）。
		if meta.autoHeaders() {
			w.Header().Set(HeaderCommand, e.Name)
		}
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
					// 显式声明 JSON 却解析失败：报 400（消息按请求语言本地化）。
					writeError(w, http.StatusBadRequest, langx.Tctx(ctx, "http.err_invalid_json"))
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
		out, err := e.Invoke(ctx, m)
		if meta.autoHeaders() {
			w.Header().Set(HeaderDuration, strconv.FormatInt(time.Since(start).Milliseconds(), 10))
		}
		if err != nil {
			writeErr(w, err)
			return
		}
		if e.HTTP.Output != nil {
			_ = e.HTTP.Output(w, r, e, out)
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

// writeErr 渲染一个命令错误：状态码取显式覆盖 > Kind 派生，错误体用三通道
// 共享的 errs.Body——扁平 error 字符串与 v0.4.2 之前逐字节一致，kind/code/
// detail 为增补键（xyz-spec §8.3）。
func writeErr(w http.ResponseWriter, err error) {
	writeErrorBody(w, errs.StatusFor(err), errs.ErrorBody(err))
}

// writeError 渲染一个传输层字符串错误（鉴权失败、坏 JSON 体等）：状态码显式
// 给定，错误体只带扁平 error 键。
func writeError(w http.ResponseWriter, status int, msg string) {
	if msg == "" {
		msg = http.StatusText(status)
	}
	writeErrorBody(w, status, errs.Body{Error: msg})
}

// writeErrorBody 以紧凑 JSON 写错误体（不缩进，与历史错误响应逐字节兼容；
// 成功响应体才缩进）。
func writeErrorBody(w http.ResponseWriter, status int, body errs.Body) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
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
	return HandlerForWithMeta(e, defaults, ResponseMeta{})
}

// HandlerForWithMeta 是带通道级默认参数与服务器上下文头的 HandlerFor。
func HandlerForWithMeta(e *spec.Entry, defaults map[string]string, meta ResponseMeta) http.HandlerFunc {
	if e == nil {
		return func(w http.ResponseWriter, r *http.Request) {
			lang := langx.Lang()
			if al := r.Header.Get("Accept-Language"); al != "" {
				if l, ok := langx.ParseAcceptLanguage(al); ok {
					lang = l
				}
			}
			writeError(w, http.StatusNotFound, langx.Tctx(langx.WithLang(r.Context(), lang), "http.err_not_found"))
		}
	}
	return makeHTTPHandler(e, defaults, meta)
}
