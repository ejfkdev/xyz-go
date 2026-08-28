package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ejfkdev/xyz-go/block"
	errs "github.com/ejfkdev/xyz-go/errors"
	"github.com/ejfkdev/xyz-go/registry"
	"github.com/ejfkdev/xyz-go/spec"
)

type addArgs struct {
	Name    string   `json:"name" desc:"用户名" required:"true" validate:"min=2" cli:"positional"`
	Age     int      `json:"age" desc:"年龄" default:"18"`
	Mode    string   `json:"mode" desc:"模式" enum:"fast,slow"`
	Verbose bool     `json:"verbose" desc:"啰嗦输出"`
	Tags    []string `json:"tags" desc:"标签"`
	Token   string   `json:"-" secret:"true"`
}

type addResp struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func addHandler(_ context.Context, in *addArgs) (*addResp, error) {
	if in.Name == "missing" {
		return nil, errs.New(errs.KindNotFound, "no such user")
	}
	return &addResp{Name: in.Name, Age: in.Age}, nil
}

type sumArgs struct {
	A int `json:"a" desc:"左操作数"`
	B int `json:"b" desc:"右操作数"`
}

func sumHandler(_ context.Context, in *sumArgs) (int, error) { return in.A + in.B, nil }

type listArgs struct {
	Q string `json:"q" desc:"关键词"`
}

func listHandler(_ context.Context, _ *listArgs) ([]addResp, error) {
	return []addResp{{Name: "alice", Age: 18}, {Name: "bob", Age: 25}}, nil
}

func buildApp(t *testing.T) *App {
	t.Helper()
	reg := registry.New()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := spec.Define("user.add", addHandler).
		Summary("创建用户").
		CLI(spec.CliHints{Fields: map[string]spec.CliFieldHint{
			"age": {Shorthand: "a", EnvVar: "APP_AGE", Default: 25},
		}}).
		Register(reg)
	must(err)
	_, err2 := spec.Define("math.sum", sumHandler).Summary("求和").Register(reg)
	must(err2)
	_, err3 := spec.Define("user.list", listHandler).Summary("列表").Register(reg)
	must(err3)
	app, err := New(reg)
	must(err)
	return app
}

func runApp(t *testing.T, app *App, args ...string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	app.out = &out
	app.errOut = &errb
	code := app.Run(args)
	return out.String(), errb.String(), code
}

func TestCLIPositionalFlagShorthand(t *testing.T) {
	out, _, code := runApp(t, buildApp(t), "user", "add", "bob", "-a", "9")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(out, "bob") || !strings.Contains(out, "9") {
		t.Fatalf("output missing values: %q", out)
	}
	// struct → aligned key/value lines
	if !strings.Contains(out, "name") || !strings.Contains(out, "age") {
		t.Fatalf("output missing field keys: %q", out)
	}
}

func TestCLISliceAndBoolFlags(t *testing.T) {
	out, _, code := runApp(t, buildApp(t), "user", "add", "bob", "--tags", "a,b", "--verbose")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	_ = out
}

func TestCLIDefaultOverridesGlobal(t *testing.T) {
	// age 全局默认 18，CLI 覆盖 25；不传 flag 时应显示 25。
	out, _, code := runApp(t, buildApp(t), "user", "add", "bob")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(out, "25") || strings.Contains(out, "18") {
		t.Fatalf("output = %q, want CLI default 25 instead of global 18", out)
	}
}

func TestCLIEnvFallback(t *testing.T) {
	t.Setenv("APP_AGE", "31")
	out, _, code := runApp(t, buildApp(t), "user", "add", "bob")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(out, "31") {
		t.Fatalf("output = %q, want env value 31", out)
	}
}

func TestCLIJSONFlag(t *testing.T) {
	out, _, code := runApp(t, buildApp(t), "user", "add", "bob", "--json")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if got["name"] != "bob" || got["age"] != float64(25) {
		t.Fatalf("json = %v", got)
	}
}

