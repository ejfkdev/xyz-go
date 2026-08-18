//go:build !nocli

package xyz

import "testing"

func TestRunCLIMode(t *testing.T) {
	if got := Run(testReg(t, "echo.hi"), []string{"echo", "hi", "--s", "yo"}); got != 0 {
		t.Fatalf("exit code = %d, want 0", got)
	}
}

func TestRunCustomModeWordsCLI(t *testing.T) {
	// 改名保留字后原词解禁：serve.x 走 CLI 分发。
	cfg := Config{Modes: ModeWords{Serve: "httpd", MCP: "protocol", Help: "assist"}}
	if got := RunConfig(testReg(t, "serve.x"), []string{"serve", "x", "--s", "yo"}, cfg); got != 0 {
		t.Fatalf("custom words released serve.x = %d, want 0", got)
	}
}
