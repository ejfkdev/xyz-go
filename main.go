// Package xyz wires one registry to every frontend. Main reads the process
// arguments, decides the running mode by itself, and exits with the code the
// dispatch produced. The whole program can be a single define-chain:
//
//	func main() {
//		xyz.Define("user.add", addUser).
//			Summary("创建用户").
//			CLI(xyz.CliHints{...}).
//			MCP(xyz.MCPHints{...}).
//			Also(xyz.Define("math.sum", sum).Summary("求和")).
//			Run()
//	}
//
// Run (and Main / MainConfig) dispatch the process-wide default registry
// and call os.Exit internally, so deferred cleanups written in main cannot
// run after them. When you need defer-based cleanup, a custom exit code,
// several registries, or want to embed the dispatcher, use Run / RunConfig
// with an explicit registry, which return the exit code instead:
//
//	func main() {
//		reg := registry.New()
//		// ... spec.Define(...).Register(reg) ...
//		defer cleanup()
//		os.Exit(xyz.Run(reg, os.Args[1:]))
//	}
//
// A registry with no registered commands is a silent no-op: the dispatcher
// exits 0 without printing anything.
//
// Mode detection:
//
//	<app> [命令] ...          -> CLI frontend (subcommands, flags, positionals, -h / -v)
//	<app> mcp stdio|sse|http  -> MCP frontend (official SDK; --versions pins protocol versions)
//	<app> serve [--addr ...]  -> HTTP frontend (REST + /openapi.json + /mcp)
//	<app> (no args) | help    -> overview listing modes and commands
//
// The mode keywords default to "serve", "mcp" and "help" and are reserved
// top-level names; both the keywords and the reserved-name checks follow
// the Modes configuration in RunConfig, so they can be renamed. Dispatch
// lives in main.go, configuration types in config.go, built-in parameter
// parsing in builtins.go, overview rendering in overview.go and the fluent
// builder in builder.go.
package xyz

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ejfkdev/xyz-go/langx"
	"github.com/ejfkdev/xyz-go/logx"
	"github.com/ejfkdev/xyz-go/registry"
)

// Main registers any fully-built command definitions passed to it (from
// xyz.Define), dispatches the process-wide default registry on the process
// arguments, and exits with the resulting exit code. Zero arguments means
// "definitions already registered via RegisterDefault, just dispatch".
// Use Run/RunConfig instead when you need the code yourself (embedding,
// testing, deferred cleanups) or want an explicit registry.
func Main(cmds ...Definable) {
	if len(cmds) > 0 {
		for _, cmd := range cmds {
			if _, err := cmd.Register(registry.Default); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
		}
	}
	os.Exit(Run(registry.Default, os.Args[1:]))
}

// MainConfig is Main with a custom configuration (e.g. renamed mode words).
func MainConfig(cfg Config) {
	os.Exit(RunConfig(registry.Default, os.Args[1:], cfg))
}

// Run is Main with explicit arguments and default configuration, returning
// the exit code without exiting the process.
func Run(reg *registry.Registry, args []string) int {
	return RunConfig(reg, args, Config{})
}

// RunConfig is Run with a custom configuration (renamed mode words, channel
// capabilities).
func RunConfig(reg *registry.Registry, args []string, cfg Config) int {
	code, _ := runInternal(reg, args, cfg, false)
	return code
}

// TryRunConfig 是可组合派发：与 RunConfig 同管线，但当参数进入 CLI 模式
// 且首段不是任何已注册命令段/别名（也不是 flag）时，不打印任何东西、
// 返回 (0, false)，由宿主路由其余参数（handled=false 即「未命中」）。
// 其余路径（总览/版本/模式词/已知命令）行为与 RunConfig 完全一致。
func TryRunConfig(reg *registry.Registry, args []string, cfg Config) (int, bool) {
	return runInternal(reg, args, cfg, true)
}

// TryRun 是 TryRunConfig 的默认配置形态。
func TryRun(reg *registry.Registry, args []string) (int, bool) {
	return TryRunConfig(reg, args, Config{})
}

// cliKnownTop 判断首段是否命中 CLI 树（已知命令段、别名或 default 子命令）。
func cliKnownTop(reg *registry.Registry, first string) bool {
	if strings.HasPrefix(first, "-") {
		return true // flag/帮助等交给 CLI 自身路径报错或展示
	}
	for _, e := range reg.All() {
		if e.CLI.Skip {
			continue
		}
		if top, _, _ := strings.Cut(e.Name, "."); top == first {
			return true
		}
		for _, al := range e.CLI.Aliases {
			if al == first {
				return true
			}
		}
		if e.CLI.Default && len(e.Name) > 0 {
			return true // 默认子命令吞掉未知名
		}
	}
	return false
}

