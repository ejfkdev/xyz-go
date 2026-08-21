package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	errs "github.com/ejfkdev/xyz-go/errors"
	"github.com/ejfkdev/xyz-go/registry"
	"github.com/ejfkdev/xyz-go/spec"
)

type sumArgs struct {
	A int `json:"a" desc:"左操作数"`
	B int `json:"b" desc:"右操作数" default:"10"`
}

func sumHandler(_ context.Context, in *sumArgs) (int, error) {
	if in.A == 404 {
		return 0, errs.New(errs.KindNotFound, "no such thing")
	}
	return in.A + in.B, nil
}

func buildReg(t *testing.T) *registry.Registry {
	t.Helper()
	reg := registry.New()
	if _, err := spec.Define("math.sum", sumHandler).
		Summary("两数求和").
		MCP(spec.MCPHints{Annotations: []string{"read", "idempotent"}}).
		Register(reg); err != nil {
		t.Fatal(err)
	}
	type echoArgs struct {
		Msg string `json:"msg" desc:"回显内容" required:"true"`
	}
	if _, err := spec.Define("echo.say", func(_ context.Context, in *echoArgs) (string, error) {
		return in.Msg, nil
	}).Register(reg); err != nil {
		t.Fatal(err)
	}
	return reg
}

func connectPair(t *testing.T, reg *registry.Registry, opts Options) (*sdkmcp.ClientSession, *sdkmcp.ServerSession) {
	t.Helper()
	server, err := Server(reg, opts)
	if err != nil {
		t.Fatalf("Server: %v", err)
	}
	t1, t2 := sdkmcp.NewInMemoryTransports()
	ss, err := server.Connect(context.Background(), t1, nil)
	if err != nil {
		t.Fatalf("server Connect: %v", err)
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "tester", Version: "0"}, nil)
	cs, err := client.Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatalf("client Connect: %v", err)
	}
	t.Cleanup(func() {
		cs.Close()
		ss.Close()
	})
	return cs, ss
}

func TestToolsListed(t *testing.T) {
	cs, _ := connectPair(t, buildReg(t), Options{})
	var names []string
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatalf("Tools: %v", err)
		}
		names = append(names, tool.Name)
	}
	if len(names) != 2 || names[0] != "echo.say" || names[1] != "math.sum" {
		t.Fatalf("tools = %v, want [echo.say math.sum]", names)
	}
}

func TestCallToolSuccess(t *testing.T) {
	cs, _ := connectPair(t, buildReg(t), Options{})
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "math.sum",
		Arguments: map[string]any{"a": float64(5)},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected isError result: %+v", res)
	}
	// 全局默认 b=10 由 Invoke 补齐；结构化内容是裸值（无 {data:...} 信封）
	if got, ok := res.StructuredContent.(float64); !ok || got != 15 {
		t.Fatalf("structured content = %#v, want bare 15", res.StructuredContent)
	}
	text := res.Content[0].(*sdkmcp.TextContent).Text
	if !strings.Contains(text, "15") {
		t.Fatalf("text content = %q, want sum 15", text)
	}
}

func TestCallToolStructuredPrimitive(t *testing.T) {
	cs, _ := connectPair(t, buildReg(t), Options{})
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "echo.say",
		Arguments: map[string]any{"msg": "你好, mcp"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	text := res.Content[0].(*sdkmcp.TextContent).Text
	if !strings.Contains(text, "你好, mcp") {
		t.Fatalf("text = %q", text)
	}
}

func TestCallToolErrorIsErrorResult(t *testing.T) {
	cs, _ := connectPair(t, buildReg(t), Options{})
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "math.sum",
		Arguments: map[string]any{"a": float64(404)},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("want isError=true result for classified handler error")
	}
	text := res.Content[0].(*sdkmcp.TextContent).Text
	if !strings.Contains(text, "no such thing") {
		t.Fatalf("text = %q, want cause message", text)
	}
}

func TestMCPDefaultsInjected(t *testing.T) {
	reg := registry.New()
	if _, err := spec.Define("def.cmd", sumHandler).
		MCP(spec.MCPHints{Fields: map[string]spec.MCPFieldHint{
			"a": {Default: 7},
		}}).
		Register(reg); err != nil {
		t.Fatal(err)
	}
	cs, _ := connectPair(t, reg, Options{})
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "def.cmd",
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	// MCP 专属默认 a=7 注入，b 走全局默认 10 → 17
	text := res.Content[0].(*sdkmcp.TextContent).Text
	if !strings.Contains(text, "17") {
		t.Fatalf("text = %q, want 17 (MCP default 7 + global 10)", text)
	}
}

// 客户端显式传入的入参不能被 MCP 专属默认值覆盖（默认值只补缺，不清赶显式值）。
func TestMCPDefaultsDoNotOverrideExplicit(t *testing.T) {
	reg := registry.New()
	if _, err := spec.Define("def.cmd", sumHandler).
		MCP(spec.MCPHints{Fields: map[string]spec.MCPFieldHint{
			"a": {Default: 7},
		}}).
		Register(reg); err != nil {
		t.Fatal(err)
	}
	cs, _ := connectPair(t, reg, Options{})
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "def.cmd",
		Arguments: map[string]any{"a": float64(100)},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected isError result: %+v", res)
	}
	// 显式 a=100 应保留，b 走全局默认 10 → 110（而非被默认 7 覆盖成 17）。
	if got, ok := res.StructuredContent.(float64); !ok || got != 110 {
		t.Fatalf("structured content = %#v, want 110 (100 + global default 10)", res.StructuredContent)
	}
}

