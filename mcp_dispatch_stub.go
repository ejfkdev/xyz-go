//go:build nomcp

package xyz

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/ejfkdev/xyz-go/langx"
	"github.com/ejfkdev/xyz-go/registry"
)

// runMCP 是 -tags nomcp 构建下的 MCP 模式兜底：本二进制没有编译进 MCP
// 前端（用于只发 CLI 的精简发布）。
func runMCP(_ context.Context, _ *registry.Registry, _ []string, _ Config) int {
	fmt.Fprintln(os.Stderr, "xyz: "+langx.Tf("stub.not_compiled", "MCP")+" (built with -tags nomcp)")
	return 1
}

// mcpHTTPHandler 在 nomcp 构建下不可用。
func mcpHTTPHandler(_ *registry.Registry, _ Config) (http.Handler, bool) { return nil, false }