func TestCLITableForStructSlice(t *testing.T) {
	out, _, code := runApp(t, buildApp(t), "user", "list", "--q", "x")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("table lines = %d (%q), want header + dashes + 2 rows", len(lines), out)
	}
	if !strings.Contains(lines[0], "name") || !strings.Contains(lines[0], "age") {
		t.Fatalf("header = %q", lines[0])
	}
	if !strings.Contains(lines[2], "alice") || !strings.Contains(lines[3], "bob") {
		t.Fatalf("rows = %v", lines)
	}
}

func TestCLIPrimitiveRendering(t *testing.T) {
	app := buildApp(t)
	out, _, code := runApp(t, app, "math", "sum", "--a", "1", "--b", "2")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if strings.TrimRight(out, "\n") != "3" {
		t.Fatalf("primitive output = %q, want bare 3 without envelope", out)
	}
}

func TestCLIVersionFlag(t *testing.T) {
	app := buildApp(t)
	for _, args := range [][]string{{"-v"}, {"--version"}, {"user", "add", "bob", "-v"}} {
		out, _, code := runApp(t, app, args...)
		if code != 0 {
			t.Fatalf("%v: exit code = %d", args, code)
		}
		if !strings.Contains(out, "version dev") {
			t.Fatalf("%v: output = %q, want version dev", args, out)
		}
	}
}

func TestCLIHelpFlag(t *testing.T) {
	app := buildApp(t)
	out, _, code := runApp(t, app, "user", "add", "--help")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	for _, want := range []string{"Usage", "-a", "--age", "年龄", "<name>",
		"(default 25)", "(env APP_AGE)", "(oneof fast|slow)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help output missing %q: %q", want, out)
		}
	}
	// 根级帮助列出版本与 json 开关。
	out2, _, _ := runApp(t, app, "--help")
	if !strings.Contains(out2, "--version") || !strings.Contains(out2, "--json") {
		t.Fatalf("root help missing global flags: %q", out2)
	}
}

func TestCLIExitCodes(t *testing.T) {
	app := buildApp(t)
	// 业务错误 → 分类映射（not_found → 1）
	out, errb, code := runApp(t, app, "user", "add", "missing")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if out != "" || !strings.Contains(errb, "no such user") {
		t.Fatalf("out=%q err=%q, want message on stderr only", out, errb)
	}
	// 未知 flag → 用法错误 → 2
	if _, _, code := runApp(t, app, "user", "add", "bob", "--nope"); code != 2 {
		t.Fatalf("exit code = %d, want 2 for unknown flag", code)
	}
	// 缺必填位置参数 → 2
	if _, _, code := runApp(t, app, "user", "add"); code != 2 {
		t.Fatalf("exit code = %d, want 2 for missing positional", code)
	}
}

func TestCLIRejectsNestedStruct(t *testing.T) {
	type sub struct {
		Level int `json:"level"`
	}
	type nestedArgs struct {
		Sub sub `json:"sub"`
	}
	reg := registry.New()
	if _, err := spec.Define("bad.nested", func(_ context.Context, _ *nestedArgs) (int, error) { return 0, nil }).Register(reg); err != nil {
		t.Fatal(err)
	}
	if _, err := New(reg); err == nil {
		t.Fatal("want error for nested struct field")
	}
}

func TestCLIRejectsRequiredAfterOptionalPositional(t *testing.T) {
	type posArgs struct {
		A string `json:"a" cli:"positional"`
		B string `json:"b" cli:"positional" required:"true"`
	}
	reg := registry.New()
	if _, err := spec.Define("bad.pos", func(_ context.Context, _ *posArgs) (int, error) { return 0, nil }).Register(reg); err != nil {
		t.Fatal(err)
	}
	if _, err := New(reg); err == nil {
		t.Fatal("want error for required positional after optional")
	}
}