func TestVersionFilterTransport(t *testing.T) {
	allowed := versionSet([]string{ProtocolV2025_06_18})
	tr := versionFilterTransport{Transport: &sdkmcp.StdioTransport{}, allowed: allowed}
	if !tr.SupportsProtocolVersion(ProtocolV2025_06_18) {
		t.Fatal("should support the configured version")
	}
	if tr.SupportsProtocolVersion(ProtocolV2026_07_28) {
		t.Fatal("should reject versions outside the subset")
	}
}

func TestVersionGate(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	allowed := versionSet([]string{ProtocolV2025_06_18})
	gate := versionGate(ok, []string{ProtocolV2025_06_18}, allowed)

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("MCP-Protocol-Version", ProtocolV2026_07_28)
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for version outside subset", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "supported") {
		t.Fatalf("body = %q, want supported-versions listing", rec.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req2.Header.Set("MCP-Protocol-Version", ProtocolV2025_06_18)
	rec2 := httptest.NewRecorder()
	gate.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for allowed version", rec2.Code)
	}
}

func TestValidateVersions(t *testing.T) {
	if err := validateVersions(nil); err != nil {
		t.Fatalf("nil versions should be valid, got %v", err)
	}
	if err := validateVersions([]string{ProtocolV2025_06_18, ProtocolV2026_07_28}); err != nil {
		t.Fatalf("known versions rejected: %v", err)
	}
	if err := validateVersions([]string{"2020-01-01"}); err == nil {
		t.Fatal("unknown version accepted")
	}
	if err := validateVersions([]string{""}); err == nil {
		t.Fatal("empty version accepted")
	}
}

func TestValidateTransportVersions(t *testing.T) {
	// SSE 传输在现代协议中被移除，2026-07-28 无法在 SSE 上服务。
	if err := validateTransportVersions("sse", Options{Versions: []string{ProtocolV2026_07_28}}); err == nil {
		t.Fatal("sse + 2026-07-28 should be rejected")
	}
	if err := validateTransportVersions("sse", Options{Versions: []string{ProtocolV2025_06_18}}); err != nil {
		t.Fatalf("sse + legacy version should pass: %v", err)
	}
	// streamable HTTP 服务 2026-07-28 需要无状态模式。
	if err := validateTransportVersions("http", Options{Versions: []string{ProtocolV2026_07_28}}); err == nil {
		t.Fatal("http + 2026-07-28 without stateless should be rejected")
	}
	if err := validateTransportVersions("http", Options{Versions: []string{ProtocolV2026_07_28}, Stateless: true}); err != nil {
		t.Fatalf("http stateless + 2026-07-28 should pass: %v", err)
	}
	if err := validateTransportVersions("stdio", Options{Versions: []string{ProtocolV2024_11_05}}); err != nil {
		t.Fatalf("stdio serves every version: %v", err)
	}
	// 混合列表：只要有一个版本该传输能服务即可（SDK 会裁剪交集）。
	if err := validateTransportVersions("sse", Options{Versions: []string{ProtocolV2025_06_18, ProtocolV2026_07_28}}); err != nil {
		t.Fatalf("mixed list should pass: %v", err)
	}
}

func TestParseArgs(t *testing.T) {
	transport, opts, err := parseArgs([]string{"sse", "--addr", ":9090", "--versions", "2025-06-18,2026-07-28", "--name", "svc"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if transport != "sse" || opts.Addr != ":9090" || opts.Name != "svc" {
		t.Fatalf("got %q %+v", transport, opts)
	}
	if len(opts.Versions) != 2 || opts.Versions[0] != ProtocolV2025_06_18 {
		t.Fatalf("versions = %v", opts.Versions)
	}
	if _, _, err := parseArgs(nil); err == nil {
		t.Fatal("missing transport accepted")
	}
	if _, _, err := parseArgs([]string{"stdio", "--nope"}); err == nil {
		t.Fatal("unknown flag accepted")
	}
	// 裸名 --bearer：模式词即命名空间，不需要 --xyz. 前缀。
	_, opts2, err := parseArgs([]string{"http", "--bearer", "a,b", "--bearer=c"})
	if err != nil {
		t.Fatalf("--bearer parse: %v", err)
	}
	if len(opts2.BearerTokens) != 3 {
		t.Fatalf("bearer tokens = %v", opts2.BearerTokens)
	}
	// 会话超时与 CORS（裸名，session-timeout 非法值报错）。
	_, opts3, err := parseArgs([]string{"http", "--session-timeout", "30s", "--cors=*"})
	if err != nil {
		t.Fatalf("session/cors parse: %v", err)
	}
	if opts3.SessionTimeout != 30*time.Second || len(opts3.CORSOrigins) != 1 {
		t.Fatalf("opts3 = %+v", opts3)
	}
	if _, _, err := parseArgs([]string{"http", "--session-timeout", "nope"}); err == nil {
		t.Fatal("bad session timeout accepted")
	}
}

func TestToolOutputSchema(t *testing.T) {
	cs, _ := connectPair(t, buildReg(t), Options{})
	var got *sdkmcp.Tool
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatalf("Tools: %v", err)
		}
		if tool.Name == "math.sum" {
			got = tool
		}
	}
	if got == nil || got.OutputSchema == nil {
		t.Fatalf("math.sum tool should carry output schema, got %+v", got)
	}
}
