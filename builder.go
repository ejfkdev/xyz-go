package xyz

import (
	"fmt"
	"os"

	"github.com/ejfkdev/xyz-go/registry"
	"github.com/ejfkdev/xyz-go/spec"
)

// spec 公开类型在根包的原样别名：链式（单例）写法下用户只需要 import "github.com/ejfkdev/xyz-go"。
type (
	Handler[T, R any] = spec.Handler[T, R]
	CliHints          = spec.CliHints
	HTTPHints         = spec.HTTPHints
	MCPHints          = spec.MCPHints
	CliFieldHint      = spec.CliFieldHint
	HTTPFieldHint     = spec.HTTPFieldHint
	MCPFieldHint      = spec.MCPFieldHint
)

// Definable is implemented by any fully-built command definition
// (spec.Command[T, R], or the Builder returned by Define), so heterogeneous
// commands can be collected into one chain or one Main call.
type Definable interface {
	Register(spec.Registrar) (*spec.Entry, error)
}

// Builder is the fluent main entry: one Define chain configures the whole
// program. Define opens it, Summary/Description/CLI/HTTP/MCP configure the
// current command, Also appends fully-built commands, and the terminal Run
// registers everything into the default registry, dispatches the process
// arguments, and exits with the resulting exit code.
//
//	xyz.Define("user.add", addUser).
//		Summary("创建用户").
//		CLI(xyz.CliHints{...}).
//		MCP(xyz.MCPHints{...}).
//		Also(
//			xyz.Define("math.sum", sum).Summary("求和"),
//			xyz.Define("time.now", now).Summary("当前 UTC 时间"),
//		).
//		Run()
//
// Go has no generic methods, so the chain needs only this one convention:
// the first command is configured inline, every further command is a
// complete Define(...) chain handed to Also.
type Builder[T, R any] struct {
	cmd       *spec.Command[T, R]
	reg       *registry.Registry
	committed bool
	config    Config
	err       error
}

// Define opens a chain on the default registry.
func Define[T, R any](name string, h Handler[T, R]) *Builder[T, R] {
	return &Builder[T, R]{
		cmd: spec.Define[T, R](name, h),
		reg: registry.Default,
	}
}

// Register implements Definable: it registers the underlying command into r.
func (b *Builder[T, R]) Register(r spec.Registrar) (*spec.Entry, error) {
	return b.cmd.Register(r)
}

// Summary sets the one-line description of the command.
func (b *Builder[T, R]) Summary(s string) *Builder[T, R] {
	b.cmd = b.cmd.Summary(s)
	return b
}

// Description sets the longer explanation of the command.
func (b *Builder[T, R]) Description(s string) *Builder[T, R] {
	b.cmd = b.cmd.Description(s)
	return b
}

// CLI attaches command-level CLI options.
func (b *Builder[T, R]) CLI(h CliHints) *Builder[T, R] {
	b.cmd = b.cmd.CLI(h)
	return b
}

// HTTP attaches command-level HTTP options.
func (b *Builder[T, R]) HTTP(h HTTPHints) *Builder[T, R] {
	b.cmd = b.cmd.HTTP(h)
	return b
}

// MCP attaches command-level MCP options.
func (b *Builder[T, R]) MCP(h MCPHints) *Builder[T, R] {
	b.cmd = b.cmd.MCP(h)
	return b
}

// Configure sets the dispatcher configuration used by Run / RunArgs (mode
// words, channel capabilities). Call it anywhere on the chain; RunConfig and
// RunArgsConfig take an explicit Config for that call instead.
func (b *Builder[T, R]) Configure(cfg Config) *Builder[T, R] {
	b.config = cfg
	return b
}

// Also registers the current command and every command passed in, all into
// the same default registry, then keeps the chain going. Call it again to
// append more. Registration failures stop the chain: they surface at Run.
func (b *Builder[T, R]) Also(cmds ...Definable) *Builder[T, R] {
	if b.err == nil && !b.committed {
		if _, err := b.cmd.Register(b.reg); err != nil {
			b.err = err
		} else {
			b.committed = true
		}
	}
	if b.err == nil {
		for _, c := range cmds {
			if _, err := c.Register(b.reg); err != nil {
				b.err = err
				break
			}
		}
	}
	return b
}

// Run registers the command (if not yet registered), dispatches the default
// registry on the process arguments, and exits with the resulting exit code.
// Everything after it is unreachable by design.
func (b *Builder[T, R]) Run() {
	os.Exit(b.RunArgs(os.Args[1:]))
}

// RunConfig is Run with a custom configuration (e.g. renamed mode words).
func (b *Builder[T, R]) RunConfig(cfg Config) {
	os.Exit(b.RunArgsConfig(os.Args[1:], cfg))
}

// RunArgs is the testable/embeddable form of Run: it registers, dispatches
// and returns the exit code without exiting the process. It uses the chain's
// Configured settings (zero value = every default).
func (b *Builder[T, R]) RunArgs(args []string) int {
	return b.RunArgsConfig(args, b.config)
}

// RunArgsConfig is RunArgs with a custom configuration.
func (b *Builder[T, R]) RunArgsConfig(args []string, cfg Config) int {
	if b.err == nil && !b.committed {
		if _, err := b.cmd.Register(b.reg); err != nil {
			b.err = err
		} else {
			b.committed = true
		}
	}
	if b.err != nil {
		fmt.Fprintln(os.Stderr, b.err)
		return 2
	}
	return RunConfig(b.reg, args, cfg)
}