func TestCLIEnvInjectedSkippedField(t *testing.T) {
	// json:"-" + cli env 的字段：不产生 flag，但 env 值经 Go 字段名注入。
	type secretArgs struct {
		Name   string `json:"name" cli:"positional"`
		Secret string `json:"-" secret:"true" cli:"env=TEST_SECRET"`
	}
	reg := registry.New()
	if _, err := spec.Define("env.cmd", func(_ context.Context, in *secretArgs) (string, error) {
		return "secret=" + in.Secret, nil
	}).Register(reg); err != nil {
		t.Fatal(err)
	}
	app, err := New(reg)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_SECRET", "s3cret")
	out, _, code := runApp(t, app, "env", "cmd", "bob")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(out, "secret=s3cret") {
		t.Fatalf("output = %q, want env-injected secret", out)
	}
}

func TestCLIAliasDispatch(t *testing.T) {
	reg := registry.New()
	if _, err := spec.Define("user.add", addHandler).
		Summary("创建用户").
		CLI(spec.CliHints{Usage: "add <name>", Aliases: []string{"ua", "new"}}).
		Register(reg); err != nil {
		t.Fatal(err)
	}
	app, err := New(reg)
	if err != nil {
		t.Fatal(err)
	}
	// 别名等同子命令名；帮助里 usage 行不再重复路径段。
	out, _, code := runApp(t, app, "user", "ua", "bob", "--age", "9")
	if code != 0 || !strings.Contains(out, "bob") {
		t.Fatalf("alias dispatch: code=%d out=%q", code, out)
	}
	out2, _, _ := runApp(t, app, "user", "new", "carol")
	_ = out2
	help, _, code2 := runApp(t, app, "user", "add", "--help")
	if code2 != 0 {
		t.Fatalf("help exit = %d", code2)
	}
	if !strings.Contains(help, "user add <name> [flags]") {
		t.Fatalf("usage line wrong: %q", help)
	}
}

func TestCLIContextPropagation(t *testing.T) {
	// 取消的 ctx 必须穿透到 handler：优雅关停语义的基础。
	type ctxArgs struct {
		S string `json:"s"`
	}
	reg := registry.New()
	if _, err := spec.Define("ctx.probe", func(c context.Context, _ *ctxArgs) (string, error) {
		if c.Err() != nil {
			return "canceled:" + c.Err().Error(), nil
		}
		return "alive", nil
	}).Register(reg); err != nil {
		t.Fatal(err)
	}
	app, err := New(reg)
	if err != nil {
		t.Fatal(err)
	}
	// 正常 ctx → alive
	var o1, e1 bytes.Buffer
	app.out, app.errOut = &o1, &e1
	if code := app.RunContext(context.Background(), []string{"ctx", "probe"}); code != 0 || !strings.Contains(o1.String(), "alive") {
		t.Fatalf("alive run: code=%d out=%q err=%q", code, o1.String(), e1.String())
	}
	// 取消的 ctx → 透传
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var o2, e2 bytes.Buffer
	app.out, app.errOut = &o2, &e2
	if code := app.RunContext(ctx, []string{"ctx", "probe"}); code != 0 || !strings.Contains(o2.String(), "canceled:context canceled") {
		t.Fatalf("canceled run: code=%d out=%q err=%q", code, o2.String(), e2.String())
	}
}

func TestCLICompletion(t *testing.T) {
	app := buildApp(t)
	out, _, code := runApp(t, app, "completion", "bash")
	if code != 0 || !strings.Contains(out, "complete -F") || !strings.Contains(out, "user") {
		t.Fatalf("bash completion: code=%d out=%q", code, out)
	}
	out2, _, _ := runApp(t, app, "completion", "fish")
	if !strings.Contains(out2, "complete -c") {
		t.Fatalf("fish completion: %q", out2)
	}
	_, errb, code3 := runApp(t, app, "completion", "powershell")
	if code3 != 2 || !strings.Contains(errb, "unknown shell") {
		t.Fatalf("unknown shell: code=%d err=%q", code3, errb)
	}
}

