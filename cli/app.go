// Package cli is the CLI frontend: it consumes a registry's entries and
// turns their cli bindings (shorthands, positionals, env fallbacks,
// transport-specific defaults) into a command tree. Dotted registry names
// map to nested subcommands: "user.add" becomes "user add".
//
// The frontend is implemented on the standard library only (no cobra /
// pflag), so this package carries zero third-party dependencies.
//
// Output rendering lives in Render: primitives print bare (no {"data": ...}
// envelope), structs print as aligned key/value pairs, slices of structs as
// an aligned table, and --json flips everything to raw JSON.
package cli

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	errs "github.com/ejfkdev/xyz-go/errors"
	"github.com/ejfkdev/xyz-go/langx"
	"github.com/ejfkdev/xyz-go/registry"
	"github.com/ejfkdev/xyz-go/spec"
)

// Version is the version reported by the CLI frontend's -v/--version flag
// when the frontend is embedded directly via cli.Run. The root dispatcher
// (xyz.Run) owns its own xyz.Version and answers -v before reaching here.
var Version = "dev"

type App struct {
	reg           *registry.Registry
	root          *cmdNode
	out           io.Writer
	errOut        io.Writer
	mws           []ExecFunc
	defaultFormat string // 全局默认格式（Config.Format / --xyz.format）；""|auto = 按 TTY 解析
	formatInteractive string // auto 的交互式解析目标（默认 text）
	formatPiped       string // auto 的非交互式解析目标（默认 jsonl）
	formatFromFlag    bool   // defaultFormat 是否来自命令行 --xyz.format（命令行层，高于逐命令 hint）
	interactiveOverride *bool // nil=按 out 是否 TTY 自动探测；非 nil=强制（测试/嵌入用）
}

// Options configures frontend-level behavior for embedding (e.g. mounting
// the CLI inside a larger program). The zero value keeps os.Stdout/os.Stderr.
type Options struct {
	Out    io.Writer // 命令结果的输出目标（默认 os.Stdout）
	ErrOut io.Writer // 错误与帮助的输出目标（默认 os.Stderr）
	// Format 是全局默认输出格式（auto|text|json|jsonl|markdown，""=auto）。
	// auto = 按 stdout 是否交互式终端解析为 FormatInteractive / FormatPiped。
	// 通常由根派发器把 --xyz.format / Config.Format 注入到这里。
	Format string
	// FormatInteractive / FormatPiped 是 Format=auto 时两种上下文各自的具体
	// 格式；空则用内置默认（交互式 text、非交互式 jsonl）。
	FormatInteractive string
	FormatPiped       string
	// FormatFromFlag 标记 Format 来自命令行 --xyz.format（命令行层，优先级
	// 高于逐命令的 CliHints.Format）；false 时 Format 视作代码级全局默认。
	FormatFromFlag bool
	// Interactive 强制指定交互式与否：nil = 按 Out 是否为 TTY 自动探测；
	// 非 nil = 强制该值（测试与嵌入场景用，无需真实终端）。
	Interactive *bool
}

// NewWithOptions is New with frontend options; nil writers keep the defaults.
func NewWithOptions(reg *registry.Registry, opts Options) (*App, error) {
	for _, f := range []string{opts.Format, opts.FormatInteractive, opts.FormatPiped} {
		if !ValidFormat(f) {
			return nil, fmt.Errorf("cli: invalid format %q (want auto|text|json|jsonl|markdown)", f)
		}
	}
	a, err := New(reg)
	if err != nil {
		return nil, err
	}
	a.SetOutput(opts.Out, opts.ErrOut)
	a.defaultFormat = opts.Format
	a.formatFromFlag = opts.FormatFromFlag
	a.interactiveOverride = opts.Interactive
	a.formatInteractive = opts.FormatInteractive
	if a.formatInteractive == "" {
		a.formatInteractive = FormatText
	}
	a.formatPiped = opts.FormatPiped
	if a.formatPiped == "" {
		a.formatPiped = FormatJSONL
	}
	return a, nil
}

// isInteractive 报告输出目标是否为交互式终端（TTY）。override 非 nil 时直接
// 采用（测试/嵌入）；否则仅当 w 是 *os.File 且为字符设备时判为交互式——
// bytes.Buffer、管道、重定向文件一律非交互式。这是格式轴与（将来的）样式/
// 彩色轴共用的同一个 TTY 探测（xyz-spec §10.7）。
func isInteractive(w io.Writer, override *bool) bool {
	if override != nil {
		return *override
	}
	if f, ok := w.(*os.File); ok {
		fi, err := f.Stat()
		if err != nil {
			return false
		}
		return fi.Mode()&os.ModeCharDevice != 0
	}
	return false
}

