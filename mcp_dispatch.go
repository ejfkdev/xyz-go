//go:build !nomcp

package xyz

import (
	"context"
	"net/http"

	"github.com/ejfkdev/xyz-go/mcp"
	"github.com/ejfkdev/xyz-go/registry"
)

// mcpOptions 把根派发器的 Config 折叠成 MCP 前端选项（版本/自定义上下文头/
// 通道默认参数/鉴权）。runMCP 与 serve 挂载的 /mcp 端点共用，保证两条路径
// 报告的 serverInfo.version 与 _meta 上下文一致。
func mcpOptions(cfg Config) mcp.Options {
	return mcp.Options{
		Addr:            cfg.Addr,
		BearerTokens:    cfg.BearerTokens,
		Defaults:        cfg.ChannelDefaults,
		Name:            cfg.resolvedName(),
		Version:         cfg.resolvedVersion(),
		SDKVersion:      SDKVersion,
		ResponseHeaders: cfg.ResponseHeaders,
		NoServerMeta:    cfg.NoServerHeaders,
	}
}

// runMCP 把 mcp 模式交给 MCP 前端。构建时加 -tags nomcp 可剔除整个官方
// MCP SDK 及其 JSON/认证依赖，只保留 CLI 前端（体积约减半）。
func runMCP(ctx context.Context, reg *registry.Registry, args []string, cfg Config) int {
	return mcp.RunContextWithOptions(ctx, reg, args, mcpOptions(cfg))
}

// mcpHTTPHandler 暴露流式 HTTP 工具端点，供 serve 模式挂载 /mcp。
func mcpHTTPHandler(reg *registry.Registry, cfg Config) (http.Handler, bool) {
	h, err := mcp.HTTPHandler(reg, mcpOptions(cfg))
	if err != nil || h == nil {
		return nil, false
	}
	return h, true
}