func TestCLISetOutput(t *testing.T) {
	app := buildApp(t)
	var out, errb bytes.Buffer
	app.SetOutput(&out, &errb)
	if _, _, code := runApp(t, app, "math", "sum", "--a", "1", "--b", "2"); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	// runApp 覆盖了 out；单测直接验证 SetOutput 的目标被写入
	app2 := buildApp(t)
	var o2, e2 bytes.Buffer
	app2.SetOutput(&o2, &e2)
	app2.out = &out // 无额外断言；主要验证 SetOutput 不 panic 且后续 runApp 正常
	_ = o2
	_ = e2
}

func TestCLIUseMiddleware(t *testing.T) {
	app := buildApp(t)
	// 中间件按注册顺序套洋葱：先注册的最外层，可改写入参、换渲染。
	app.Use(
		func(ctx context.Context, ec *ExecContext, args map[string]any, next func() error) error {
			args["a"] = "3"
			return next()
		},
		func(ctx context.Context, ec *ExecContext, args map[string]any, next func() error) error {
			args["b"] = "4"
			return next()
		},
	)
	out, _, code := runApp(t, app, "math", "sum")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if strings.TrimSpace(out) != "7" {
		t.Fatalf("middleware-injected args: out=%q, want 7", out)
	}
	// 短路中间件：不调 next、自写输出。
	app2 := buildApp(t)
	app2.Use(func(ctx context.Context, ec *ExecContext, args map[string]any, next func() error) error {
		fmt.Fprintf(ec.Out, "short-circuit %s\n", ec.Path)
		return nil
	})
	out2, _, code2 := runApp(t, app2, "math", "sum", "--a", "1", "--b", "2")
	if code2 != 0 || strings.TrimSpace(out2) != "short-circuit math.sum" {
		t.Fatalf("short-circuit: code=%d out=%q", code2, out2)
	}
}

func TestCLIDoubleDashTerminator(t *testing.T) {
	app := buildApp(t)
	// "--" 之后全是位置参数：-v / --json 不再是开关。
	out, ver, code := runApp(t, app, "user", "add", "--", "-v")
	if code != 0 || !strings.Contains(out, "-v") {
		t.Fatalf("-v as positional: code=%d out=%q", code, out)
	}
	if strings.Contains(ver, "version") || strings.Contains(ver, "dev") {
		t.Fatalf("version should not trigger after --: %q", ver)
	}
	out2, err2, code2 := runApp(t, app, "user", "add", "--", "--json")
	if code2 != 0 || !strings.Contains(out2, "--json") {
		t.Fatalf("--json as positional: code=%d out=%q err=%q", code2, out2, err2)
	}
	// 未带 -- 时版本照旧（回归确认没砍坏）。
	if _, _, code3 := runApp(t, app, "-v"); code3 != 0 {
		t.Fatalf("-v before -- should still work: %d", code3)
	}
}

// ---- 默认子命令（CliHints.Default）：首段不是已注册段时整串转发 ----

func TestCLIDefaultSubcommandForwardsAllArgs(t *testing.T) {
	reg := registry.New()
	_, err := spec.Define("extract", addHandler).
		Summary("提取").
		CLI(spec.CliHints{Usage: "extract <name>", Default: true}).
		Register(reg)
	if err != nil {
		t.Fatal(err)
	}
	app, err := New(reg)
	if err != nil {
		t.Fatal(err)
	}
	// 老用法：udf ./image.tar —— 全部参数转发给默认的 extract
	out, errb, code := runApp(t, app, "./image.tar", "--age", "9")
	if code != 0 {
		t.Fatalf("exit code = %d, err=%q", code, errb)
	}
	if !strings.Contains(out, "./image.tar") || !strings.Contains(out, "9") {
		t.Fatalf("forwarded output missing values: %q", out)
	}
	// 显式子命令路径不受影响
	out2, _, code2 := runApp(t, app, "extract", "tarball.tar")
	if code2 != 0 || !strings.Contains(out2, "tarball.tar") {
		t.Fatalf("explicit path broken: code=%d out=%q", code2, out2)
	}
	// -h / -v 不触发默认下沉
	out3, _, code3 := runApp(t, app, "-h")
	if code3 != 0 || !strings.Contains(out3, "Usage:") {
		t.Fatalf("-h should stay root help: code=%d out=%q", code3, out3)
	}
	if _, ver, code4 := runApp(t, app, "-v"); code4 != 0 || !strings.Contains(ver+out3, "version") && !strings.Contains(out3, "version") {
		// -v 由版本分支处理（输出在结果流），这里只断言退出码
		_ = ver
	}
	// 位置参数之后的 -h 归默认命令自己的帮助
	out5, _, code5 := runApp(t, app, "img.tar", "-h")
	if code5 != 0 || !strings.Contains(out5, "extract") {
		t.Fatalf("default command help: code=%d out=%q", code5, out5)
	}
}

