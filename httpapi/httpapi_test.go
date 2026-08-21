package httpapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	errs "github.com/ejfkdev/xyz-go/errors"
	"github.com/ejfkdev/xyz-go/registry"
	"github.com/ejfkdev/xyz-go/spec"
)

type addHTTPArgs struct {
	Name string `json:"name" desc:"用户名" required:"true" validate:"min=2" http:"path"`
	Age  int    `json:"age" desc:"年龄" default:"18" http:"query"`
	Mode string `json:"mode" enum:"fast,slow" http:"query"`
}

type addHTTPResp struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Age  int    `json:"age"`
}

var idCounter int64

func addHTTP(_ context.Context, in *addHTTPArgs) (*addHTTPResp, error) {
	if in.Name == "missing" {
		return nil, errs.New(errs.KindNotFound, "no such user")
	}
	idCounter++
	return &addHTTPResp{ID: idCounter, Name: in.Name, Age: in.Age}, nil
}

type searchArgs struct {
	Query string `json:"query" desc:"关键词" required:"true"` // 无 http 标注 = 默认从 query 绑定
	K     int    `json:"k" default:"10" http:"query"`
}

func search(_ context.Context, in *searchArgs) ([]string, error) {
	return []string{in.Query, "ok"}, nil
}

func buildHTTPReg(t *testing.T) *registry.Registry {
	t.Helper()
	reg := registry.New()
	if _, err := spec.Define("user.add", addHTTP).
		Summary("创建用户").
		HTTP(spec.HTTPHints{Method: "POST", Path: "/users/{name}"}).
		Register(reg); err != nil {
		t.Fatal(err)
	}
	if _, err := spec.Define("search.query", search).
		Summary("搜索").
		HTTP(spec.HTTPHints{Method: "GET", Path: "/search"}).
		Register(reg); err != nil {
		t.Fatal(err)
	}
	// 未声明 HTTP 路由的命令：不应出现在路由表里。
	if _, err := spec.Define("cli.only", addHTTP).Summary("仅供 CLI").Register(reg); err != nil {
		t.Fatal(err)
	}
	return reg
}

func mustHandler(t *testing.T, reg *registry.Registry) http.Handler {
	t.Helper()
	h, err := Handler(reg)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	return h
}

func do(t *testing.T, h http.Handler, method, url, body string) (int, string) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, url, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestHTTPPathParamAndBody(t *testing.T) {
	h := mustHandler(t, buildHTTPReg(t))
	code, body := do(t, h, "POST", "/users/alice", `{"age": 9}`)
	if code != 200 {
		t.Fatalf("status = %d, body = %s", code, body)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if got["name"] != "alice" || got["age"] != float64(9) {
		t.Fatalf("resp = %v", got)
	}
}

func TestHTTPQueryBindingAndDefault(t *testing.T) {
	h := mustHandler(t, buildHTTPReg(t))
	// 缺 age：全局默认 18 由 Invoke 补齐。
	_, body := do(t, h, "GET", "/search?query=golang", "")
	if !strings.Contains(body, "golang") {
		t.Fatalf("body = %q", body)
	}
	// 显式 k 走 query。
	if _, body2 := do(t, h, "GET", "/search?query=g&k=3", ""); !strings.Contains(body2, `"ok"`) {
		t.Fatalf("body2 = %q", body2)
	}
}

func TestHTTPMethodMismatch(t *testing.T) {
	h := mustHandler(t, buildHTTPReg(t))
	if code, _ := do(t, h, "GET", "/users/alice", ""); code != http.StatusMethodNotAllowed {
		t.Fatalf("GET on POST route = %d, want 405", code)
	}
}

func TestHTTPErrorMapping(t *testing.T) {
	h := mustHandler(t, buildHTTPReg(t))
	// not_found → 404
	code, body := do(t, h, "POST", "/users/missing", `{}`)
	if code != http.StatusNotFound || !strings.Contains(body, "no such user") {
		t.Fatalf("code=%d body=%q", code, body)
	}
	// 校验失败（min=2）→ 400
	code2, body2 := do(t, h, "POST", "/users/x", `{}`)
	if code2 != http.StatusBadRequest || !strings.Contains(body2, "min") {
		t.Fatalf("code=%d body=%q", code2, body2)
	}
	// enum 违规 → 400
	if code3, _ := do(t, h, "POST", "/users/zed?mode=warp", `{}`); code3 != http.StatusBadRequest {
		t.Fatalf("enum violation code = %d, want 400", code3)
	}
}

func TestHTTPOpenAPIDoc(t *testing.T) {
	h := mustHandler(t, buildHTTPReg(t))
	code, body := do(t, h, "GET", "/openapi.json", "")
	if code != 200 || !strings.Contains(body, "/users/{name}") || !strings.Contains(body, `"openapi"`) {
		t.Fatalf("code=%d body=%q", code, body)
	}
}

func TestHTTPUnroutedEntry(t *testing.T) {
	h := mustHandler(t, buildHTTPReg(t))
	if code, _ := do(t, h, "POST", "/cli.only", `{}`); code != http.StatusNotFound {
		t.Fatalf("unrouted entry answered %d, want 404", code)
	}
}

func TestHTTPRouteConflict(t *testing.T) {
	reg := buildHTTPReg(t)
	if _, err := spec.Define("user.add2", addHTTP).
		HTTP(spec.HTTPHints{Method: "POST", Path: "/users/{name}"}).
		Register(reg); err != nil {
		t.Fatal(err)
	}
	if _, err := Handler(reg); err == nil {
		t.Fatal("conflicting route should error at build time")
	}
}

func TestBearerMiddleware(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	// 空令牌 = 不鉴权（原样透传）。
	if code, _ := do(t, Bearer(nil, inner), "GET", "/anything", ""); code != http.StatusOK {
		t.Fatalf("no-token should passthrough, got %d", code)
	}
	gate := Bearer([]string{"tok-a", "tok-b"}, inner)
	req := func(auth string) int {
		r := httptest.NewRequest("GET", "/x", nil)
		if auth != "" {
			r.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		gate.ServeHTTP(rec, r)
		return rec.Code
	}
	if code := req(""); code != http.StatusUnauthorized {
		t.Fatalf("missing auth = %d, want 401", code)
	}
	if code := req("Bearer wrong"); code != http.StatusUnauthorized {
		t.Fatalf("wrong token = %d, want 401", code)
	}
	if code := req("Bearer tok-a"); code != http.StatusOK {
		t.Fatalf("valid token = %d, want 200", code)
	}
	if code := req("Bearer tok-b"); code != http.StatusOK {
		t.Fatalf("second valid token = %d, want 200", code)
	}
}

func TestCORSMiddleware(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	// 空列表 = 关闭（透传且无头）。
	rec := httptest.NewRecorder()
	CORS(nil, inner).ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("disabled CORS leaked header %q", got)
	}
	gate := CORS([]string{"https://app.example"}, inner)
	req := func(origin, method string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/x", nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		gate.ServeHTTP(rec, r)
		return rec
	}
	if got := req("https://app.example", "GET").Header().Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Fatalf("allowed origin header = %q", got)
	}
	if got := req("https://evil.example", "GET").Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("disallowed origin got header %q", got)
	}
	pre := req("https://app.example", "OPTIONS")
	if pre.Code != http.StatusNoContent {
		t.Fatalf("preflight = %d, want 204", pre.Code)
	}
	// "*" 放行任意 Origin。
	any := CORS([]string{"*"}, inner)
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("Origin", "https://anything.example")
	rec2 := httptest.NewRecorder()
	any.ServeHTTP(rec2, r)
	if got := rec2.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("wildcard header = %q", got)
	}
}

