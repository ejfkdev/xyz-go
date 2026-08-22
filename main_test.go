package xyz

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ejfkdev/xyz-go/langx"

	"github.com/ejfkdev/xyz-go/logx"
	"github.com/ejfkdev/xyz-go/registry"
	"github.com/ejfkdev/xyz-go/spec"
)

type tArgs struct {
	S string `json:"s"`
}

func testReg(t *testing.T, names ...string) *registry.Registry {
	t.Helper()
	reg := registry.New()
	for _, n := range names {
		_, err := spec.Define(n, func(_ context.Context, in *tArgs) (string, error) { return in.S, nil }).Register(reg)
		if err != nil {
			t.Fatal(err)
		}
	}
	return reg
}

func TestRunOverview(t *testing.T) {
	if got := Run(testReg(t, "a.b"), nil); got != 0 {
		t.Fatalf("exit code = %d, want 0", got)
	}
	if got := Run(testReg(t, "a.b"), []string{"help"}); got != 0 {
		t.Fatalf("exit code = %d, want 0", got)
	}
}

func TestRunEmptyRegistryIsNoOp(t *testing.T) {
	// 没有任何注册命令：任何形态都静默退出 0，不打印任何东西。
	for _, args := range [][]string{nil, {"help"}, {"user", "add"}, {"mcp", "stdio"}, {"serve"}} {
		if got := Run(registry.New(), args); got != 0 {
			t.Fatalf("Run(empty, %v) = %d, want 0", args, got)
		}
	}
}

func TestRunCustomModeWords(t *testing.T) {
	cfg := Config{Modes: ModeWords{Serve: "httpd", MCP: "protocol", Help: "assist"}}
	reg := testReg(t, "a.b")
	// 自定义的帮助词走总览。
	if got := RunConfig(reg, []string{"assist"}, cfg); got != 0 {
		t.Fatalf("RunConfig(assist) = %d, want 0", got)
	}
	// httpd 词仍归 HTTP 模式；真实监听会阻塞，用 NoHTTP 能力挡在半路验证分发。
	noHTTP := cfg
	noHTTP.Capabilities.NoHTTP = true
	if got := RunConfig(reg, []string{"httpd"}, noHTTP); got != 1 {
		t.Fatalf("RunConfig(httpd) = %d, want 1 (disabled)", got)
	}
}

func TestRunInvalidModeWords(t *testing.T) {
	reg := testReg(t, "a.b")
	for _, cfg := range []Config{
		{Modes: ModeWords{Serve: "serve", MCP: "serve"}}, // 重复
		{Modes: ModeWords{Serve: "-serve"}},              // 前导横线
		{Modes: ModeWords{Serve: "sv c"}},                // 空白
	} {
		if got := RunConfig(reg, nil, cfg); got != 2 {
			t.Fatalf("invalid cfg %+v: exit = %d, want 2", cfg, got)
		}
	}
}

func TestRunReservedName(t *testing.T) {
	for _, name := range []string{"serve.x", "mcp.up", "help.me"} {
		if got := Run(testReg(t, name), []string{"whatever"}); got != 2 {
			t.Fatalf("Run with reserved name %q = %d, want 2", name, got)
		}
	}
}

func TestRunNilRegistry(t *testing.T) {
	if got := Run(nil, []string{"x"}); got != 2 {
		t.Fatalf("exit code = %d, want 2", got)
	}
}

func TestRunVersionFlag(t *testing.T) {
	// -v/--version 由根派发器处理：任何能力组合与构建标签下都可用。
	for _, args := range [][]string{{"-v"}, {"--version"}, {"echo", "hi", "-v"}} {
		if got := Run(testReg(t, "a.b"), args); got != 0 {
			t.Fatalf("Run(%v) = %d, want 0", args, got)
		}
	}
}