// SetOutput redirects the frontend's output streams; nil keeps the current
// writer. Useful when embedding the CLI in a larger program or in tests.
func (a *App) SetOutput(out, errOut io.Writer) {
	if out != nil {
		a.out = out
	}
	if errOut != nil {
		a.errOut = errOut
	}
}

// ExecContext is a read-only snapshot of one leaf-command execution, passed
// to Execute middleware (App.Use).
type ExecContext struct {
	Path  string      // 点分注册名，如 user.add
	Entry *spec.Entry // 命令元数据（Hints、InputSchema、OutputSchema）
	JSON  bool        // 等价 Format==json（向后兼容；判断机器模式请优先用 Format）
	// Format 是本次执行*解析后*的具体格式（text/json/jsonl/markdown，永不为
	// auto）。机器模式（json/jsonl）下错误也以 JSON 写 stderr；显式非 text
	// 格式绕过命令的 CLIOutputFunc，text 才进入 Output > 信封投影 > Render 链。
	Format string
	// Interactive 报告本次执行的输出目标是否为交互式终端（TTY）。自定义
	// Output 函数与中间件可据此决定是否上色/用富排版（与格式轴正交，是将来
	// 样式/彩色轴的同一信号）。
	Interactive bool
	Out         io.Writer // 结果的输出目标
}

// ExecFunc is an Execute middleware around leaf execution: args is the
// already-parsed argument map (flags, env and positionals applied); next()
// continues the chain down to Invoke + rendering. Return value semantics are
// identical to normal command errors (mapped to exit codes by kind).
type ExecFunc func(ctx context.Context, ec *ExecContext, args map[string]any, next func() error) error

// Use appends Execute middleware (outermost first). next() continues the
// remaining chain down to Invoke + rendering. Middleware may mutate args,
// short-circuit (skip next for a custom rendering) or wrap next for
// timing/tracing.
func (a *App) Use(mws ...ExecFunc) {
	a.mws = append(a.mws, mws...)
}

// New builds the command tree. Unbindable field kinds (nested structs,
// maps) and ambiguous positionals (required after optional) are
// configuration errors.
func (a *App) Run(args []string) int {
	return a.RunContext(context.Background(), args)
}

// RunContext is Run with an explicit context, which flows into the invoked
// handlers (graceful shutdown, cancellation).
func (a *App) RunContext(ctx context.Context, args []string) int {
	// 内建 completion 子命令：生成 shell 补全脚本（bash/zsh/fish）。
	if len(args) > 0 && args[0] == "completion" {
		shell := "bash"
		if len(args) > 1 {
			shell = args[1]
		}
		if code := a.printCompletion(a.out, a.errOut, shell); code != 0 {
			return code
		}
		return 0
	}
	bin := filepath.Base(os.Args[0])
	if bin == "" || bin == "." || bin == "/" {
		bin = "app"
	}
	for _, arg := range args {
		if arg == "--" {
			break // 之后的 token 全是位置参数，-v 不再算开关
		}
		if arg == "-v" || arg == "--version" {
			fmt.Fprintf(a.out, "%s version %s\n", bin, Version)
			return 0
		}
	}
	// 裸 --format/--json 是命令行逐次调用层（最高优先级）；冲突感知：目标命令
	// 自有同名 flag 时让位给命令（裸标志留进 filtered 交给 parseFlags）。
	// 全局默认 / 逐命令 hint / 内置 auto 在 execute 里按层级解析（需要 entry）。
	bareFormat := ""
	target := a.resolveTargetNode(args)
	formatConflict := nodeHasFlagLong(target, "format")
	jsonConflict := nodeHasFlagLong(target, "json")
	filtered := make([]string, 0, len(args))
	pastDoubleDash := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if pastDoubleDash {
			filtered = append(filtered, arg)
			continue
		}
		switch {
		case arg == "--":
			pastDoubleDash = true
			filtered = append(filtered, arg)
		case arg == "--json" && !jsonConflict:
			bareFormat = FormatJSON
		case arg == "--format" && !formatConflict:
			if i+1 >= len(args) {
				fmt.Fprintln(a.errOut, "xyz: --format needs an argument (auto|text|json|jsonl|markdown)")
				return 2
			}
			i++
			bareFormat = args[i]
		case strings.HasPrefix(arg, "--format=") && !formatConflict:
			bareFormat = strings.TrimPrefix(arg, "--format=")
		default:
			// 含冲突时未消费的裸 --format/--json：原样留给命令自己的 flag 解析。
			filtered = append(filtered, arg)
		}
	}
	if bareFormat != "" && !ValidFormat(bareFormat) {
		fmt.Fprintf(a.errOut, "xyz: invalid output format %q (want auto|text|json|jsonl|markdown)\n", bareFormat)
		return 2
	}
	interactive := isInteractive(a.out, a.interactiveOverride)
	resolved, err := a.execute(ctx, a.root, filtered, bareFormat, interactive, bin)
	if err != nil {
		a.renderError(err, resolved)
		return exitCode(err)
	}
	return 0
}

