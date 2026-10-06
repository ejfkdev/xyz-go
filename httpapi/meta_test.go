package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	errs "github.com/ejfkdev/xyz-go/errors"
	"github.com/ejfkdev/xyz-go/registry"
	"github.com/ejfkdev/xyz-go/spec"
)

// 富化错误体 + X-XYZ-* 服务器上下文头（xyz-spec §8.3 / §11.4）。

type richErrArgs struct {
	ID string `json:"id" http:"path"`
}

func richErrHandler(_ context.Context, in *richErrArgs) (string, error) {
	return "", errs.NotFound("thing %s missing", in.ID).
		WithCode("THING_MISSING").
		WithDetail("id", in.ID).
		WithDetail("scope", "global")
}

func buildRichErrReg(t *testing.T) *registry.Registry {
	t.Helper()
	reg := registry.New()
	if _, err := spec.Define("thing.get", richErrHandler).
		HTTP(spec.HTTPHints{Method: "GET", Path: "/things/{id}"}).
		Register(reg); err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestHTTPEnrichedErrorBody(t *testing.T) {
	h := mustHandler(t, buildRichErrReg(t))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/things/42", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body=%s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body not JSON: %v (%s)", err, rec.Body.String())
	}
	if body["error"] != "thing 42 missing" {
		t.Fatalf("error message = %v", body["error"])
	}
	if body["kind"] != "not_found" {
		t.Fatalf("kind = %v", body["kind"])
	}
	if body["code"] != "THING_MISSING" {
		t.Fatalf("code = %v", body["code"])
	}
	detail, _ := body["detail"].(map[string]any)
	if detail["id"] != "42" || detail["scope"] != "global" {
		t.Fatalf("detail = %v", body["detail"])
	}
}

func TestHTTPStatusOverride(t *testing.T) {
	reg := registry.New()
	if _, err := spec.Define("gone.get", func(_ context.Context, in *richErrArgs) (string, error) {
		return "", errs.NotFound("gone").WithStatus(http.StatusGone)
	}).HTTP(spec.HTTPHints{Method: "GET", Path: "/gone/{id}"}).Register(reg); err != nil {
		t.Fatal(err)
	}
	h := mustHandler(t, reg)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/gone/x", nil))
	if rec.Code != http.StatusGone {
		t.Fatalf("status = %d, want 410 (Status override)", rec.Code)
	}
}

func TestHTTPErrorBodyBackwardCompatible(t *testing.T) {
	// 简单错误（无 code/detail）的错误体仍含扁平 error 字符串键，与 v0.5 前一致。
	h := mustHandler(t, buildHTTPReg(t))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/users/missing", strings.NewReader(`{}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "no such user" {
		t.Fatalf("flat error key = %v", body["error"])
	}
	if _, has := body["code"]; has {
		t.Fatalf("simple error must not carry a code key: %v", body)
	}
}

func TestHTTPServerContextHeaders(t *testing.T) {
	meta := ResponseMeta{
		AppName:    "demoapp",
		AppVersion: "v9.9.9",
		SDKVersion: "0.4.2",
		Headers:    map[string]string{"X-App": "demo"},
	}
	h, err := HandlerWithMeta(buildHTTPReg(t), nil, meta)
	if err != nil {
		t.Fatal(err)
	}
	h = ServerHeaders(meta, h)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/users/alice", strings.NewReader(`{"age":9}`)))
	if rec.Code != 200 {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-App-Name"); got != "demoapp" {
		t.Fatalf("X-App-Name = %q", got)
	}
	if got := rec.Header().Get("X-App-Version"); got != "v9.9.9" {
		t.Fatalf("X-App-Version = %q", got)
	}
	if got := rec.Header().Get("X-XYZ-Version"); got != "0.4.2" {
		t.Fatalf("X-XYZ-Version (SDK) = %q", got)
	}
	if got := rec.Header().Get("X-XYZ-Command"); got != "user.add" {
		t.Fatalf("X-XYZ-Command = %q", got)
	}
	if got := rec.Header().Get("X-XYZ-Duration-Ms"); got == "" {
		t.Fatalf("X-XYZ-Duration-Ms missing")
	}
	if got := rec.Header().Get("X-App"); got != "demo" {
		t.Fatalf("custom header X-App = %q", got)
	}
}

func TestHTTPNoAutoHeadersKeepsCustom(t *testing.T) {
	meta := ResponseMeta{
		AppName:       "demoapp",
		AppVersion:    "v1",
		SDKVersion:    "0.4.2",
		NoAutoHeaders: true,
		Headers:       map[string]string{"X-App": "demo"},
	}
	h, err := HandlerWithMeta(buildHTTPReg(t), nil, meta)
	if err != nil {
		t.Fatal(err)
	}
	h = ServerHeaders(meta, h)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/users/alice", strings.NewReader(`{}`)))
	for _, hdr := range []string{"X-App-Name", "X-App-Version", "X-XYZ-Version", "X-XYZ-Command"} {
		if got := rec.Header().Get(hdr); got != "" {
			t.Fatalf("auto header %s should be suppressed, got %q", hdr, got)
		}
	}
	if got := rec.Header().Get("X-App"); got != "demo" {
		t.Fatalf("custom header must survive NoAutoHeaders, got %q", got)
	}
}