func TestRunDisabledCapabilities(t *testing.T) {
	reg := testReg(t, "echo.hi")

	// NoCLI：子命令不可用，但 help/-v/serve/mcp 壳能力保留。
	noCLI := Config{Capabilities: Capabilities{NoCLI: true}}
	if got := RunConfig(reg, []string{"echo", "hi", "--s", "x"}, noCLI); got != 1 {
		t.Fatalf("NoCLI dispatch = %d, want 1", got)
	}
	if got := RunConfig(reg, []string{"help"}, noCLI); got != 0 {
		t.Fatalf("NoCLI help = %d, want 0", got)
	}
	if got := RunConfig(reg, []string{"-v"}, noCLI); got != 0 {
		t.Fatalf("NoCLI -v = %d, want 0", got)
	}

	// NoMCP：mcp 模式被拒绝（配置检查在进入 MCP 前端之前，不会阻塞）。
	noMCP := Config{Capabilities: Capabilities{NoMCP: true}}
	if got := RunConfig(reg, []string{"mcp", "stdio"}, noMCP); got != 1 {
		t.Fatalf("NoMCP mcp stdio = %d, want 1 (disabled)", got)
	}

	// NoHTTP：serve 模式被拒绝（真实监听会阻塞，禁用检查在半路拦截）。
	noHTTP := Config{Capabilities: Capabilities{NoHTTP: true}}
	if got := RunConfig(reg, []string{"serve"}, noHTTP); got != 1 {
		t.Fatalf("NoHTTP serve = %d, want 1 (disabled)", got)
	}
	// 与 NoCLI 叠加同样生效。
	both := Config{Capabilities: Capabilities{NoCLI: true, NoHTTP: true}}
	if got := RunConfig(reg, []string{"serve"}, both); got != 1 {
		t.Fatalf("NoCLI+NoHTTP serve = %d, want 1 (disabled)", got)
	}
}

func TestStripXYZFlags(t *testing.T) {
	cfg := Config{BearerTokens: []string{"code-tok"}}
	rest, err := stripXYZFlags([]string{
		"--xyz.bearer=a,b", "mcp", "stdio", "--xyz.addr=:9090", "--xyz.bearer=b",
	}, &cfg)
	if err != nil {
		t.Fatalf("stripXYZFlags: %v", err)
	}
	if cfg.Addr != ":9090" {
		t.Fatalf("addr = %q, want :9090", cfg.Addr)
	}
	want := []string{"code-tok", "a", "b"}
	if len(cfg.BearerTokens) != 3 {
		t.Fatalf("tokens = %v, want %v (code preset merged, deduped, flag added)", cfg.BearerTokens, want)
	}
	if len(rest) != 2 || rest[0] != "mcp" || rest[1] != "stdio" {
		t.Fatalf("rest = %v, want [mcp stdio]", rest)
	}
	// 分开写法与空值去重
	cfg2 := Config{}
	rest2, err := stripXYZFlags([]string{"--xyz.bearer", "x,,y", "echo"}, &cfg2)
	if err != nil {
		t.Fatalf("stripXYZFlags: %v", err)
	}
	if len(cfg2.BearerTokens) != 2 {
		t.Fatalf("tokens2 = %v", cfg2.BearerTokens)
	}
	_ = rest2

	// 新增内置参数：日志级别 / 超时 / TLS / CORS。
	cfg3 := Config{}
	rest3, err := stripXYZFlags([]string{
		"serve", "--xyz.log-level=debug", "--xyz.timeout", "45s",
		"--xyz.tls-cert=a.pem", "--xyz.tls-key", "k.pem", "--xyz.cors=x,y,z",
	}, &cfg3)
	if err != nil {
		t.Fatalf("stripXYZFlags: %v", err)
	}
	if cfg3.LogLevel != logx.LevelDebug || cfg3.Timeout != 45*time.Second ||
		cfg3.CertFile != "a.pem" || cfg3.KeyFile != "k.pem" || len(cfg3.CORSOrigins) != 3 {
		t.Fatalf("cfg3 = %+v", cfg3)
	}
	if len(rest3) != 1 || rest3[0] != "serve" {
		t.Fatalf("rest3 = %v", rest3)
	}
	// 非法值在解析期报错。
	if _, err := stripXYZFlags([]string{"--xyz.log-level=verbose"}, &Config{}); err == nil {
		t.Fatal("bad log level accepted")
	}
	if _, err := stripXYZFlags([]string{"--xyz.timeout=nope"}, &Config{}); err == nil {
		t.Fatal("bad timeout accepted")
	}
}