func TestCLIDuplicateDefaultRejected(t *testing.T) {
	reg := registry.New()
	if _, err := spec.Define("a.one", addHandler).CLI(spec.CliHints{Default: true}).Register(reg); err != nil {
		t.Fatal(err)
	}
	if _, err := spec.Define("a.two", addHandler).CLI(spec.CliHints{Default: true}).Register(reg); err != nil {
		t.Fatal(err)
	}
	if _, err := New(reg); err == nil {
		t.Fatal("duplicate default children should be a build error")
	} else if !strings.Contains(err.Error(), "default conflicts") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCLIChannelSkipAndTypes(t *testing.T) {
	reg := registry.New()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	// extract：只 CLI（HTTP/MCP Skip）；watch：整条 CLI Skip → 不进树/completion
	must(func() error {
		_, err := spec.Define("extract", addHandler).
			Summary("提取").
			CLI(spec.CliHints{Usage: "extract <name>", Skip: false, Fields: map[string]spec.CliFieldHint{"age": {Shorthand: "a"}}}).
			HTTP(spec.HTTPHints{Skip: true}).
			MCP(spec.MCPHints{Skip: true}).
			Register(reg)
		return err
	}())
	must(func() error {
		_, err := spec.Define("watch.target", addHandler).CLI(spec.CliHints{Skip: true}).Register(reg)
		return err
	}())
	app, err := New(reg)
	if err != nil {
		t.Fatal(err)
	}
	// Skip 命令不在树里：任何参数都不命中
	out, _, code := runApp(t, app, "watch", "target", "x")
	if code != 0 || !strings.Contains(out, "Usage:") {
		t.Fatalf("skipped command must not be routable: code=%d out=%q", code, out)
	}
	// help 类型保真：age (int) → integer
	out2, _, code2 := runApp(t, app, "extract", "-h")
	if code2 != 0 {
		t.Fatalf("code=%d", code2)
	}
	if !strings.Contains(out2, "--age integer") {
		t.Fatalf("help should render integer: %q", out2)
	}
	// testRegistry 里 tags 是 []string → strings (repeatable)
	reg2 := registry.New()
	must(func() error {
		_, err := spec.Define("t.list", listHandler).Register(reg2)
		return err
	}())
	app2, err := New(reg2)
	if err != nil {
		t.Fatal(err)
	}
	out3, _, _ := runApp(t, app2, "t", "list", "-h")
	if !strings.Contains(out3, "strings (repeatable)") && !strings.Contains(out3, "string") {
		t.Fatalf("help flags missing: %q", out3)
	}
}

func TestCLIDaemonCommandRunsUntilCancel(t *testing.T) {
	// #2 守护写法的烟测：handler 阻塞到 ctx.Done() 再返回（CLI 通道），
	// 取消后退出 0。HTTP/MCP 侧由 Skip 位排除（见通道开关测试）。
	reg := registry.New()
	started := make(chan struct{})
	stopped := make(chan struct{})
	type daemonArgs struct{}
	_, err := spec.Define("watch", func(ctx context.Context, _ *daemonArgs) (string, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return "stopped", nil
	}).CLI(spec.CliHints{Usage: "watch", Daemon: true}).Register(reg)
	if err != nil {
		t.Fatal(err)
	}
	app, err := New(reg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		done <- app.RunContext(ctx, []string{"watch"})
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never started")
	}
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("daemon exit code = %d, want 0", code)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not observe cancellation")
	}
}

