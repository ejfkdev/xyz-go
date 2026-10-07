package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ejfkdev/xyz-go/registry"
	"github.com/ejfkdev/xyz-go/spec"
)

// openapi.json 富化：operation description、参数 description + 富 schema
//（enum/default）、应用身份 info。
func TestOpenAPIRich(t *testing.T) {
	reg := registry.New()
	type sArgs struct {
		Q     string `json:"q" desc:"搜索关键词" required:"true"`
		Limit int    `json:"limit" desc:"条数上限" default:"10"`
		Mode  string `json:"mode" desc:"模式" enum:"fast,slow"`
	}
	if _, err := spec.Define("search.q", func(_ context.Context, in *sArgs) (string, error) {
		return in.Q, nil
	}).Summary("搜索").Description("按关键词搜索文档并返回匹配。").
		HTTP(spec.HTTPHints{Method: "GET", Path: "/search"}).Register(reg); err != nil {
		t.Fatal(err)
	}
	h, err := HandlerWithMeta(reg, nil, ResponseMeta{AppName: "myapp", AppVersion: "v9.9.9"})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/openapi.json", nil))
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("openapi not JSON: %v", err)
	}
	info := doc["info"].(map[string]any)
	if info["title"] != "myapp" || info["version"] != "v9.9.9" {
		t.Fatalf("info should use app identity, got %v", info)
	}
	op := doc["paths"].(map[string]any)["/search"].(map[string]any)["get"].(map[string]any)
	if op["summary"] != "搜索" {
		t.Fatalf("summary = %v", op["summary"])
	}
	if d, _ := op["description"].(string); !strings.Contains(d, "按关键词搜索") {
		t.Fatalf("operation description missing: %v", op["description"])
	}
	params := op["parameters"].([]any)
	if len(params) != 3 {
		t.Fatalf("want 3 params (q/limit/mode), got %d", len(params))
	}
	byName := map[string]map[string]any{}
	for _, p := range params {
		pm := p.(map[string]any)
		byName[pm["name"].(string)] = pm
	}
	q := byName["q"]
	if q == nil || q["required"] != true || q["description"] != "搜索关键词" {
		t.Fatalf("q param = %v", q)
	}
	if qsch := q["schema"].(map[string]any); qsch["type"] != "string" || qsch["description"] != "搜索关键词" {
		t.Fatalf("q schema not rich: %v", qsch)
	}
	if msch := byName["mode"]["schema"].(map[string]any); msch["enum"] == nil {
		t.Fatalf("mode schema missing enum: %v", msch)
	}
	if lsch := byName["limit"]["schema"].(map[string]any); lsch["default"] == nil {
		t.Fatalf("limit schema missing default: %v", lsch)
	}
}

// 无 Method 的命令默认同时路由 GET 与 POST，openapi 也产出两个操作。
func TestHTTPDefaultGETAndPOST(t *testing.T) {
	reg := registry.New()
	type tArgs struct {
		Name string `json:"name"`
	}
	if _, err := spec.Define("thing.do", func(_ context.Context, in *tArgs) (string, error) {
		return "hi " + in.Name, nil
	}).HTTP(spec.HTTPHints{Path: "/thing"}).Register(reg); err != nil { // 无 Method
		t.Fatal(err)
	}
	h := mustHandler(t, reg)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/thing?name=alice", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "hi alice") {
		t.Fatalf("GET: code=%d body=%s", rec.Code, rec.Body.String())
	}
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest("POST", "/thing", strings.NewReader(`{"name":"bob"}`)))
	if rec2.Code != http.StatusOK || !strings.Contains(rec2.Body.String(), "hi bob") {
		t.Fatalf("POST: code=%d body=%s", rec2.Code, rec2.Body.String())
	}

	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, httptest.NewRequest("GET", "/openapi.json", nil))
	var doc map[string]any
	if err := json.Unmarshal(rec3.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	thing := doc["paths"].(map[string]any)["/thing"].(map[string]any)
	if _, ok := thing["get"]; !ok {
		t.Fatalf("openapi missing get operation: %v", thing)
	}
	post, ok := thing["post"].(map[string]any)
	if !ok {
		t.Fatalf("openapi missing post operation: %v", thing)
	}
	if post["requestBody"] == nil {
		t.Fatalf("post operation should carry a requestBody")
	}
}

// 逗号分隔的多方法列表。
func TestHTTPMethodList(t *testing.T) {
	reg := registry.New()
	type tArgs struct {
		Name string `json:"name"`
	}
	if _, err := spec.Define("thing.put", func(_ context.Context, in *tArgs) (string, error) {
		return in.Name, nil
	}).HTTP(spec.HTTPHints{Method: "PUT,PATCH", Path: "/thing2"}).Register(reg); err != nil {
		t.Fatal(err)
	}
	h := mustHandler(t, reg)
	for _, m := range []string{"PUT", "PATCH"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(m, "/thing2", strings.NewReader(`{"name":"x"}`)))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: code=%d", m, rec.Code)
		}
	}
	// GET 未注册 → 405
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/thing2", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET on PUT,PATCH route = %d, want 405", rec.Code)
	}
}