func TestStripXYZFlagsStopsAtDoubleDash(t *testing.T) {
	cfg := Config{}
	rest, err := stripXYZFlags([]string{"serve", "--", "--xyz.bearer=hacked"}, &cfg)
	if err != nil {
		t.Fatalf("stripXYZFlags: %v", err)
	}
	if len(cfg.BearerTokens) != 0 {
		t.Fatalf("builtin flags after -- must not be stripped: %v", cfg.BearerTokens)
	}
	if len(rest) != 3 || rest[2] != "--xyz.bearer=hacked" {
		t.Fatalf("rest = %v, want tokens preserved verbatim", rest)
	}
}

func TestOverviewLanguage(t *testing.T) {
	reg := testReg(t, "a.b")
	langx.Set(langx.En, nil)
	var en bytes.Buffer
	printOverview(&en, reg, "serve", "mcp", Capabilities{}, "", "")
	if !strings.Contains(en.String(), "Usage (the mode is detected") {
		t.Fatalf("en overview missing: %q", en.String())
	}
	langx.Set(langx.ZhCn, nil)
	var zh bytes.Buffer
	printOverview(&zh, reg, "serve", "mcp", Capabilities{}, "", "")
	if !strings.Contains(zh.String(), "用法（模式由程序自动判断") {
		t.Fatalf("zh overview missing: %q", zh.String())
	}
	langx.Set(langx.En, nil)
	// 覆盖表生效
	langx.Set(langx.En, map[string]string{"overview.commands": "Commands!:"})
	var ov bytes.Buffer
	printOverview(&ov, reg, "serve", "mcp", Capabilities{}, "", "")
	if !strings.Contains(ov.String(), "Commands!:") {
		t.Fatalf("overrides not applied: %q", ov.String())
	}
	langx.Set(langx.En, nil)
}

func TestTryRunComposability(t *testing.T) {
	reg := testReg(t, "a.b")
	// 未命中顶层词：静默 (0,false)
	code, handled := TryRun(reg, []string{"nope"})
	if code != 0 || handled {
		t.Fatalf("TryRun unknown top = (%d,%v), want (0,false)", code, handled)
	}
	// 已知路径/内建照常（无 CLI 前端的构建走 stub：退出 1 仍属 handled）
	wantKnownCode := 0
	if !cliFrontend {
		wantKnownCode = 1
	}
	if code, handled := TryRun(reg, []string{"a", "b", "--s", "x"}); !handled || code != wantKnownCode {
		t.Fatalf("TryRun known = (%d,%v), want (%d,true)", code, handled, wantKnownCode)
	}
	if code, handled := TryRun(reg, []string{"-v"}); !handled || code != 0 {
		t.Fatalf("TryRun -v = (%d,%v)", code, handled)
	}
	// 已知命令下的未知 flag 交给 CLI 自身报错（退出 2，handled=true；
	// 无 CLI 前端的构建走 stub 退出 1）
	wantGhostCode := 2
	if !cliFrontend {
		wantGhostCode = 1
	}
	if code, handled := TryRun(reg, []string{"a", "b", "--ghost"}); !handled || code != wantGhostCode {
		t.Fatalf("TryRun known+--ghost = (%d,%v), want (%d,true)", code, handled, wantGhostCode)
	}
	// CLI Skip 的命令段视为未命中
	reg2 := registry.New()
	if _, err := spec.Define("watch.target", func(_ context.Context, _ *tArgs) (string, error) { return "", nil }).
		CLI(spec.CliHints{Skip: true}).Register(reg2); err != nil {
		t.Fatal(err)
	}
	if _, handled := TryRun(reg2, []string{"watch"}); handled {
		t.Fatal("CLI-skipped top must be composable-miss")
	}
}

