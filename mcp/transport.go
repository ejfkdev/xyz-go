package mcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ejfkdev/xyz-go/langx"
	"github.com/ejfkdev/xyz-go/logx"
)

func serveHTTP(ctx context.Context, addr string, handler http.Handler) int {
	if addr == "" {
		addr = ":8080"
	}
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	srv.BaseContext = func(net.Listener) context.Context { return ctx }
	logx.Infof("%s", langx.Tf("log.mcp_listening", addr))
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	select {
	case err := <-errc:
		if err != nil && err != http.ErrServerClosed {
			logx.Errorf("%v", err)
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

// makeHandler wires one registry entry to the shared Invoke pipeline.
// MCP-specific defaults are injected exactly like the other frontends do,
// and calls negotiated outside the configured version subset are rejected.
func validateVersions(versions []string) error {
	for _, v := range versions {
		if v == "" {
			return errors.New("mcp: empty protocol version in --versions")
		}
		if !slices.Contains(DefaultVersions, v) {
			return fmt.Errorf("mcp: unknown protocol version %q (known: %s)", v, strings.Join(DefaultVersions, ", "))
		}
	}
	return nil
}

// validateTransportVersions rejects explicit version lists none of which the
// chosen transport can serve. The 2026-07-28 revision removed the HTTP+SSE
// binding, so SSE caps below it; streamable HTTP only speaks 2026-07-28 in
// Stateless mode. These are the same constraints the SDK applies internally.
func validateTransportVersions(transport string, opts Options) error {
	if len(opts.Versions) == 0 {
		return nil // 默认全集：SDK 按传输自身能力裁剪
	}
	for _, v := range opts.Versions {
		if transportServes(transport, v, opts.Stateless) {
			return nil
		}
	}
	return fmt.Errorf("transport %q cannot serve any of the requested versions %v", transport, opts.Versions)
}

func transportServes(transport, v string, stateless bool) bool {
	switch transport {
	case "sse":
		return v != ProtocolV2026_07_28
	case "http":
		return v != ProtocolV2026_07_28 || stateless
	default: // stdio 全版本
		return true
	}
}

func versionSet(versions []string) map[string]bool {
	if len(versions) == 0 {
		versions = DefaultVersions
	}
	set := make(map[string]bool, len(versions))
	for _, v := range versions {
		set[v] = true
	}
	return set
}

// versionFilterTransport trims the versions the SDK advertises for a
// session, via the SDK's optional ProtocolVersionSupporter interface.
type versionFilterTransport struct {
	sdkmcp.Transport
	allowed map[string]bool
}

// SupportsProtocolVersion reports whether this transport may serve the
// given protocol version.
func (t versionFilterTransport) SupportsProtocolVersion(version string) bool {
	return t.allowed[version]
}

// versionGate rejects streamable/SSE requests that pin an explicit
// MCP-Protocol-Version outside the configured subset, before the SDK sees
// them.
func versionGate(h http.Handler, versions []string, allowed map[string]bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := r.Header.Get("MCP-Protocol-Version"); v != "" && !allowed[v] {
			supported := versions
			if len(supported) == 0 {
				supported = DefaultVersions
			}
			http.Error(w, fmt.Sprintf("unsupported protocol version %q (supported: %s)",
				v, strings.Join(supported, ",")), http.StatusBadRequest)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// parseArgs handles: <transport> [--addr A] [--versions v1,v2] [--name N]
// [--server-version V] [--json-response] [--stateless]