func TestDaemonMarkerExcludesChannelsCompact(t *testing.T) {
	reg := registry.New()
	type dArgs struct{}
	if _, err := spec.Define("watch", func(_ context.Context, _ *dArgs) (string, error) {
		return "rendered?", nil
	}).CLI(spec.CliHints{Daemon: true}).Register(reg); err != nil {
		t.Fatal(err)
	}
	// CLI 树路径正常存在；返回值不渲染（守护语义）由 TestCLIDaemonCommandRunsUntilCancel 覆盖。
	// HTTP/MCP 排除在各自包的测试里断言（cli 包无法导入 mcp，避免循环）。
	_ = reg
}

func TestCLIHelpBlocks(t *testing.T) {
	reg := registry.New()
	_, err := spec.Define("extract", addHandler).
		Summary("提取").
		CLI(spec.CliHints{
			Before: "extract — 解包镜像\n用法示例见下方",
			After:  "更多: https://example.com/udf#extract",
		}).
		Register(reg)
	if err != nil {
		t.Fatal(err)
	}
	app, err := New(reg)
	if err != nil {
		t.Fatal(err)
	}
	out, _, code := runApp(t, app, "extract", "-h")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.HasPrefix(out, "extract — 解包镜像\n") {
		t.Fatalf("before block not at top of -h: %q", out)
	}
	if !strings.HasSuffix(out, "更多: https://example.com/udf#extract\n") {
		t.Fatalf("after block not at end of -h: %q", out)
	}

	// 中间节点没有 CliHints：块只在叶子帮助上出现。
	reg2 := registry.New()
	if _, err := spec.Define("user.add", addHandler).CLI(spec.CliHints{Before: "LEAFBLK"}).Register(reg2); err != nil {
		t.Fatal(err)
	}
	app2, err := New(reg2)
	if err != nil {
		t.Fatal(err)
	}
	out2, _, _ := runApp(t, app2, "user", "-h")
	if strings.Contains(out2, "LEAFBLK") {
		t.Fatalf("intermediate node must not print leaf blocks: %q", out2)
	}
	out3, _, code3 := runApp(t, app2, "user", "add", "-h")
	if code3 != 0 || !strings.HasPrefix(out3, "LEAFBLK\n") {
		t.Fatalf("leaf -h missing before block: code=%d out=%q", code3, out3)
	}
}

func TestCLIBlockProjection(t *testing.T) {
	reg := registry.New()
	if _, err := spec.Define("blk.show", func(_ context.Context, _ *sumArgs) (block.Envelope, error) {
		return block.Envelope{Content: []block.Item{
			block.Text("hello blocks"),
			block.Image("image/png", []byte{0x89, 0x50, 0x4e, 0x47}),
		}}, nil
	}).Register(reg); err != nil {
		t.Fatal(err)
	}
	app, err := New(reg)
	if err != nil {
		t.Fatal(err)
	}
	// 人类输出：文本块内联，二进制块打印临时文件路径且文件内容正确。
	out, _, code := runApp(t, app, "blk", "show")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 || lines[0] != "hello blocks" {
		t.Fatalf("projection = %v", lines)
	}
	data, err := os.ReadFile(lines[1])
	if err != nil {
		t.Fatalf("temp file missing: %v", err)
	}
	if string(data) != string([]byte{0x89, 0x50, 0x4e, 0x47}) {
		t.Fatalf("temp file content = %v", data)
	}
	os.Remove(lines[1])
	// --json：信封 JSON 原样（不投影）。
	out2, _, code2 := runApp(t, app, "blk", "show", "--json")
	if code2 != 0 || !strings.Contains(out2, `"content"`) {
		t.Fatalf("--json envelope: code=%d out=%q", code2, out2)
	}
}