// Run builds the tree and executes, for one-call use.
func Run(reg *registry.Registry, args []string) int {
	return RunContext(context.Background(), reg, args)
}

// RunContext is Run with an explicit context for the invoked handlers.
func RunContext(ctx context.Context, reg *registry.Registry, args []string) int {
	a, err := New(reg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return a.RunContext(ctx, args)
}

// RunWithOptions 是带前端选项（默认输出格式等）的 Run，供根派发器把
// --xyz.format 注入 CLI 前端。
func RunWithOptions(reg *registry.Registry, args []string, opts Options) int {
	return RunContextWithOptions(context.Background(), reg, args, opts)
}

// RunContextWithOptions 是 RunWithOptions 的显式 context 形态。
func RunContextWithOptions(ctx context.Context, reg *registry.Registry, args []string, opts Options) int {
	a, err := NewWithOptions(reg, opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return a.RunContext(ctx, args)
}

// resolveTargetNode 沿命令树下降，找到裸 --format/--json 冲突检测的目标节点：
// 透明跳过全局格式标志（--json、--format 及其值、--format=v），遇到其它 flag
// 或 `--` 停止下降，未匹配段触发默认子命令回退。仅用于判定目标命令是否自有
// 同名 flag，不消费参数、不报错。
func (a *App) resolveTargetNode(args []string) *cmdNode {
	node := a.root
	for i := 0; i < len(args); i++ {
		t := args[i]
		if t == "--" {
			break
		}
		if t == "--json" || strings.HasPrefix(t, "--format=") {
			continue
		}
		if t == "--format" {
			i++ // 连同其值一起跳过
			continue
		}
		if strings.HasPrefix(t, "-") {
			break // 其它 flag：命令路径到此为止
		}
		if child, ok := node.children[t]; ok {
			node = child
			continue
		}
		if !node.leaf && node.dflt != nil {
			node = node.dflt
			if child, ok := node.children[t]; ok {
				node = child
				continue
			}
		}
		break // t 是位置参数（或未知段）：路径结束
	}
	return node
}

// nodeHasFlagLong 报告节点是否定义了指定长名的 flag（用于裸全局标志的冲突
// 让位判定）。
func nodeHasFlagLong(node *cmdNode, long string) bool {
	if node == nil {
		return false
	}
	for i := range node.defs {
		if node.defs[i].long == long {
			return true
		}
	}
	return false
}

func exitCode(err error) int {
	var ce *errs.CodedError
	if stderrors.As(err, &ce) {
		return errs.ExitCode(ce.Kind)
	}
	return 2 // 用法/flag 解析错误
}

// renderError 把命令错误写到 errOut：机器模式（json/jsonl）下输出三通道共享
// 的富化错误体（errs.Body：扁平 error 字符串 + kind/code/detail，与 HTTP
// 错误体同款），text 模式输出人类可读的错误行。stdout 始终只承载数据，
// 错误一律走 stderr（xyz-spec §8.3/§10.5）。
func (a *App) renderError(err error, format string) {
	switch format {
	case FormatJSON, FormatJSONL:
		enc := json.NewEncoder(a.errOut)
		if format == FormatJSON {
			enc.SetIndent("", "  ")
		}
		_ = enc.Encode(errs.ErrorBody(err))
	default:
		fmt.Fprintln(a.errOut, err)
	}
}

func (a *App) execute(ctx context.Context, node *cmdNode, args []string, bareFormat string, interactive bool, bin string) (string, error) {
	rest := args
	for len(rest) > 0 {
		child, ok := node.children[rest[0]]
		if !ok {
			break
		}
		node, rest = child, rest[1:]
	}
	// 默认子命令：首段不是已注册命令段、也不是 flag（-h/-v/--json 等）时，
	// 整串参数不消费地转发给默认子命令（udf img == udf extract img）。
	if !node.leaf && node.dflt != nil && len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		node = node.dflt
	}
	for _, t := range rest {
		if t == "-h" || t == "--help" {
			a.printHelp(node, bin)
			return "", nil
		}
	}
	if !node.leaf {
		a.printHelp(node, bin)
		return "", nil
	}
	// 目标命令已确定，按四层优先级解析生效格式（含 auto 的 TTY 解析）。
	eff := a.resolveFormat(bareFormat, node.entry, interactive)
	fvals, pos, err := parseFlags(node.defs, rest)
	if err != nil {
		return eff, err
	}
	if len(pos) < node.minPos || len(pos) > node.maxPos {
		return eff, fmt.Errorf("%s", langx.Tf("cli.err_positional_count",
			strings.Join(strings.Split(node.path, "."), " "),
			fmt.Sprint(node.minPos), fmt.Sprint(node.maxPos), fmt.Sprint(len(pos))))
	}
	m := map[string]any{}
	for i := range node.defs {
		d := &node.defs[i]
		fv := fvals[i]
		if fv.seen {
			switch d.kind {
			case fBool:
				m[d.field.JSONName] = fv.boolean
			case fSlice:
				m[d.field.JSONName] = fv.list
			default:
				m[d.field.JSONName] = fv.str
			}
			continue
		}
		if v, ok := os.LookupEnv(d.field.CLI.EnvVar); ok && v != "" {
			m[d.field.JSONName] = v
			continue
		}
		if d.field.CLI.Default != nil {
			m[d.field.JSONName] = d.field.CLI.Default
		}
	}
	for _, f := range node.envOnly {
		if v, ok := os.LookupEnv(f.CLI.EnvVar); ok && v != "" {
			m[f.Name] = v // json:"-" 字段以 Go 字段名为注入键
		}
	}
	for i, f := range node.posF {
		if i < len(pos) {
			m[f.JSONName] = pos[i]
		}
	}
	ec := &ExecContext{Path: node.path, Entry: node.entry, JSON: eff == FormatJSON, Format: eff, Interactive: interactive, Out: a.out}
	var chain ExecFunc = func(ctx context.Context, ec *ExecContext, args map[string]any, _ func() error) error {
		out, err := ec.Entry.Invoke(ctx, args)
		if err != nil {
			return err
		}
		// 长驻命令：达到 ctx 取消即优雅关停，不渲染返回值。
		if ec.Entry.CLI.Daemon {
			return nil
		}
		// 显式 --format（json/jsonl/markdown）绕过命令的 CLIOutputFunc；
		// text（默认）才进入 Output > §12.7 信封投影 > Render 链（§10.6）。
		switch ec.Format {
		case FormatJSON:
			enc := json.NewEncoder(ec.Out)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		case FormatJSONL:
			return RenderJSONL(ec.Out, out)
		case FormatMarkdown:
			return RenderMarkdown(ec.Out, out)
		default:
			if ec.Entry.CLI.Output != nil {
				return ec.Entry.CLI.Output(ec.Out, out)
			}
			if handled, err := projectBlocks(ec.Out, out); handled || err != nil {
				return err
			}
			return Render(ec.Out, out)
		}
	}
	for i := len(a.mws) - 1; i >= 0; i-- {
		mw := a.mws[i]
		inner := chain
		chain = func(ctx context.Context, ec *ExecContext, args map[string]any, next func() error) error {
			return mw(ctx, ec, args, func() error { return inner(ctx, ec, args, next) })
		}
	}
	return eff, chain(ctx, ec, m, func() error { return nil })
}

// resolveFormat 按四层优先级解析本次执行生效的具体格式（xyz-spec §10.7）：
//  1. 裸 --format/--json（命令行逐次调用，bareFormat 非空）
//  2. --xyz.format（命令行全局；a.formatFromFlag 为真时的 a.defaultFormat）
//  3. CliHints.Format（逐命令代码默认）
//  4. Config.Format（全局代码默认，即 a.defaultFormat）
//  5. 内置 auto
//
// 任一层取值为 auto（或最终落到 auto）时，按 interactive 解析为
// a.formatInteractive（默认 text）/ a.formatPiped（默认 jsonl）。返回值恒为
// 具体格式（text/json/jsonl/markdown），供渲染与错误输出直接使用。
func (a *App) resolveFormat(bare string, e *spec.Entry, interactive bool) string {
	eff := bare
	if eff == "" && a.formatFromFlag {
		eff = a.defaultFormat // 命令行 --xyz.format：高于逐命令 hint
	}
	if eff == "" && e != nil {
		eff = e.CLI.Format // 逐命令 hint
	}
	if eff == "" {
		eff = a.defaultFormat // 代码级全局 Config.Format
	}
	if eff == "" || eff == FormatAuto {
		if interactive {
			eff = a.formatInteractive
			if eff == "" {
				eff = FormatText // 内置默认：交互式 → text
			}
		} else {
			eff = a.formatPiped
			if eff == "" {
				eff = FormatJSONL // 内置默认：非交互式 → jsonl
			}
		}
	}
	return eff
}

// parseFlags 解析 args 中的 flag（长短名、= 形式、bool 无值形式），返回
// 每个 flag 的取值与剩余位置参数。未知 flag 报错（用法错误 → 退出码 2）。
