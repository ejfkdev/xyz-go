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
	"github.com/ejfkdev/xyz-go/registry"
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
		if arg == "-v" || arg == "--version" {
			fmt.Fprintf(a.out, "%s version %s\n", bin, Version)
			return 0
		}
	}
	jsonOut := false
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--json" {
			jsonOut = true
			continue
		}
		filtered = append(filtered, arg)
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
		return fmt.Errorf("%s: 位置参数数量不符（需要 %d 到 %d 个，收到 %d 个）",
			strings.Join(strings.Split(node.path, "."), " "), node.minPos, node.maxPos, len(pos))
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
	out, err := node.entry.Invoke(ctx, m)
	if err != nil {
		return err
	}
	if jsonOut {
		enc := json.NewEncoder(a.out)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	return Render(a.out, out)
}

// parseFlags 解析 args 中的 flag（长短名、= 形式、bool 无值形式），返回
// 每个 flag 的取值与剩余位置参数。未知 flag 报错（用法错误 → 退出码 2）。
