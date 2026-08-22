//go:build !nomcp

package xyz

import (
	"context"
	"net/http"

	"github.com/ejfkdev/xyz-go/mcp"
	"github.com/ejfkdev/xyz-go/registry"
)

// runMCP 把 mcp 模式交给 MCP 前端。构建时加 -tags nomcp 可剔除整个官方
// MCP SDK 及其 JSON/认证依赖，只保留 CLI 前端（体积约减半）。
func runMCP(ctx context.Context, reg *registry.Registry, args []string, cfg Config) int {
	return mcp.RunContextWithOptions(ctx, reg, args, mcp.Options{Addr: cfg.Addr, BearerTokens: cfg.BearerTokens, Defaults: cfg.ChannelDefaults})
}

// mcpHTTPHandler 暴露流式 HTTP 工具端点，供 serve 模式挂载 /mcp。
func mcpHTTPHandler(reg *registry.Registry, cfg Config) (http.Handler, bool) {
	h, err := mcp.HTTPHandler(reg, mcp.Options{BearerTokens: cfg.BearerTokens, Defaults: cfg.ChannelDefaults})
	if err != nil || h == nil {
		return nil, false
	}
	return h, true
}
