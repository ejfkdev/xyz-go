package xyz

import (
	"context"
	"testing"
)

type chainArgs struct {
	S string `json:"s"`
}

func chainHandler(_ context.Context, in *chainArgs) (string, error) { return in.S, nil }

func TestBuilderRegistrationErrorSurfacesAtRun(t *testing.T) {
	code := Define("dup.a", chainHandler).
		Summary("x").
		Also(
			Define("ok.one", chainHandler).Summary("ok"),
			Define("dup.a", chainHandler).Summary("y"), // 与开头命令重名
		).
		RunArgs(nil)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 for registration failure", code)
	}
}

func TestBuilderIsDefinable(t *testing.T) {
	// Builder 满足 Definable：可以作为 Also/Main 的参数互相嵌套。
	var _ Definable = Define("def.able", chainHandler)
}

func TestBuilderConfigureDisablesCLI(t *testing.T) {
	// 链上 Configure：禁用 CLI 后子命令报错，壳能力保留。
	b := Define("cfg.cmd", chainHandler).Summary("配置演示").
		Configure(Config{Capabilities: Capabilities{NoCLI: true}})
	if code := b.RunArgs([]string{"cfg", "cmd", "--s", "x"}); code != 1 {
		t.Fatalf("NoCLI dispatch = %d, want 1", code)
	}
	if code := b.RunArgs([]string{"help"}); code != 0 {
		t.Fatalf("NoCLI help = %d, want 0", code)
	}
	if code := b.RunArgs([]string{"-v"}); code != 0 {
		t.Fatalf("NoCLI -v = %d, want 0", code)
	}
}

func TestBuilderConfigureDisablesMCP(t *testing.T) {
	b := Define("cfg2.cmd", chainHandler).Summary("配置演示2").
		Configure(Config{Capabilities: Capabilities{NoMCP: true}})
	if code := b.RunArgs([]string{"mcp", "stdio"}); code != 1 {
		t.Fatalf("NoMCP mcp stdio = %d, want 1 (disabled)", code)
	}
}

func TestBuilderMultiDispatch(t *testing.T) {
	// 同一个 Builder 允许多次 RunArgs（每次派发是幂等的；注册只发生一次）。
	b := Define("multi.cmd", chainHandler).Summary("多次派发")
	if code := b.RunArgs([]string{"help"}); code != 0 {
		t.Fatalf("first dispatch = %d, want 0", code)
	}
	if code := b.RunArgs([]string{"help"}); code != 0 {
		t.Fatalf("second dispatch = %d, want 0 (no duplicate registration)", code)
	}
}
