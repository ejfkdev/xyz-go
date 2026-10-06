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
	defaultFormat string // 默认输出格式（来自 --xyz.format，经 Options.Format 注入）；""=text
}

// Options configures frontend-level behavior for embedding (e.g. mounting
// the CLI inside a larger program). The zero value keeps os.Stdout/os.Stderr.
type Options struct {
	Out    io.Writer // 命令结果的输出目标（默认 os.Stdout）
	ErrOut io.Writer // 错误与帮助的输出目标（默认 os.Stderr）
	// Format 是默认输出格式（text|json|jsonl|markdown，""=text），通常由根
	// 派发器把 --xyz.format 注入到这里。命令行的裸 --format/--json 优先于它。
	Format string
}

// NewWithOptions is New with frontend options; nil writers keep the defaults.
func NewWithOptions(reg *registry.Registry, opts Options) (*App, error) {
	if !ValidFormat(opts.Format) {
		return nil, fmt.Errorf("cli: invalid default format %q (want text|json|jsonl|markdown)", opts.Format)
	}
	a, err := New(reg)
	if err != nil {
		return nil, err
	}
	a.SetOutput(opts.Out, opts.ErrOut)
	a.defaultFormat = opts.Format
	return a, nil
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
	// Format 是本次执行的 --format 取值（"" 等价 text）。机器模式
	//（json/jsonl）下错误也以 JSON 写 stderr；显式格式绕过命令的
	// CLIOutputFunc，text 才进入 Output > 信封投影 > Render 链。
	Format string
	Out    io.Writer // 结果的输出目标
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
	// 输出格式：默认取自 --xyz.format（经 Options.Format 注入 a.defaultFormat），
	// 命令行裸 --format/--json 覆盖之。冲突感知（xyz-spec §10.7）：裸形式仅在
	// 目标命令*没有*定义同名 flag 时才作全局格式；若命令自有 format/json 字段，
	// 裸标志让位给命令（全局格式只认全称 --xyz.format）。
	format := a.defaultFormat
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
			format = FormatJSON
		case arg == "--format" && !formatConflict:
			if i+1 >= len(args) {
				fmt.Fprintln(a.errOut, "xyz: --format needs an argument (text|json|jsonl|markdown)")
				return 2
			}
			i++
			format = args[i]
		case strings.HasPrefix(arg, "--format=") && !formatConflict:
			format = strings.TrimPrefix(arg, "--format=")
		default:
			// 含冲突时未消费的裸 --format/--json：原样留给命令自己的 flag 解析。
			filtered = append(filtered, arg)
		}
	}
	if !ValidFormat(format) {
		fmt.Fprintf(a.errOut, "xyz: invalid output format %q (want text|json|jsonl|markdown)\n", format)
		return 2
	}
	if err := a.execute(ctx, a.root, filtered, format, bin); err != nil {
		a.renderError(err, format)
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

func (a *App) execute(ctx context.Context, node *cmdNode, args []string, format string, bin string) error {
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
			return nil
		}
	}
	if !node.leaf {
		a.printHelp(node, bin)
		return nil
	}
	fvals, pos, err := parseFlags(node.defs, rest)
	if err != nil {
		return err
	}
	if len(pos) < node.minPos || len(pos) > node.maxPos {
		return fmt.Errorf("%s", langx.Tf("cli.err_positional_count",
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
	ec := &ExecContext{Path: node.path, Entry: node.entry, JSON: format == FormatJSON, Format: format, Out: a.out}
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
	return chain(ctx, ec, m, func() error { return nil })
}

// parseFlags 解析 args 中的 flag（长短名、= 形式、bool 无值形式），返回
// 每个 flag 的取值与剩余位置参数。未知 flag 报错（用法错误 → 退出码 2）。
