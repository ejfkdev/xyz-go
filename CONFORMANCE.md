# Conformance — xyz-go

Specification target: [xyz-spec](https://github.com/ejfkdev/xyz-spec) **v0.4.0**.

Status: **conformant (baseline anchor)** — xyz-go is one of the two reference
implementations the specification was written from; where ambiguous, Go
behaviour is normative.

Deviations register: one open — A.52 tagged unions (§4.7), registered as
D-go-01 in [xyz-spec/deviations.md](https://github.com/ejfkdev/xyz-spec/blob/main/deviations.md)
(Go has no language-native enum arguments; semantics now settled by the
Rust implementation, Go implementation planned against that fixture).

## Checklist

Every Class A item (conformance.md) is implemented and covered by the
following evidence:

| Evidence | Covers |
|---|---|
| `go test ./...` (default tags) | A.1–A.42 pipeline, taxonomy, rendering, dispatcher semantics |
| `block` package tests + `TestCLIBlockProjection` / `TestMCPBlockResult` / `TestHTTPBlockEnvelopePassThrough` | A.53 content-block results (§12.7): envelope detection, CLI projection, MCP verbatim `Content` + envelope `structuredContent`, HTTP pass-through |
| daemon suite (`Daemon` marker tests) | A.50 daemon commands (§4.5a): CLI-only consumption, no rendering, graceful ctx exit |
| MCP tool-name suite (`MCPHints.Name` tests) | A.51 tool-name override (§12.4a): upgrade-only name advertised and accepted, §3.1 grammar enforced |
| `go test -tags "nocli nomcp nohttp" ./...` and the six build-tag combinations in `.github/workflows/test.yml` | A.38–A.39 trim invariants, four-way feature matrix |
| `cmd/example` (11 commands) | showcase fixture §3.1, invocation matrix §3.2 |
| `cmd/tour`, `docs/adapters.md`, `examples/cobra`, `examples/gin` | A.41 embedding surfaces, §15.2 documentation |

Golden outputs (conformance.md §3.3) were diff-verified byte-for-byte
against the Go binary and cross-checked against xyz-rust where marked
byte-exact (`file hash`, `math sum`, `math div`, `/healthz`, the error
lines' exit codes).

## Showcase evidence

The fixture program runs with `go run ./cmd/example`; all commands of the
§3.2 matrix behave as specified, including `search query --query golang`
(CLI default k=25), the `--q`-is-an-unknown-flag behaviour now pinned by
the golden scenarios, and the default-subcommand forwarding of §10.1
(`TestCLIDefaultSubcommandForwardsAllArgs` in cli/cli_test.go), plus the
custom help blocks of §10.4/§13.2 (`TestPrintOverviewHelpBlocks` in
main_test.go, `TestCLIHelpBlocks` in cli/cli_test.go) and the language
catalog of §15.5 (`langx/langx_test.go`, `TestOverviewLanguage` in
main_test.go), plus §4.5a/§13.9/§6.1/§10.4 (channel
switches, TryRun, defaults) and the v0.3.1 rulings (`TestCLIDaemonCommandRunsUntilCancel`,
`TestDaemonMarkerExcludesChannelsCompact` in cli/cli_test.go,
`TestDaemonExcludesHTTPAndMCP` in mcp/mcp_test.go,
`TestBareFlagPassthroughDefaults` in main_test.go and mcp/args_test.go)
(`TestCLIChannelSkipAndTypes`, `TestCLIDaemonCommandRunsUntilCancel` in
cli/cli_test.go; `TestTryRunComposability`, `TestChannelDefaultsFlag` in
main_test.go).

## Deviations

None. See
[deviations.md](https://github.com/ejfkdev/xyz-spec/blob/main/deviations.md)
for the register and xyz-rust's entries.