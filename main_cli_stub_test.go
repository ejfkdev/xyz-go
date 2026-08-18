//go:build nocli

package xyz

import "testing"

// -tags nocli 构建下，子命令模式一律由 stub 兜底：报错并退出 1。
// （-v/--version 与 help 由根派发器处理，任何构建变体下都可用。）
func TestRunCLIStub(t *testing.T) {
	if got := Run(testReg(t, "echo.hi"), []string{"echo", "hi", "--s", "yo"}); got != 1 {
		t.Fatalf("dispatch = %d, want 1 (stub)", got)
	}
	if got := Run(testReg(t, "echo.hi"), []string{"-v"}); got != 0 {
		t.Fatalf("-v = %d, want 0 (root-level shell capability)", got)
	}
}
