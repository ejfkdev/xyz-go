package spec

import (
	"context"
	"fmt"
	"reflect"
	"regexp"

	errs "github.com/ejfkdev/xyz-go/errors"
)

// entryNameRe is MCP tool-name compatible and doubles as the constraint for
// CLI subcommand names and HTTP route slugs.
var entryNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// Entry is the type-erased view of one command. This is the ONLY surface
// frontends see: metadata, InputSchema and Invoke. The concrete argument and
// result types stay hidden inside the Invoke closure.
type Entry struct {
	Name        string
	Summary     string
	Description string

	// InputSchema is the JSON Schema of the argument struct; the MCP frontend
	// and the future OpenAPI document consume it directly.
	InputSchema *Schema

	// OutputSchema is the JSON Schema of the handler's return type
	// (MCP tool.outputSchema / OpenAPI response schema). Nil when the result
	// type cannot be schema-ed (interfaces, maps, recursive types).
	OutputSchema *Schema

	// Root describes the argument struct tree (tags, defaults, CLI/HTTP
	// bindings) for frontends that generate flags, routes or binders.
	Root *FieldMeta

	CLI  CliHints
	HTTP HTTPHints
	MCP  MCPHints

	// Invoke decodes args — any frontend's reduced map shape — applies
	// defaults, validates and runs the handler. Decode and validation
	// failures are always returned as coded KindInvalidInput errors.
	Invoke func(ctx context.Context, args map[string]any) (any, error)
}

// Registrar is implemented by registry.Registry. Keeping it an interface
// here avoids a spec -> registry import cycle.
type Registrar interface {
	Add(*Entry) error
}

// DefaultRegistrar is the target of RegisterDefault. The registry package
// wires its process-wide default registry into this slot at init time, so
// programs that import xyz (or registry) never set it themselves. Programs
// that use only explicit registries can ignore it.	Set to nil (e.g. in
// tests) to observe RegisterDefault's error path.
var DefaultRegistrar Registrar

// RegisterDefault builds the Entry and registers it into DefaultRegistrar.
// It is the singleton flavor: pair it with xyz.Main() and user code never
// constructs or imports a registry.
func (c *Command[T, R]) RegisterDefault() (*Entry, error) {
	if DefaultRegistrar == nil {
		return nil, fmt.Errorf("spec: command %q: no default registrar wired (import xyz/registry, or use Register with an explicit registry)", c.name)
	}
	return c.Register(DefaultRegistrar)
}

// Entry analyzes the argument type once and returns the frontend-facing
// Entry without registering it. All definition errors (bad names, bad tags,
// unsupported field kinds) surface here rather than at first invocation.
func (c *Command[T, R]) Entry() (*Entry, error) {
	if c.handler == nil {
		return nil, fmt.Errorf("spec: command %q: nil handler", c.name)
	}
	if !entryNameRe.MatchString(c.name) {
		return nil, fmt.Errorf("spec: command %q: name must match %s", c.name, entryNameRe)
	}
	var zero T
	t := reflect.TypeOf(zero)
	if t == nil {
		return nil, fmt.Errorf("spec: command %q: type parameter T is an interface; pass the argument struct itself", c.name)
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("spec: command %q: type parameter T must be a struct (not %s); the handler receives *T", c.name, t.Kind())
	}
	root := &FieldMeta{Name: t.Name(), Type: t, Kind: t.Kind()}
	if err := analyzeStruct(root, t, 0); err != nil {
		return nil, fmt.Errorf("spec: command %q: %w", c.name, err)
	}
	if err := applyHints(root, c.cli.Fields, c.http.Fields, c.mcp.Fields); err != nil {
		return nil, fmt.Errorf("spec: command %q: %w", c.name, err)
	}
	e := &Entry{
		Name:        c.name,
		Summary:     c.summary,
		Description: c.description,
		InputSchema: buildSchema(root),
		Root:        root,
		CLI:         c.cli,
		HTTP:        c.http,
		MCP:         c.mcp,
	}
	var zeroR R
	if rt := reflect.TypeOf(zeroR); rt != nil {
		if s, err := buildOutputSchema(rt); err == nil {
			e.OutputSchema = s
		}
	}
	e.Invoke = c.makeInvoke(root)
	return e, nil
}

// Register builds the Entry and registers it, returning the registered Entry.
func (c *Command[T, R]) Register(r Registrar) (*Entry, error) {
	e, err := c.Entry()
	if err != nil {
		return nil, err
	}
	if err := r.Add(e); err != nil {
		return nil, err
	}
	return e, nil
}

// CLIDefaults returns the CLI-specific default for every bindable field,
// keyed by JSON name. The CLI frontend injects these into the argument map
// before Invoke; the global tag defaults are applied by Invoke itself, so
// precedence is CLI override > global tag > zero value.
func (e *Entry) CLIDefaults() map[string]any {
	return transportDefaults(e.Root, func(f *FieldMeta) any { return f.CLI.Default })
}

// HTTPDefaults is CLIDefaults for the HTTP frontend.
func (e *Entry) HTTPDefaults() map[string]any {
	return transportDefaults(e.Root, func(f *FieldMeta) any { return f.HTTP.Default })
}

// MCPDefaults is CLIDefaults for the MCP frontend. MCP overrides also
// replace the global default in InputSchema.
func (e *Entry) MCPDefaults() map[string]any {
	return transportDefaults(e.Root, func(f *FieldMeta) any { return f.MCP.Default })
}

func transportDefaults(node *FieldMeta, get func(*FieldMeta) any) map[string]any {
	out := map[string]any{}
	for _, f := range node.Fields {
		if f.Skip {
			continue
		}
		if d := get(f); d != nil {
			out[f.JSONName] = d
		}
	}
	return out
}

// makeInvoke returns the type-erased invocation closure. It is the shared
// pipeline every transport funnels its input into:
// map[string]any -> typed struct (with defaults) -> validation -> handler.
func (c *Command[T, R]) makeInvoke(root *FieldMeta) func(context.Context, map[string]any) (any, error) {
	return func(ctx context.Context, args map[string]any) (any, error) {
		in := reflect.New(root.Type).Interface().(*T)
		if err := decodeTree(root, args, reflect.ValueOf(in).Elem()); err != nil {
			return nil, errs.Wrap(errs.KindInvalidInput, err)
		}
		if err := runValidation(in, root); err != nil {
			return nil, err
		}
		return c.handler(ctx, in)
	}
}
