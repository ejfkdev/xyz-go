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
	"path/filepath"
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
	serve, mcpWord, helpWord, err := resolveModes(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2, true
	}
	if err := checkReserved(reg, serve, mcpWord, helpWord); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2, true
	}
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
			fmt.Fprintf(os.Stdout, "%s version %s\n", filepath.Base(os.Args[0]), Version)
			return 0, true
		}
	}
	// 内置参数 --xyz.*：剥离开分发给各前端（帮助/版本不受影响）。
	var err2 error
	args, err2 = stripXYZFlags(args, &cfg)
	if err2 != nil {
		fmt.Fprintln(os.Stderr, "xyz:", err2)
		return 2, true
	}
	if cfg.LogLevel != logx.LevelUnset {
		logx.SetLevel(cfg.LogLevel)
	}
	// 界面语言：--xyz.lang（已写回 cfg）> Config.Lang > 环境检测 > 英文。
	lang := langx.En
	if cfg.Lang != "" {
		if l, ok := langx.Parse(cfg.Lang); !ok {
			fmt.Fprintf(os.Stderr, "xyz: invalid --xyz.lang %q (want en|zh-CN)\n", cfg.Lang)
			return 2, true
		} else {
			lang = l
		}
	} else {
		lang = langx.Detect()
	}
	langKey := langx.En.String()
	switch lang {
	case langx.ZhCn:
		langKey = "zh-CN"
	}
	langx.Set(lang, cfg.Translations[langKey])
	if len(args) == 0 || args[0] == helpWord || args[0] == "--help" || args[0] == "-h" {
		printOverview(os.Stdout, reg, serve, mcpWord, cfg.Capabilities, cfg.HelpBefore, cfg.HelpAfter)
		return 0, true
	}
	// 优雅关停：信号取消的 ctx 贯穿 CLI/HTTP/MCP，长任务可在退出前排空。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logx.Debugf("dispatch: mode word=%s addr=%s tokens=%d timeout=%s cors=%d",
		args[0], cfg.Addr, len(cfg.BearerTokens), cfg.Timeout, len(cfg.CORSOrigins))
	switch args[0] {
	case serve:
		if cfg.Capabilities.NoHTTP {
			logx.Warnf("%s", langx.Tf("warn.mode_disabled", serve, "HTTP"))
			return 1, true
		}
		return runServe(ctx, reg, args[1:], cfg), true
	case mcpWord:
		if cfg.Capabilities.NoMCP {
			logx.Warnf("%s", langx.Tf("warn.mode_disabled", mcpWord, "MCP"))
			return 1, true
		}
		return runMCP(ctx, reg, args[1:], cfg), true
	default:
		if cfg.Capabilities.NoCLI {
			logx.Warnf("%s", langx.Tf("warn.no_cli", mcpWord, serve))
			return 1, true
		}
		if composable && len(args) > 0 && !cliKnownTop(reg, args[0]) {
			// 宿主兜底：静默交还，不做任何输出。
			return 0, false
		}
		return runCLI(ctx, reg, args), true
	}
}

// resolveModes defaults and validates the mode keywords: they must be
// plain words (no leading dash) and pairwise distinct.
func resolveModes(cfg Config) (serve, mcpWord, helpWord string, err error) {
	serve, mcpWord, helpWord = cfg.Modes.Serve, cfg.Modes.MCP, cfg.Modes.Help
	if serve == "" {
		serve = "serve"
	}
	if mcpWord == "" {
		mcpWord = "mcp"
	}
	if helpWord == "" {
		helpWord = "help"
	}
	for _, w := range []string{serve, mcpWord, helpWord} {
		if strings.HasPrefix(w, "-") || strings.ContainsAny(w, " \t") {
			return "", "", "", fmt.Errorf("xyz: invalid mode word %q (no leading dash, no whitespace)", w)
		}
	}
	if serve == mcpWord || serve == helpWord || mcpWord == helpWord {
		return "", "", "", fmt.Errorf("xyz: mode words must be pairwise distinct (serve=%q mcp=%q help=%q)",
			serve, mcpWord, helpWord)
	}
	return serve, mcpWord, helpWord, nil
}

// checkReserved rejects registry names whose top-level segment collides
// with a mode keyword, because the dispatcher owns those words.
func checkReserved(reg *registry.Registry, serve, mcpWord, helpWord string) error {
	if reg == nil {
		return fmt.Errorf("xyz: nil registry")
	}
	for _, name := range reg.Names() {
		top, _, _ := strings.Cut(name, ".")
		switch top {
		case serve, mcpWord, helpWord:
			return fmt.Errorf("xyz: command %q: top-level name %q is reserved for mode dispatch", name, top)
		}
	}
	return nil
}
