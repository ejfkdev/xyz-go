//go:build nohttp

package xyz

import "testing"

// -tags nohttp 构建下，serve 模式由 stub 兜底：报错并退出 1。
func TestRunServeStub(t *testing.T) {
	for _, args := range [][]string{{"serve"}, {"serve", "--addr", ":1"}} {
		if got := Run(testReg(t, "a.b"), args); got != 1 {
			t.Fatalf("Run(%v) = %d, want 1 (stub)", args, got)
		}
	}
}
