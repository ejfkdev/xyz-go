// Package mcp is the MCP frontend, built on the official Model Context
// Protocol Go SDK (github.com/modelcontextprotocol/go-sdk). It exposes
// every registered command as an MCP tool through the three transports
// (stdio / SSE / streamable HTTP) and lets the caller pin which protocol
// versions the server negotiates.
//
// Output contract: every successful call returns the human-readable text
// (the same rendering the CLI frontend uses) in Content plus the raw value
// as JSON in StructuredContent; failures are reported as isError=true
// results carrying the classified error message.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	stderrors "errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ejfkdev/xyz-go/httpapi"
	"github.com/ejfkdev/xyz-go/langx"
	"github.com/ejfkdev/xyz-go/logx"
	"github.com/ejfkdev/xyz-go/registry"
)

// Protocol versions known to the official Go SDK at v1.7.0. Negotiation
// prefers the client's requested version when the server supports it;
// 2026-07-28 is the latest revision and deprecates the legacy initialize
// handshake in favor of mutually-negotiated version lists.
const (
	ProtocolV2024_11_05 = "2024-11-05"
	ProtocolV2025_03_26 = "2025-03-26"
	ProtocolV2025_06_18 = "2025-06-18"
	ProtocolV2025_11_25 = "2025-11-25"
	ProtocolV2026_07_28 = "2026-07-28" // latest
)

// DefaultVersions is the full set the SDK can serve, in negotiation
// preference order (newest first).
var DefaultVersions = []string{
	ProtocolV2026_07_28,
	ProtocolV2025_11_25,
	ProtocolV2025_06_18,
	ProtocolV2025_03_26,
	ProtocolV2024_11_05,
}

// Options configures the MCP frontend. The zero value serves every protocol
// version the SDK knows.
type Options struct {
	// Name and Version identify this server implementation to clients.
	// Defaults: binary base name and "0.0.0".
	Name    string
	Version string

	// Versions restricts which protocol versions this server stands behind,
	// subset of DefaultVersions. Order is the preference order used during
	// negotiation. Empty means "all of them".
	Versions []string

	// Instructions is shown to clients after initialization.
	Instructions string

	// Addr is the listen address for the sse and http transports.
	// Default ":8080".
	Addr string

	// JSONResponse makes streamable HTTP answer with application/json
	// instead of text/event-stream (handy for debugging).
	JSONResponse bool

	// Stateless enables the streamable HTTP stateless mode (SEP-2567).
	Stateless bool

	// BearerTokens turns on Bearer-token verification for the http and sse
	// transports (stdio is local and unaffected). Empty means no auth.
	BearerTokens []string

	// SessionTimeout configures idle-session expiry for streamable HTTP
	// (the SDK's StreamableHTTPOptions.SessionTimeout). 0 keeps sessions.
	SessionTimeout time.Duration

	// CORSOrigins enables CORS for the http/sse transports ("*" = any origin).
	CORSOrigins []string
}

// Server builds a ready sdkmcp.Server with one tool per registered command.
// Tool input schemas come straight from the registry's JSON Schema
// generation; the shared Invoke pipeline does all decoding and validation.
func Server(reg *registry.Registry, opts Options) (*sdkmcp.Server, error) {
	if reg == nil {
		return nil, errors.New("mcp: nil registry")
	}
	if err := validateVersions(opts.Versions); err != nil {
		return nil, err
	}
	name, version := implName(opts)
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: name, Version: version},
		&sdkmcp.ServerOptions{Instructions: opts.Instructions})
	allowed := versionSet(opts.Versions)
	for _, e := range reg.All() {
		schemaJSON, err := json.Marshal(e.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("mcp: tool %q: %w", e.Name, err)
		}
		tool := &sdkmcp.Tool{
			Name:        e.Name,
			Description: toolDescription(e),
			InputSchema: json.RawMessage(schemaJSON),
			Annotations: parseAnnotations(e),
		}
		if e.OutputSchema != nil {
			if outJSON, err := json.Marshal(e.OutputSchema); err == nil {
				tool.OutputSchema = json.RawMessage(outJSON)
			}
		}
		server.AddTool(tool, makeHandler(e, allowed))
	}
	return server, nil
}

