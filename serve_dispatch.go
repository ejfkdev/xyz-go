//go:build !nohttp

package xyz

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/ejfkdev/xyz-go/httpapi"
	"github.com/ejfkdev/xyz-go/logx"
	"github.com/ejfkdev/xyz-go/registry"
)

// httpFrontend 标记本编译变体是否包含 HTTP 前端（用于总览标注）。
const httpFrontend = true

// runServe 启动 HTTP 前端：REST 路由 + /openapi.json；MCP 前端被编译时再把
// 流式 HTTP 工具端点挂在 /mcp（nomcp 构建下自动消失）。
func runServe(ctx context.Context, reg *registry.Registry, args []string, cfg Config) int {
	cfg = parseServeArgs(args, cfg)
	handler, err := httpapi.Handler(reg)
	if err != nil {
		logx.Errorf("%v", err)
		return 2
	}
	mcpNote := ""
	if mh, ok := mcpHTTPHandler(reg, cfg); ok {
		outer := http.NewServeMux()
		outer.Handle("/mcp", mh)
		outer.Handle("/", handler)
		handler = outer
		mcpNote = " + /mcp"
	}
	if len(cfg.CORSOrigins) > 0 {
		logx.Debugf("CORS 开启：%v", cfg.CORSOrigins)
	}
	// 中间件链（由外到内）：CORS 预检（鉴权前，浏览器预检不带凭据）→ Bearer → Gzip → 路由。
	handler = httpapi.CORS(cfg.CORSOrigins, httpapi.Bearer(cfg.BearerTokens, httpapi.Gzip(handler)))
	srv := &http.Server{Addr: cfg.Addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	srv.BaseContext = func(net.Listener) context.Context { return ctx }
	if cfg.Timeout > 0 {
		srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout = cfg.Timeout, cfg.Timeout, cfg.Timeout
	}
	scheme := "http"
	tlsOn := cfg.CertFile != "" || cfg.KeyFile != ""
	if tlsOn {
		if cfg.CertFile == "" || cfg.KeyFile == "" {
			logx.Errorf("TLS 需要同时给定 --tls-cert 与 --tls-key")
			return 2
		}
		scheme = "https"
	}
	logx.Infof("监听 %s://%s（REST + /openapi.json%s）", scheme, cfg.Addr, mcpNote)
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
		logx.Infof("已优雅关停（ctx 取消）")
		return 0
	}
}