func TestCORSBeforeAuthOrdering(t *testing.T) {
	// 浏览器预检（OPTIONS）不带凭据：CORS 必须在 Bearer 之外，否则预检被 401。
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	chain := CORS([]string{"*"}, Bearer([]string{"k"}, inner))
	r := httptest.NewRequest("OPTIONS", "/x", nil)
	r.Header.Set("Origin", "https://x.example")
	r.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, r)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight = %d, want 204 (CORS outermost)", rec.Code)
	}
}

func TestHealthz(t *testing.T) {
	h := mustHandler(t, buildHTTPReg(t))
	code, body := do(t, h, "GET", "/healthz", "")
	if code != 200 || !strings.Contains(body, `"status":"ok"`) {
		t.Fatalf("healthz: code=%d body=%q", code, body)
	}
}

func TestGzipMiddleware(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello gzip"))
	})
	// 客户端不接受时不压缩。
	rec := httptest.NewRecorder()
	Gzip(inner).ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != "hello gzip" {
		t.Fatalf("unaltered response: %q %q", rec.Header().Get("Content-Encoding"), rec.Body.String())
	}
	// 接受 gzip 时压缩并解压一致。
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec2 := httptest.NewRecorder()
	Gzip(inner).ServeHTTP(rec2, req)
	if rec2.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("encoding = %q", rec2.Header().Get("Content-Encoding"))
	}
	var buf bytes.Buffer
	z, err := gzip.NewReader(rec2.Body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(&buf, z); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "hello gzip" {
		t.Fatalf("gunzipped = %q", buf.String())
	}
}

func TestHTTPFormBinding(t *testing.T) {
	reg := buildHTTPReg(t)
	if _, err := spec.Define("form.submit", func(_ context.Context, in *struct {
		X string `json:"x" http:"form"`
	}) (string, error) {
		return "x=" + in.X, nil
	}).
		HTTP(spec.HTTPHints{Method: "POST", Path: "/form"}).
		Register(reg); err != nil {
		t.Fatal(err)
	}
	h := mustHandler(t, reg)
	// 表单体（非 JSON 声明）走 form 绑定，且不能被 JSON 解析误杀。
	req := httptest.NewRequest("POST", "/form", strings.NewReader("x=hello"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "x=hello") {
		t.Fatalf("form binding: code=%d body=%q", rec.Code, rec.Body.String())
	}
	// 显式声明 JSON 但坏体 → 400。
	req2 := httptest.NewRequest("POST", "/form", strings.NewReader("{oops"))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 400 {
		t.Fatalf("declared-JSON bad body: code=%d, want 400", rec2.Code)
	}
}
