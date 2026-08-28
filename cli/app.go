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
	reg    *registry.Registry
	root   *cmdNode
	out    io.Writer
	errOut io.Writer
	mws    []ExecFunc
}

// Options configures frontend-level behavior for embedding (e.g. mounting
// the CLI inside a larger program). The zero value keeps os.Stdout/os.Stderr.
type Options struct {
	Out    io.Writer // 命令结果的输出目标（默认 os.Stdout）
	ErrOut io.Writer // 错误与帮助的输出目标（默认 os.Stderr）
}

// NewWithOptions is New with frontend options; nil writers keep the defaults.
func NewWithOptions(reg *registry.Registry, opts Options) (*App, error) {
	a, err := New(reg)
	if err != nil {
		return nil, err
	}
	a.SetOutput(opts.Out, opts.ErrOut)
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
	JSON  bool        // --json 是否生效（未调用 next 时自行渲染可参考）
	Out   io.Writer   // 结果的输出目标
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
	jsonOut := false
	filtered := make([]string, 0, len(args))
	pastDoubleDash := false
	for _, arg := range args {
		if pastDoubleDash {
			filtered = append(filtered, arg)
			continue
		}
		switch arg {
		case "--":
			pastDoubleDash = true
			filtered = append(filtered, arg)
		case "--json":
			jsonOut = true
		default:
			filtered = append(filtered, arg)
		}
	}
	if err := a.execute(ctx, a.root, filtered, jsonOut, bin); err != nil {
		fmt.Fprintln(a.errOut, err)
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

func exitCode(err error) int {
	var ce *errs.CodedError
	if stderrors.As(err, &ce) {
		return errs.ExitCode(ce.Kind)
	}
	return 2 // 用法/flag 解析错误
}

func (a *App) execute(ctx context.Context, node *cmdNode, args []string, jsonOut bool, bin string) error {
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
	ec := &ExecContext{Path: node.path, Entry: node.entry, JSON: jsonOut, Out: a.out}
	var chain ExecFunc = func(ctx context.Context, ec *ExecContext, args map[string]any, _ func() error) error {
		out, err := ec.Entry.Invoke(ctx, args)
		if err != nil {
			return err
		}
		// 长驻命令：达到 ctx 取消即优雅关停，不渲染返回值。
		if ec.Entry.CLI.Daemon {
			return nil
		}
		if ec.JSON {
			enc := json.NewEncoder(ec.Out)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		}
		if ec.Entry.CLI.Output != nil {
			return ec.Entry.CLI.Output(ec.Out, out)
		}
		if handled, err := projectBlocks(ec.Out, out); handled || err != nil {
			return err
		}
		return Render(ec.Out, out)
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
