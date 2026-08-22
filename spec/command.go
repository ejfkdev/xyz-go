package spec

import (
	"context"
	"time"
)

// Handler implements one command: it receives the fully decoded, defaulted
// and validated argument struct and returns an arbitrary result value. The
// result type R may itself be a pointer (return nil on "no result") or any
// other value.
type Handler[T, R any] func(ctx context.Context, in *T) (R, error)

// Command is the builder for one command definition. Obtain one with Define
// and chain transport hints before calling Entry or Register.
type Command[T, R any] struct {
	name        string
	summary     string
	description string
	handler     Handler[T, R]

	cli  CliHints
	http HTTPHints
	mcp  MCPHints
}

// CliHints is command-level configuration for the CLI frontend. Field-level
// bindings can live in two places: on the argument struct's cli tags, or
// here in Fields (keyed by the field's JSON or Go name). A Fields entry is
// merged over the tag binding — zero-valued hint fields keep the tag value,
// so you can set the shorthand in the tag and only override the default in
// the builder.
type CliHints struct {
	Usage   string   // one-line invocation, e.g. "add <name>"
	Aliases []string // alternative subcommand spellings
	Hidden  bool     // omit from help listings
	// Default makes this command the default child of its parent node: when
	// the first argument is not a registered command segment (and is not a
	// flag), the whole argument list is forwarded to it (udf image.tar with
	// default extract == udf extract image.tar).
	Default bool
	// Skip 从 CLI 通道整体移除该命令：不建子命令、别名不生效、不出现在
	// completion。与 Hidden 的区别：Hidden 只是帮助列表藏起、仍可执行。
	Skip bool
	// Daemon 声明「长驻命令」：handler 阻塞到 ctx 取消再返回。语义：
	// 隐含 HTTP/MCP 双 Skip（通道层面不消费）；执行时不渲染返回值
	// （handler 的 error 照常分类映射）；ctx 取消即优雅关停、退出 0。
	Daemon bool
	// Before/After 是 -h 帮助的自定义文本块：分别插在帮助最前（description
	// 之前）与最后（Global Flags 之后）。原样输出（多行、缩进自控；结尾换行
	// 归一）。空 = 不插入。
	Before string
	After  string
	Fields map[string]CliFieldHint // per-field CLI configuration
}

// CliFieldHint is Define-time per-field configuration for the CLI frontend.
type CliFieldHint struct {
	Shorthand  string // single character, e.g. "a"
	Positional bool   // consume the next positional argument
	Hidden     bool   // exclude from --help listings
	Skip       bool   // invisible to the CLI frontend (tag equivalent: cli:"-")
	EnvVar     string // fall back to this environment variable when unset
	Default    any    // CLI-only default; overrides the global default tag for the CLI frontend
}

// HTTPHints is command-level configuration for the HTTP frontend. Field-level
// binding locations can live on the http tags or here in Fields, with the
// same merge semantics as CliHints.
type HTTPHints struct {
	Method  string        // GET, POST, ...
	Path    string        // route pattern, e.g. "/users"
	Timeout time.Duration // per-request override; 0 keeps the frontend default
	// Skip 从 HTTP 通道整体移除该命令：不注册路由、不进 /openapi.json。
	// 比「不给 Method/Path」更声明式（适合只适合 CLI 的命令）。
	Skip   bool
	Fields map[string]HTTPFieldHint // per-field HTTP configuration
}

// HTTPFieldHint is Define-time per-field configuration for the HTTP frontend.
type HTTPFieldHint struct {
	Location string // "", query, path, header, form, body
	Name     string // wire name override, e.g. "X-Custom-Header"
	Default  any    // HTTP-only default; overrides the global default tag for the HTTP frontend
}

// MCPHints is command-level configuration for the MCP frontend. Field-level
// overrides can live here in Fields, with the same merge semantics as
// CliHints.
type MCPHints struct {
	Annotations []string                // e.g. "read", "write", "destructive"
	Fields      map[string]MCPFieldHint // per-field MCP configuration
	// Skip 从 MCP 通道整体移除该命令：不成为工具（负担重的守护/本地命令
	// 用；配合 CLI.Skip/HTTP.Skip 自由组合每个命令的通道面）。
	Skip bool
}

// MCPFieldHint is Define-time per-field configuration for the MCP frontend.
type MCPFieldHint struct {
	Default any // MCP-only default; also replaces the global default in the generated input schema
}

// Define starts a command definition. name must match
// ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$ (MCP tool-name compatible); the check
// runs in Entry, so Define itself never fails.
func Define[T, R any](name string, h Handler[T, R]) *Command[T, R] {
	return &Command[T, R]{name: name, handler: h}
}

// Summary sets the one-line description used by help texts and the MCP tool
// list.
func (c *Command[T, R]) Summary(s string) *Command[T, R] {
	c.summary = s
	return c
}

// Description sets a longer explanation shown in full help and tool docs.
func (c *Command[T, R]) Description(s string) *Command[T, R] {
	c.description = s
	return c
}

// CLI attaches command-level CLI options.
func (c *Command[T, R]) CLI(h CliHints) *Command[T, R] {
	c.cli = h
	return c
}

// HTTP attaches command-level HTTP options.
func (c *Command[T, R]) HTTP(h HTTPHints) *Command[T, R] {
	c.http = h
	return c
}

// MCP attaches command-level MCP options.
func (c *Command[T, R]) MCP(h MCPHints) *Command[T, R] {
	c.mcp = h
	return c
}

// Name returns the command's registered name.
func (c *Command[T, R]) Name() string { return c.name }