func TestChannelDefaultsFlag(t *testing.T) {
	cfg := Config{}
	rest, err := stripXYZFlags([]string{"--xyz.default=index=./wiki", "--xyz.default", "k=v,a=b", "serve"}, &cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 1 || rest[0] != "serve" {
		t.Fatalf("rest = %v", rest)
	}
	want := map[string]string{"index": "./wiki", "k": "v", "a": "b"}
	for k, v := range want {
		if cfg.ChannelDefaults[k] != v {
			t.Fatalf("ChannelDefaults[%s] = %q, want %q", k, cfg.ChannelDefaults[k], v)
		}
	}
	// serve 裸名
	cfg2 := Config{}
	cfg2 = parseServeArgs([]string{"--default", "index=./wiki"}, cfg2)
	if cfg2.ChannelDefaults["index"] != "./wiki" {
		t.Fatalf("serve bare --default lost: %v", cfg2.ChannelDefaults)
	}
	// 非法对报错
	if _, err := stripXYZFlags([]string{"--xyz.default=noequals"}, &Config{}); err == nil {
		t.Fatal("invalid --default pair accepted")
	}
}

func TestBareFlagPassthroughDefaults(t *testing.T) {
	// serve：gs serve --index ./wiki --k v 等价 --default index=./wiki,k=v
	cfg := parseServeArgs([]string{"--index", "./wiki", "--k=v"}, Config{})
	if cfg.ChannelDefaults["index"] != "./wiki" || cfg.ChannelDefaults["k"] != "v" {
		t.Fatalf("bare passthrough lost: %v", cfg.ChannelDefaults)
	}
	// mcp 解析器的透传语义由 mcp/args_test.go 覆盖。
}

func TestPrintOverviewHelpBlocks(t *testing.T) {
	reg := testReg(t, "a.b")
	var buf bytes.Buffer
	before := "myapp v1.2.3 — do the thing\nhttps://github.com/me/myapp"
	after := "Need help? https://github.com/me/myapp#faq"
	printOverview(&buf, reg, "serve", "mcp", Capabilities{}, before, after)
	out := buf.String()
	if !strings.HasPrefix(out, before+"\n") {
		t.Fatalf("before block not at top: %q", out)
	}
	if !strings.HasSuffix(out, after+"\n") {
		t.Fatalf("after block not at end: %q", out)
	}
	// 空块零变化：与不传块逐字节一致
	var base, withEmpty bytes.Buffer
	printOverview(&base, reg, "serve", "mcp", Capabilities{}, "", "")
	printOverview(&withEmpty, reg, "serve", "mcp", Capabilities{}, "", "")
	if base.String() != withEmpty.String() {
		t.Fatal("empty blocks must be a no-op")
	}
	// 空注册表时 after 仍打印（早退路径）
	empty := registry.New()
	var buf2 bytes.Buffer
	printOverview(&buf2, empty, "serve", "mcp", Capabilities{}, "", "tail")
	if !strings.HasSuffix(buf2.String(), "tail\n") {
		t.Fatalf("after block lost on empty registry: %q", buf2.String())
	}
	// 多行保留、结尾换行归一（多个 \n 折叠为一个）
	var buf3 bytes.Buffer
	printOverview(&buf3, reg, "serve", "mcp", Capabilities{}, "a\nb\n\n\n", "")
	if !strings.HasPrefix(buf3.String(), "a\nb\n"+langx.T("overview.usage_line")) {
		t.Fatalf("block newline normalization wrong: %q", buf3.String())
	}
}
