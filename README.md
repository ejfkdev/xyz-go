# xyz-go — One definition, three interfaces

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev/dl/)
[![MCP Protocol](https://img.shields.io/badge/MCP-2024%E2%80%932026--07--28-0764e0?style=flat)](https://modelcontextprotocol.io/specification/2026-07-28)
[![Dependencies](https://img.shields.io/badge/core_packages-zero_deps-2ea44f?style=flat)](#dependency-policy--binary-size)
[![中文](https://img.shields.io/badge/中文-README.zh--CN.md-red?style=flat)](README.zh-CN.md)

A command toolkit for Go: **define a command once** (argument struct + validation + per-interface details) and one binary automatically speaks three interfaces — **CLI subcommands**, an **HTTP REST service** (with an OpenAPI document), and an **MCP tool server** (official Go SDK). The library decides the running mode by itself.

```go
import "github.com/ejfkdev/xyz-go" // package name is xyz; the import binds as xyz

func main() {
	xyz.Define("user.add", addUser).
		Summary("Add a user").
		CLI(xyz.CliHints{Usage: "add <name>", Fields: map[string]xyz.CliFieldHint{
			"age": {Shorthand: "a", Default: 20},
		}}).
		HTTP(xyz.HTTPHints{Method: "POST", Path: "/users/{name}"}).
		MCP(xyz.MCPHints{Annotations: []string{"write"}}).
		Also(xyz.Define("math.sum", sum).Summary("Sum two numbers")).
		Run() // register + dispatch + exit — that's the whole program
}
```

```console
$ example user add bob -a 25          # CLI: subcommands + shorthand flags
id    1
name  bob
age   25

$ example user list                   # []struct renders as an aligned table
id  name   age
--  -----  ---
1   alice  18

$ curl -s -X POST localhost:8080/users/alice -d '{"age":9}'
{"id":2,"name":"alice","age":9}

$ example mcp stdio                   # MCP: every command becomes a tool (stdio/SSE/streamable HTTP)
```

## Features

- **One definition, zero boilerplate**: the whole `main` is a single `xyz.Define(...)...Run()` chain — no `os.Exit`, no explicit registry, no dispatch switch.
- **One pipeline across all three interfaces**: CLI (strings), HTTP (JSON) and MCP arguments are normalized into one input shape and flow through the same decode → defaults → validate → handler path, so behavior never drifts.
- **Per-interface fine-tuning**: shorthands, aliases, env fallbacks, binding locations, and **interface-specific default values** (two-tier layering: global tag default → per-interface override).
- **Envelope-free responses**: primitives print bare, structs align as key/value, `[]struct` becomes a table, `--json` flips to JSON; HTTP answers bare JSON; MCP returns both `structuredContent` and human `textContent`.
- **One error taxonomy**: a single `errs.New(errs.KindNotFound, ...)` drives the CLI exit code, the HTTP status code and the MCP error code simultaneously.
- **Dependency hygiene**: the core packages (spec / registry / errors / cli / httpapi / root) have **zero third-party dependencies**; the only third-party tree is the official MCP SDK, removable wholesale with `-tags nomcp` (smallest trimmed build ≈ 3.9M).
- **Protocol versions under control**: MCP speaks the five spec revisions from 2024-11-05 to 2026-07-28; `--versions` pins the subset. Tools also carry a reflection-generated `outputSchema` (OpenAPI response schemas share the same source).
- **Production-friendly**: SIGINT/SIGTERM graceful shutdown (context flows into handlers), `/healthz` probe, gzip, CLI help with inline `(default …)`/`(env …)`/`(oneof …)` hints, and `completion bash|zsh|fish`.

## Install

```bash
go get github.com/ejfkdev/xyz-go
```

Requires Go ≥ 1.25 (dictated by the official MCP SDK's go directive). A complete runnable showcase lives in [cmd/example](cmd/example/main.go) (11 commands covering the full API surface); [cmd/tour](cmd/tour/main.go) walks through the internals.

## One definition: the argument struct

Tags on the argument struct form the **shared contract** across every interface (wire names, descriptions, defaults, required-ness, enums, validation, secrecy):

| Tag | Meaning | Example |
|---|---|---|
| `json:"name"` | Wire field name; `-` excludes it from binding & schema (still injectable via env/header by Go field name) | `json:"user_name"` |
| `desc:"..."` | Field description (CLI help and JSON Schema alike) | `desc:"username"` |
| `default:"..."` | Global default, parsed per field type, overridable per interface | `default:"18"` |
| `required:"true"` | Must be provided | `required:"true"` |
| `enum:"a,b"` | Allowed values (enforced at decode; written into schema) | `enum:"fast,slow"` |
| `validate:"..."` | Validation rules (built-in validator; see the supported set below) | `validate:"min=2,email"` |
| `secret:"true"` | Sensitive: redact in help/logs/echoes | `secret:"true"` |
| `cli:"..."` | CLI bindings: `shorthand=a`, `positional`, `hidden`, `env=VAR`, `-` | `cli:"shorthand=a,env=TOKEN"` |
| `http:"query"` | HTTP binding: `query` (**the default when unset**) / `path` / `header` / `form` / `body` | `http:"header"` |
| `httpName:"X-Key"` | HTTP wire-name override (typically a header name) | `httpName:"X-Api-Key"` |

`validate` supports: `required`, `omitempty`, `min`, `max`, `len`, `gt`, `gte`, `lt`, `lte`, `oneof`, `email` — a go-playground-compatible subset. Unsupported rules fail at **registration time**, never silently at runtime.

Type support: all scalars and named scalars (`type Port int`), `[]T`, `[]byte`, `*T`, nested structs, `time.Time`, `time.Duration`; maps, interfaces, anonymous embedding and recursive types are rejected at registration. All wired formats accept strings (CLI), JSON shapes (HTTP body) and raw JSON (MCP) with lossless conversion checks (`3.7` never silently becomes `int(3)`).

## Per-interface configuration & default layering

`CLI()/HTTP()/MCP()` on the Define chain configure command-level details and, via the `Fields` map, override tags per field (both layers merge; a zero hint field means "keep the tag"):

```go
CLI(xyz.CliHints{
	Usage:   "add <name>",             // usage line in help
	Aliases: []string{"ua", "new"},    // aliases equal subcommand names
	Fields: map[string]xyz.CliFieldHint{
		"age":   {Shorthand: "a"},        // shorthand (also available as a tag)
		"mode":  {Default: "fast"},       // CLI-only default
		"token": {EnvVar: "APP_TOKEN"},   // env fallback
	},
})
```

**Default precedence for one field** (CLI as the example):

```
explicit flag > env fallback > interface default > global tag default (Invoke fills it) > zero value
```

Mechanism: each frontend injects its own overrides (`Entry.CLIDefaults()/HTTPDefaults()/MCPDefaults()`) before calling `Invoke`, which then applies global tag defaults — one pipeline, drift-free. MCP's overrides also replace `default` in `inputSchema` (the schema is MCP's contract).

## Three modes

```
example [command] [args]          CLI: subcommand tree, shorthands/aliases/-h/-v/--json/positionals/env
example serve --addr :8080        HTTP: REST routes + /openapi.json + /mcp on the same port
example mcp stdio|sse|http        MCP: official SDK, three transports (--versions pins revisions)
example completion bash|zsh|fish  Built-in shell completion scripts
```

**CLI** (pure standard library): registry name `user.add` becomes the two-level subcommand `user add`; `-h/--help` prints per-command help (with inline `(default …)`/`(env …)`/`(oneof …)` hints), `-v/--version` prints the version (`xyz.Version`, injectable via `-ldflags "-X github.com/ejfkdev/xyz-go.Version=v1.2.3"`).

**HTTP** (pure standard library): routes come straight from `HTTPHints{Method, Path}` (`{name}` is a path parameter); fields without an `http:` tag bind from the query string by default, a JSON body merges as the argument base; the error taxonomy maps to status codes (400/401/403/404/409/500) with `{"error":"..."}` bodies; `GET /openapi.json` serves an OpenAPI 3 document from the same `InputSchema` (response schemas included); `GET /healthz` probes liveness and `Accept-Encoding: gzip` is answered transparently. Commands without HTTP hints are not routed.

**MCP** (official SDK): commands become tools; `tools/list` serves the reflection-generated `inputSchema` **and `outputSchema`**; success returns dual content — `structuredContent` (bare JSON) + `textContent` (the CLI-style rendering); failures return `isError: true` with the classified message. Supported spec revisions: `2024-11-05`, `2025-03-26`, `2025-06-18`, `2025-11-25`, `2026-07-28` (the newest is the handshake-free `server/discover` era); built-in constraints: SSE serves ≤2025-11-25 only, streamable HTTP needs `--stateless` for 2026-07-28.

### Response rendering (envelope-free)

| Return type | CLI | `--json` / HTTP / MCP structuredContent |
|---|---|---|
| `nil` / nil pointer | nothing | JSON `null` |
| `string` / `bool` / numbers | bare value, one line | bare JSON value |
| `time.Time` | RFC3339 | RFC3339 string |
| `[]scalars` | one per line | JSON array |
| `struct` | aligned `key  value` columns | JSON object |
| `[]struct` | aligned table (header + rule) | JSON array |
| `map` | sorted key/value pairs | JSON object |

## Error taxonomy

Handlers return `errs "github.com/ejfkdev/xyz-go/errors"` and one classification drives the three interfaces:

| Kind | HTTP | CLI exit | JSON-RPC / MCP |
|---|---|---|---|
| `invalid_input` (auto for decode/validation) | 400 | 2 | -32602 |
| `unauthorized` | 401 | 1 | -32010 |
| `forbidden` | 403 | 1 | -32011 |
| `not_found` | 404 | 1 | -32001 |
| `conflict` | 409 | 1 | -32009 |
| `unavailable` | 503 | 1 | -32603 |
| unclassified (falls back to `internal`) | 500 | 1 | -32603 |

## Configuration: mode words & capability switches

All dispatch configuration lives in `xyz.Config`; the zero value means defaults. Chain style uses `.Configure(cfg)`, functional style uses `MainConfig`/`RunConfig`:

```go
xyz.MainConfig(xyz.Config{
	// serve/mcp/help are reserved words; renaming releases the old words for use as commands
	Modes: xyz.ModeWords{Serve: "httpd", MCP: "protocol", Help: "assist"},
	// disabling a channel removes only its runtime path: mcp/serve/help/-v always survive
	Capabilities: xyz.Capabilities{NoCLI: true, NoMCP: true, NoHTTP: true},
})
```

With `NoCLI`, user subcommands disappear but `mcp stdio`, `serve`, `help` and `-v` keep working (the overview annotates "disabled" and hides the command table); the disabled channel's `CLI()/HTTP()/MCP()` configuration still compiles and runs — it simply stops being consumed by a frontend that is switched off. A registry with zero registered commands is a silent no-op (exit 0).

## Built-in configuration (`--xyz.*` and `Config` fields)

The library's own settings live in `xyz.Config` fields and the `--xyz.*` command-line namespace; precedence: **mode-local flag > global `--xyz.*` / code Config > library defaults**. Inside `serve`/`mcp` the mode word _is_ the namespace, so built-ins use bare names (`--bearer`, `--addr`, `--cors`, …), and the prefixed `--xyz.*` forms work anywhere on the command line. Renaming the mode words migrates the namespace with them.

| Parameter | Code field | Meaning |
|---|---|---|
| `--bearer=tok1,tok2` (or `--bearer tok`) | `Config.BearerTokens` | Bearer verification for **serve REST** and **MCP http/sse**: `Authorization: Bearer <tok>` must hit one of the tokens, else 401 + `{"error":"unauthorized"}`; empty = no auth. stdio is a local process and unaffected (a note is logged) |
| `--addr=:8080` | `Config.Addr` | Default listen address for serve and mcp(http/sse) |
| `--log-level=debug` (or `--xyz.log-level`) | `Config.LogLevel` | Library diagnostics to stderr (`xyz[level]:` prefix): `debug`/`info`/`warn`/`error`, default `info`. Command results and usage errors are unaffected |
| `--timeout=45s` | `Config.Timeout` | serve read/write/idle timeouts; 0 keeps only the 10s header timeout |
| `--tls-cert/--tls-key` | `Config.CertFile/KeyFile` | serve switches to TLS when both are given |
| `--cors=https://a,b` (or `*`) | `Config.CORSOrigins` | CORS allowlist for serve and MCP http/sse; OPTIONS preflights answer before auth (browser preflights carry no credentials) |
| `--session-timeout=30m` (mcp only) | `mcp.Options.SessionTimeout` | Idle-session expiry for streamable HTTP (the SDK's SessionTimeout) |

```bash
example serve --bearer=s3cret                        # REST/openapi/mcp all require credentials
example mcp http --addr :9000 --bearer a,b           # standalone MCP, same scheme
example mcp http --xyz.bearer a,b                    # prefixed form is equivalent
```

Waiting list (kept out to stay minimal — each lands in one iteration when asked): log file rotation, basic rate limiting.

## Embedding & multiple registries

Besides the singleton chain, the parameterized pure functions (which return the exit code without exiting) serve embedding, tests and multi-registry setups:

```go
reg := registry.New()
spec.Define("user.add", addUser).Summary("...").Register(reg) // explicit path
os.Exit(xyz.Run(reg, os.Args[1:]))                            // for deferred cleanup in main

srv := mcp.Server(reg, mcp.Options{Versions: []string{mcp.ProtocolV2026_07_28}})
h, _ := httpapi.Handler(reg)      // mount all HTTP routes (healthz & openapi included)
addOne, _ := spec.Define(...).Register(reg) // 或 httpapi.HandlerFor(entry) 挂单条命令到任意路由器
app, _ := cli.NewWithOptions(reg, cli.Options{Out: w, ErrOut: ew}) // 或 cli.New(reg) + app.SetOutput / app.Use(中间件)

mcp.RunContext(ctx, reg, args)
cli.RunContext(ctx, reg, args)
```

Already on Cobra / Gin / Echo / chi? See the [migration guide](docs/adapters.md) with runnable examples in [`examples/cobra`](examples/cobra) and [`examples/gin`](examples/gin) — three coexistence levels (replace the frontend, mount per-command handlers, or reuse middleware pieces), all without touching the core.

## Dependency policy & binary size

- **Zero third-party dependencies in the core packages**: `go.mod` has exactly one direct dependency tree — the official MCP SDK (`github.com/modelcontextprotocol/go-sdk`). CLI and HTTP are pure standard library; no mimetype, no locale packs.
- **Trim any channel via build tags** (any combination):

| Build tags | Channels | Size (`-s -w -trimpath`, cmd/example measured) |
|---|---|---|
| (default) | CLI + HTTP + MCP | 8.3M |
| `-tags nomcp` | CLI + HTTP | 6.5M |
| `-tags nocli` | HTTP + MCP | 8.3M |
| `-tags nohttp` | CLI + MCP | 7.9M |
| `-tags nomcp,nohttp` | CLI only | 4.1M |
| `-tags nocli,nomcp,nohttp` | embedding only | 3.9M |

```bash
go build -ldflags "-s -w" -trimpath -o example ./cmd/example
go build -tags nomcp,nohttp -ldflags "-s -w" -trimpath -o example ./cmd/example
```

Size breakdown (stripped): Go's runtime floor ≈1.1M (self-contained static linking — every Go binary pays this) + the library itself (fmt/json/reflection, plus the example's own code) ≈2.8M + HTTP (net/http, TLS, gzip) ≈2M + the MCP SDK ≈3–5M (grows with command count). A trimmed channel disappears as a block; invoking it answers a clear error and exits 1.

## Package layout

| Package | Responsibility | Dependencies |
|---|---|---|
| `/` (root package `xyz`) | fluent Builder, mode dispatch, capability switches, built-in parameters (`--xyz.*`), version | stdlib |
| `/spec` | generic definition, field reflection, decode pipeline, validation, JSON Schema | stdlib |
| `/registry` | command table: registration, conflict checks, the default singleton | stdlib |
| `/errors` | error taxonomy and three-interface mappings | stdlib |
| `/cli` | CLI frontend: command tree, flag parsing, help, completion | stdlib |
| `/httpapi` | HTTP frontend: routing, binding, middleware, openapi.json | stdlib |
| `/logx` | leveled diagnostics to stderr (`xyz[level]:` prefix) | stdlib |
| `/mcp` | MCP frontend: three transports, protocol versions | official SDK |
| `/cmd/example`, `/cmd/tour` | showcase & internal tour | — |

## Design principles

1. **Zero boilerplate by default, explicit paths always available**: the singleton chain is the main entry; registry-parameterized pure functions are the backdoor for embedding and tests — both share one dispatch and invoke pipeline.
2. **Fail at registration**: bad names/types/tags/route conflicts/unsupported validation rules surface at startup, never at runtime.
3. **Configuration is data; frontends are consumers**: `CLI()/HTTP()/MCP()` only store metadata, so build tags and capability switches never break compilation.
4. **Envelope-free, natural forms**: machines read JSON, humans read tables, from one return value.
5. **The shell is uncuttable**: `help`/`-v`/mode words/`completion` work in every combination.
6. **The mode word is the namespace**: built-ins in `serve`/`mcp` use bare names; `--xyz.*` works globally; library messages never hardcode mode words — renaming migrates everything.
7. **Cancellation flows everywhere**: the dispatcher owns a signal context that reaches CLI/HTTP/MCP handlers; HTTP drains in-flight requests before exiting.

## Development

```bash
go vet ./... && go test ./...                    # unit tests across all 8 library packages
go test -tags "nocli nomcp nohttp" ./...         # every build-tag variant also passes
go run ./cmd/example                             # full showcase
go run ./cmd/tour                                # internal-walkthrough tour
```

Output contract: command results go to stdout; errors and diagnostics go to stderr (diagnostics carry the `xyz[level]:` prefix, level via `--log-level`); under the stdio transport stdout is reserved for protocol frames. The `context.Context` passed to handlers is canceled on SIGINT/SIGTERM (the HTTP server drains in-flight requests first).

### Release

```bash
git tag v0.1.0 && git push origin v0.1.0   # consumers: go get github.com/ejfkdev/xyz-go@v0.1.0
```

> 📄 Also available: [中文文档](README.zh-CN.md)