package cli

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/ejfkdev/xyz-go/registry"
	"github.com/ejfkdev/xyz-go/spec"
)

// 全局 --format text|json|jsonl|markdown（xyz-spec §10.6）。
// --json 保留为 --format json 的别名；显式格式绕过命令的 CLIOutputFunc。

func TestCLIFormatText(t *testing.T) {
	out, _, code := runApp(t, buildApp(t), "user", "add", "bob", "--format", "text")
	if code != 0 || !strings.Contains(out, "bob") {
		t.Fatalf("text: code=%d out=%q", code, out)
	}
}

func TestCLIFormatJSON(t *testing.T) {
	out, _, code := runApp(t, buildApp(t), "user", "add", "bob", "--format", "json")
	if code != 0 {
		t.Fatalf("json: code=%d out=%q", code, out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, out)
	}
	if got["name"] != "bob" {
		t.Fatalf("json name = %v", got["name"])
	}
}

func TestCLIFormatJSONAlias(t *testing.T) {
	out, _, code := runApp(t, buildApp(t), "user", "add", "bob", "--json")
	if code != 0 || !strings.Contains(out, `"name"`) {
		t.Fatalf("--json alias: code=%d out=%q", code, out)
	}
}

func TestCLIFormatJSONLSlice(t *testing.T) {
	// []struct → 每元素一行紧凑 JSON。
	out, _, code := runApp(t, buildApp(t), "user", "list", "--format", "jsonl")
	if code != 0 {
		t.Fatalf("jsonl: code=%d out=%q", code, out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("jsonl lines = %d, want 2 (%q)", len(lines), out)
	}
	for i, ln := range lines {
		var got map[string]any
		if err := json.Unmarshal([]byte(ln), &got); err != nil {
			t.Fatalf("line %d not JSON: %v (%q)", i, err, ln)
		}
		if strings.Contains(ln, "  ") {
			t.Fatalf("jsonl line %d must be compact (no indent): %q", i, ln)
		}
	}
}

func TestCLIFormatJSONLScalar(t *testing.T) {
	// 非切片 → 整体一行。
	out, _, code := runApp(t, buildApp(t), "math", "sum", "--a", "1", "--b", "2", "--format", "jsonl")
	if code != 0 {
		t.Fatalf("jsonl scalar: code=%d", code)
	}
	if strings.TrimRight(out, "\n") != "3" {
		t.Fatalf("jsonl scalar = %q, want 3", out)
	}
}

func TestCLIFormatMarkdownStruct(t *testing.T) {
	out, _, code := runApp(t, buildApp(t), "user", "add", "bob", "--format", "markdown")
	if code != 0 {
		t.Fatalf("markdown: code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "| Field | Value |") || !strings.Contains(out, "| --- | --- |") {
		t.Fatalf("markdown struct missing table header: %q", out)
	}
	if !strings.Contains(out, "| name | bob |") || !strings.Contains(out, "| age |") {
		t.Fatalf("markdown struct rows: %q", out)
	}
}

func TestCLIFormatMarkdownTable(t *testing.T) {
	// []struct → 以字段为列的表。
	out, _, code := runApp(t, buildApp(t), "user", "list", "--format", "markdown")
	if code != 0 {
		t.Fatalf("markdown table: code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "| name | age |") {
		t.Fatalf("markdown table header: %q", out)
	}
	if !strings.Contains(out, "| alice | 18 |") || !strings.Contains(out, "| bob | 25 |") {
		t.Fatalf("markdown table rows: %q", out)
	}
}

func TestCLIFormatMarkdownEscaping(t *testing.T) {
	reg := registry.New()
	type escResp struct {
		Text string `json:"text"`
	}
	if _, err := spec.Define("esc.show", func(_ context.Context, in *listArgs) (*escResp, error) {
		return &escResp{Text: "a|b\nc"}, nil
	}).Register(reg); err != nil {
		t.Fatal(err)
	}
	app, err := New(reg)
	if err != nil {
		t.Fatal(err)
	}
	out, _, code := runApp(t, app, "esc", "show", "--format", "markdown")
	if code != 0 {
		t.Fatalf("esc: code=%d out=%q", code, out)
	}
	if !strings.Contains(out, `a\|b<br>c`) {
		t.Fatalf("markdown escaping (| → \\|, \\n → <br>): %q", out)
	}
}

func TestCLIFormatInvalid(t *testing.T) {
	_, errOut, code := runApp(t, buildApp(t), "user", "add", "bob", "--format", "xml")
	if code != 2 {
		t.Fatalf("invalid format code = %d, want 2", code)
	}
	if !strings.Contains(errOut, "invalid output format") {
		t.Fatalf("invalid format err = %q", errOut)
	}
}

func TestCLIFormatMissingArg(t *testing.T) {
	_, errOut, code := runApp(t, buildApp(t), "user", "add", "bob", "--format")
	if code != 2 || !strings.Contains(errOut, "--format needs an argument") {
		t.Fatalf("missing arg: code=%d err=%q", code, errOut)
	}
}

func TestCLIMachineModeErrorJSON(t *testing.T) {
	// user.add name=="missing" → KindNotFound；--format json 下 stderr 输出富化错误体。
	_, errOut, code := runApp(t, buildApp(t), "user", "add", "missing", "--format", "json")
	if code == 0 {
		t.Fatalf("expected non-zero exit for error")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(errOut), &body); err != nil {
		t.Fatalf("machine-mode error not JSON: %v (%q)", err, errOut)
	}
	if body["kind"] != "not_found" {
		t.Fatalf("error kind = %v, want not_found", body["kind"])
	}
	if body["error"] == nil || body["error"] == "" {
		t.Fatalf("error message missing: %v", body)
	}
}

func TestCLIFormatBypassesCustomOutput(t *testing.T) {
	// 显式 --format markdown 绕过 CLIOutputFunc（与 --json 同优先级语义）；
	// text（默认）才触发自定义 Output。
	reg := registry.New()
	called := 0
	if _, err := spec.Define("rich.show", func(_ context.Context, in *sumArgs) (int, error) {
		return in.A + in.B, nil
	}).CLI(spec.CliHints{
		Output: func(w io.Writer, v any) error { called++; return nil },
	}).Register(reg); err != nil {
		t.Fatal(err)
	}
	app, err := New(reg)
	if err != nil {
		t.Fatal(err)
	}
	runApp(t, app, "rich", "show", "--a", "1", "--b", "2")
	if called != 1 {
		t.Fatalf("text should call Output, called=%d", called)
	}
	runApp(t, app, "rich", "show", "--a", "1", "--b", "2", "--format", "markdown")
	if called != 1 {
		t.Fatalf("markdown must bypass Output, called=%d", called)
	}
}

// --xyz.format 经 Options.Format 注入为默认格式；裸 --format 覆盖之。
func TestCLIDefaultFormatViaOptions(t *testing.T) {
	reg := registry.New()
	type pResp struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	if _, err := spec.Define("p.get", func(_ context.Context, in *listArgs) (*pResp, error) {
		return &pResp{Name: "bob", Age: 9}, nil
	}).Register(reg); err != nil {
		t.Fatal(err)
	}
	app, err := NewWithOptions(reg, Options{Format: FormatJSONL})
	if err != nil {
		t.Fatal(err)
	}
	// 默认 jsonl：结构体 → 单行紧凑 JSON。
	out, _, code := runApp(t, app, "p", "get")
	if code != 0 {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if strings.TrimRight(out, "\n") != `{"name":"bob","age":9}` {
		t.Fatalf("default jsonl = %q", out)
	}
	// 裸 --format markdown 覆盖默认 → 两列表。
	out2, _, code2 := runApp(t, app, "p", "get", "--format", "markdown")
	if code2 != 0 || !strings.Contains(out2, "| Field | Value |") || !strings.Contains(out2, "| name | bob |") {
		t.Fatalf("bare override markdown: code=%d out=%q", code2, out2)
	}
}

// 冲突让位：命令自有 format 字段时，裸 --format 归命令而非全局；全局格式
// 只认全称（Options.Format / --xyz.format）。
func TestCLIFormatConflictYieldsToCommand(t *testing.T) {
	reg := registry.New()
	type convArgs struct {
		Format string `json:"format"` // 命令自有的 format flag
	}
	if _, err := spec.Define("conv.run", func(_ context.Context, in *convArgs) (string, error) {
		return "fmt=" + in.Format, nil
	}).Register(reg); err != nil {
		t.Fatal(err)
	}
	// 无全局默认：裸 --format csv 让位给命令字段，全局保持 text，退出 0。
	app, err := New(reg)
	if err != nil {
		t.Fatal(err)
	}
	out, _, code := runApp(t, app, "conv", "run", "--format", "csv")
	if code != 0 {
		t.Fatalf("bare --format must yield to command field, got code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "fmt=csv") {
		t.Fatalf("command did not receive its --format value: %q", out)
	}
	// 有全局默认 json（来自 --xyz.format）：全局走 json，裸 --format 仍归命令。
	app2, err := NewWithOptions(reg, Options{Format: FormatJSON})
	if err != nil {
		t.Fatal(err)
	}
	out2, _, code2 := runApp(t, app2, "conv", "run", "--format", "csv")
	if code2 != 0 {
		t.Fatalf("code=%d out=%q", code2, out2)
	}
	// 全局 json → 结果是字符串 "fmt=csv" 的 JSON 编码（带引号）。
	if strings.TrimRight(out2, "\n") != `"fmt=csv"` {
		t.Fatalf("global json + command format: out=%q", out2)
	}
}
