//go:build !nomcp

package xyz

import "testing"

// 默认构建（含 MCP 前端）下，mcp 模式的用法错误由 MCP 前端报出。
func TestRunMCPUsageError(t *testing.T) {
	// mcp 缺少 transport 参数 → 用法错误；mcp stdio 会真正运行并阻塞，
	// 由 mcp 包自己的测试覆盖。
	for _, args := range [][]string{{"mcp"}, {"mcp", "carrier-pigeon"}} {
		if got := Run(testReg(t, "a.b"), args); got != 2 {
			t.Fatalf("Run(%v) = %d, want 2 (usage)", args, got)
		}
	}
	// 自定义 mcp 词同样直达 MCP 分发。
	cfg := Config{Modes: ModeWords{Serve: "httpd", MCP: "protocol", Help: "assist"}}
	if got := RunConfig(testReg(t, "a.b"), []string{"protocol"}, cfg); got != 2 {
		t.Fatalf("RunConfig(protocol) = %d, want 2 (missing transport)", got)
	}
}
