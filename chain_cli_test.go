//go:build !nocli

package xyz

import (
	"testing"

	"github.com/ejfkdev/xyz-go/registry"
)

// 链式 Builder 走 CLI 子命令分发的集成测试（-tags nocli 构建下不适用）。

func TestBuilderRunArgs(t *testing.T) {
	code := Define("echo.hi", chainHandler).
		Summary("回显").
		Also(
			Define("echo.bye", chainHandler).Summary("再见"),
			Define("echo.ho", chainHandler).Summary("三号"),
		).
		RunArgs([]string{"echo", "hi", "--s", "yo"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if _, ok := registry.Default.Get("echo.bye"); !ok {
		t.Fatal("Also should register into the default registry")
	}
	if _, ok := registry.Default.Get("echo.hi"); !ok {
		t.Fatal("Run should register the opening command")
	}
}

func TestBuilderSingleCommandRun(t *testing.T) {
	// 单命令链：xyz.Define(...).XXX.Run() 形态。
	code := Define("solo.cmd", chainHandler).Summary("单飞").RunArgs([]string{"solo", "cmd", "--s", "hi"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if _, ok := registry.Default.Get("solo.cmd"); !ok {
		t.Fatal("single-command chain should register at Run")
	}
}