// Run parses the transport and flags from args and serves until the
// transport ends:
//
//	mcp stdio [flags]
//	mcp sse   [flags]     # flags: --addr, --versions v1,v2, --name, --server-version
//	mcp http  [flags]     # streamable HTTP; adds --json-response, --stateless
//
// It returns the process exit code.
func Run(reg *registry.Registry, args []string) int {
	return runWithOptions(context.Background(), reg, args, Options{})
}

// RunContext is Run with an explicit context (graceful shutdown), both for
// the stdio server and the http/sse listeners.
func RunContext(ctx context.Context, reg *registry.Registry, args []string) int {
	return runWithOptions(ctx, reg, args, Options{})
}

// RunWithOptions is Run with preset options (e.g. bearer tokens injected by
// the dispatcher's --xyz.bearer). Command-line flags still win over presets.
func RunWithOptions(reg *registry.Registry, args []string, base Options) int {
	return runWithOptions(context.Background(), reg, args, base)
}

// RunContextWithOptions combines RunContext and RunWithOptions.
func RunContextWithOptions(ctx context.Context, reg *registry.Registry, args []string, base Options) int {
	return runWithOptions(ctx, reg, args, base)
}

func runWithOptions(ctx context.Context, reg *registry.Registry, args []string, base Options) int {
	transport, opts, err := parseArgs(args)
	// 预设（--xyz.*）作为默认，命令行 flag 优先。
	opts.mergeDefaults(base)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mcp:", err)
		fmt.Fprintln(os.Stderr, langx.T("mcp.usage"))
		return 2
	}
	if transport != "stdio" && transport != "sse" && transport != "http" {
		fmt.Fprintf(os.Stderr, "mcp: %s\n", langx.Tf("mcp.err_unknown_transport", transport))
		return 2
	}
	if err := validateTransportVersions(transport, opts); err != nil {
		fmt.Fprintln(os.Stderr, "mcp:", err)
		return 2
	}
	server, err := Server(reg, opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mcp:", err)
		return 2
	}
	allowed := versionSet(opts.Versions)
	switch transport {
	case "stdio":
		if len(opts.BearerTokens) > 0 {
			logx.Warnf("%s", langx.T("warn.bearer_stdio"))
		}
		t := versionFilterTransport{Transport: &sdkmcp.StdioTransport{}, allowed: allowed}
		if err := server.Run(ctx, t); err != nil {
			// 客户端断开（EOF/关停）是正常退出，不是失败。
			if strings.Contains(err.Error(), "server is closing") || stderrors.Is(err, io.EOF) {
				return 0
			}
			fmt.Fprintln(os.Stderr, "mcp:", err)
			return 1
		}
		return 0
	case "sse":
		handler := versionGate(sdkmcp.NewSSEHandler(func(*http.Request) *sdkmcp.Server { return server }, &sdkmcp.SSEOptions{}), opts.Versions, allowed)
		return serveHTTP(ctx, opts.Addr, httpapi.CORS(opts.CORSOrigins, httpapi.Bearer(opts.BearerTokens, handler)))
	case "http":
		inner := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server },
			&sdkmcp.StreamableHTTPOptions{JSONResponse: opts.JSONResponse, Stateless: opts.Stateless, SessionTimeout: opts.SessionTimeout})
		logx.Debugf("streamable HTTP: sessionTimeout=%s cors=%d stateless=%v", opts.SessionTimeout, len(opts.CORSOrigins), opts.Stateless)
		return serveHTTP(ctx, opts.Addr, httpapi.CORS(opts.CORSOrigins, httpapi.Bearer(opts.BearerTokens, versionGate(inner, opts.Versions, allowed))))
	default:
		fmt.Fprintf(os.Stderr, "mcp: unknown transport %q (want stdio|sse|http)\n", transport)
		return 2
	}
}

func HTTPHandler(reg *registry.Registry, opts Options) (http.Handler, error) {
	server, err := Server(reg, opts)
	if err != nil {
		return nil, err
	}
	h := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server },
		&sdkmcp.StreamableHTTPOptions{JSONResponse: opts.JSONResponse, Stateless: opts.Stateless, SessionTimeout: opts.SessionTimeout})
	return httpapi.CORS(opts.CORSOrigins, httpapi.Bearer(opts.BearerTokens, h)), nil
}

// mergeDefaults 把预设选项（如根派发器传入的 --xyz.bearer）作为默认值；
// 命令行 flag 已设置的字段优先，布尔项取或（未提供关闭旗标）。