func runInternal(reg *registry.Registry, args []string, cfg Config, composable bool) (int, bool) {
	m, err := resolveModes(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2, true
	}
	if reg == nil {
		fmt.Fprintln(os.Stderr, "xyz: nil registry")
		return 2, true
	}
	// 用户命令顶层段与模式词冲突时，裸词让位给用户命令，内建模式仍走
	// xyz.<词>（xyz-spec §13.1）——不再像旧版那样注册期报错拒绝。
	shadowed := shadowedModes(reg, m)
	// 没有任何已注册命令：什么都不做，静默退出 0。
	if len(reg.Names()) == 0 {
		return 0, true
	}
	// 壳能力：-v/--version 由根派发器管，任何能力组合下都可用。
	for _, a := range args {
		if a == "--" {
			break // "--" 之后是位置参数，不再识别 -v
		}
		if a == "-v" || a == "--version" {
			fmt.Fprintf(os.Stdout, "%s version %s\n", cfg.resolvedName(), cfg.resolvedVersion())
			return 0, true
		}
	}
	// 内置参数 --xyz.*：剥离开分发给各前端（帮助/版本不受影响）。
	args, err = stripXYZFlags(args, &cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xyz:", err)
		return 2, true
	}
	if cfg.LogLevel != logx.LevelUnset {
		logx.SetLevel(cfg.LogLevel)
	}
	// 界面语言：--xyz.lang（已写回 cfg）> Config.Lang > 环境检测 > 英文。
	lang := langx.En
	if cfg.Lang != "" {
		l, ok := langx.Parse(cfg.Lang)
		if !ok {
			fmt.Fprintf(os.Stderr, "xyz: invalid --xyz.lang %q (want en|zh-CN)\n", cfg.Lang)
			return 2, true
		}
		lang = l
	} else {
		lang = langx.Detect()
	}
	langx.Set(lang, cfg.Translations[lang.String()])

	// 根总览：无参数，或根级 -h/--help。
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		printOverview(os.Stdout, reg, m, shadowed, cfg)
		return 0, true
	}
	logx.Debugf("dispatch: lead=%s addr=%s tokens=%d timeout=%s cors=%d",
		args[0], cfg.Addr, len(args), cfg.Timeout, len(cfg.CORSOrigins))

	// 模式派发：xyz.<词> 恒命中；裸词仅未被用户命令遮蔽时命中。
	if kind := matchMode(args[0], m, shadowed); kind != modeNone {
		rest := args[1:]
		if kind == modeHelp {
			return runHelp(reg, rest, m, shadowed, cfg), true
		}
		// serve/http/mcp 的 -h/--help：打印模式帮助而非起服务。
		if hasHelpFlag(rest) {
			return printModeHelp(kind, m, cfg), true
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		switch kind {
		case modeServe:
			if cfg.Capabilities.NoHTTP {
				logx.Warnf("%s", langx.Tf("warn.mode_disabled", m.serve, "HTTP"))
				return 1, true
			}
			return runServe(ctx, reg, rest, cfg, !cfg.Capabilities.NoMCP), true
		case modeHTTP:
			if cfg.Capabilities.NoHTTP {
				logx.Warnf("%s", langx.Tf("warn.mode_disabled", m.http, "HTTP"))
				return 1, true
			}
			return runServe(ctx, reg, rest, cfg, false), true // 单独 HTTP：不挂 /mcp
		case modeMCP:
			if cfg.Capabilities.NoMCP {
				logx.Warnf("%s", langx.Tf("warn.mode_disabled", m.mcp, "MCP"))
				return 1, true
			}
			return runMCP(ctx, reg, rest, cfg), true
		}
	}

	// CLI 模式（默认）。
	if cfg.Capabilities.NoCLI {
		logx.Warnf("%s", langx.Tf("warn.no_cli", m.mcp, m.serve))
		return 1, true
	}
	if composable && !cliKnownTop(reg, args[0]) {
		// 宿主兜底：静默交还，不做任何输出。
		return 0, false
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runCLI(ctx, reg, args, cfg), true
}

// modes 是解析后的四个模式词。
type modes struct{ serve, http, mcp, help string }

// modeKind 标识命中的内建模式。
type modeKind int

const (
	modeNone modeKind = iota
	modeServe
	modeHTTP
	modeMCP
	modeHelp
)

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// resolveModes defaults and validates the mode keywords: they must be plain
// words (no leading dash, no whitespace) and pairwise distinct.
func resolveModes(cfg Config) (modes, error) {
	m := modes{
		serve: orDefault(cfg.Modes.Serve, "serve"),
		http:  orDefault(cfg.Modes.HTTP, "http"),
		mcp:   orDefault(cfg.Modes.MCP, "mcp"),
		help:  orDefault(cfg.Modes.Help, "help"),
	}
	seen := map[string]bool{}
	for _, w := range []string{m.serve, m.http, m.mcp, m.help} {
		if strings.HasPrefix(w, "-") || strings.ContainsAny(w, " \t") {
			return m, fmt.Errorf("xyz: invalid mode word %q (no leading dash, no whitespace)", w)
		}
		if seen[w] {
			return m, fmt.Errorf("xyz: mode words must be pairwise distinct (duplicate %q)", w)
		}
		seen[w] = true
	}
	return m, nil
}

// allWords 返回四个模式词（顺序：serve/http/mcp/help）。
func (m modes) allWords() []string { return []string{m.serve, m.http, m.mcp, m.help} }

// shadowedModes 计算哪些模式词被用户命令的顶层段遮蔽（遮蔽时裸词让位给
// 用户命令，内建模式仅经 xyz.<词> 可达）。CLI-Skip 的命令不参与遮蔽。
func shadowedModes(reg *registry.Registry, m modes) map[string]bool {
	tops := map[string]bool{}
	for _, name := range reg.Names() {
		if e, _ := reg.Get(name); e != nil && e.CLI.Skip {
			continue
		}
		top, _, _ := strings.Cut(name, ".")
		tops[top] = true
	}
	out := map[string]bool{}
	for _, w := range m.allWords() {
		out[w] = tops[w]
	}
	return out
}

// matchMode 把首个 token 映射到内建模式：namespaced "xyz.<词>" 恒命中；
// 裸词仅在未被用户命令遮蔽时命中。
func matchMode(token string, m modes, shadowed map[string]bool) modeKind {
	switch token {
	case "xyz." + m.serve:
		return modeServe
	case "xyz." + m.http:
		return modeHTTP
	case "xyz." + m.mcp:
		return modeMCP
	case "xyz." + m.help:
		return modeHelp
	}
	switch {
	case token == m.serve && !shadowed[m.serve]:
		return modeServe
	case token == m.http && !shadowed[m.http]:
		return modeHTTP
	case token == m.mcp && !shadowed[m.mcp]:
		return modeMCP
	case token == m.help && !shadowed[m.help]:
		return modeHelp
	}
	return modeNone
}

// hasHelpFlag 报告 args 中（"--" 之前）是否出现 -h/--help。
func hasHelpFlag(args []string) bool {
	for _, a := range args {
		if a == "--" {
			break
		}
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

// runHelp 实现 `help` 模式：裸 help → 总览；help <模式> → 模式帮助；
// help <命令路径> → 该命令的详细帮助（委托 CLI 的 -h，点分或空格分隔皆可）。
func runHelp(reg *registry.Registry, rest []string, m modes, shadowed map[string]bool, cfg Config) int {
	if len(rest) == 0 {
		printOverview(os.Stdout, reg, m, shadowed, cfg)
		return 0
	}
	if kind := matchMode(rest[0], m, shadowed); kind != modeNone {
		if kind == modeHelp {
			printOverview(os.Stdout, reg, m, shadowed, cfg)
			return 0
		}
		return printModeHelp(kind, m, cfg)
	}
	// 命令路径：把每个 token 再按 "." 拆分（help user.add 与 help user add 等价），
	// 追加 -h 交给 CLI 前端打印该节点的详细帮助。
	var path []string
	for _, r := range rest {
		path = append(path, strings.Split(r, ".")...)
	}
	if cfg.Capabilities.NoCLI {
		logx.Warnf("%s", langx.Tf("warn.no_cli", m.mcp, m.serve))
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runCLI(ctx, reg, append(path, "-h"), cfg)
}

// printModeHelp 打印某个模式的帮助（用途 + 旗标），不起服务。
func printModeHelp(kind modeKind, m modes, cfg Config) int {
	switch kind {
	case modeServe:
		fmt.Fprintln(os.Stdout, langx.Tf("mode_help.serve", m.serve))
	case modeHTTP:
		fmt.Fprintln(os.Stdout, langx.Tf("mode_help.http", m.http))
	case modeMCP:
		fmt.Fprintln(os.Stdout, langx.Tf("mode_help.mcp", m.mcp))
	case modeHelp:
		fmt.Fprintln(os.Stdout, langx.Tf("mode_help.help", m.help))
	}
	return 0
}
