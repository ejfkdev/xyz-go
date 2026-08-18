//go:build nomcp

package xyz

import "testing"

// -tags nomcp 构建下，mcp 模式一律由 stub 兜底：报错并退出 1。
func TestRunMCPStub(t *testing.T) {
	for _, args := range [][]string{{"mcp"}, {"mcp", "stdio"}, {"mcp", "carrier-pigeon"}} {
		if got := Run(testReg(t, "a.b"), args); got != 1 {
			t.Fatalf("Run(%v) = %d, want 1 (stub)", args, got)
		}
	}
}
