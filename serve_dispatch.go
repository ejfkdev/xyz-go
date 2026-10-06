//go:build !nohttp

package xyz

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/ejfkdev/xyz-go/httpapi"
	"github.com/ejfkdev/xyz-go/langx"
	"github.com/ejfkdev/xyz-go/logx"
	"github.com/ejfkdev/xyz-go/registry"
)

// httpFrontend 标记本编译变体是否包含 HTTP 前端（用于总览标注）。
const httpFrontend = true

// runServe 启动 HTTP 前端：REST 路由 + /openapi.json。mountMCP 为真（serve
// 模式且未禁用 MCP）时再把流式 HTTP 工具端点挂在 /mcp；http 模式传 false =
// 单独 HTTP 接口，不挂 /mcp（nomcp 构建下 mcpHTTPHandler 自动返回 false）。
func runServe(ctx context.Context, reg *registry.Registry, args []string, cfg Config, mountMCP bool) int {
	cfg = parseServeArgs(args, cfg)
	meta := httpapi.ResponseMeta{
		AppName:       cfg.resolvedName(),
		AppVersion:    cfg.resolvedVersion(),
		SDKVersion:    SDKVersion,
		Headers:       cfg.ResponseHeaders,
		NoAutoHeaders: cfg.NoServerHeaders,
	}
	handler, err := httpapi.HandlerWithMeta(reg, cfg.ChannelDefaults, meta)
	if err != nil {
		logx.Errorf("%v", err)
		return 2
	}
	mcpNote := ""
	if mountMCP {
		if mh, ok := mcpHTTPHandler(reg, cfg); ok {
			outer := http.NewServeMux()
			outer.Handle("/mcp", mh)
			outer.Handle("/", handler)
			handler = outer
			mcpNote = " + /mcp"
		}
	}
	if len(cfg.CORSOrigins) > 0 {
		logx.Debugf("%s", langx.Tf("log.cors_on", fmt.Sprint(cfg.CORSOrigins)))
	}
	// 中间件链（由外到内）：服务器上下文头（版本/自定义头，覆盖含 /mcp 的
	// 全部路由）→ CORS 预检（鉴权前，浏览器预检不带凭据）→ Bearer → Gzip → 路由。
	handler = httpapi.ServerHeaders(meta,
		httpapi.CORS(cfg.CORSOrigins, httpapi.Bearer(cfg.BearerTokens, httpapi.Gzip(handler))))
	srv := &http.Server{Addr: cfg.Addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	srv.BaseContext = func(net.Listener) context.Context { return ctx }
	if cfg.Timeout > 0 {
		srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout = cfg.Timeout, cfg.Timeout, cfg.Timeout
	}
	scheme := "http"
	tlsOn := cfg.CertFile != "" || cfg.KeyFile != ""
	if tlsOn {
		if cfg.CertFile == "" || cfg.KeyFile == "" {
			logx.Errorf("TLS requires both --tls-cert and --tls-key")
			return 2
		}
		scheme = "https"
	}
	logx.Infof("%s", langx.Tf("log.serve_listening", scheme, cfg.Addr, mcpNote))
	errc := make(chan error, 1)
	go func() {
		if tlsOn {
			errc <- srv.ListenAndServeTLS(cfg.CertFile, cfg.KeyFile)
		} else {
			errc <- srv.ListenAndServe()
		}
	}()
	select {
	case serveErr := <-errc:
		if serveErr != nil && serveErr != http.ErrServerClosed {
			logx.Errorf("%v", serveErr)
			return 1
		}
		return 0
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		logx.Infof("%s", langx.T("log.graceful"))
		return 0
	}
}
