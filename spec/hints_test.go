package spec

import (
	"context"
	"testing"
)

type hintArgs struct {
	Name string `json:"name" desc:"名称" required:"true" validate:"min=2"`
	Age  int    `json:"age" desc:"年龄" default:"18"`
}

func hintHandler(_ context.Context, in *hintArgs) (int, error) { return in.Age, nil }

func findField(t *testing.T, root *FieldMeta, jsonName string) *FieldMeta {
	t.Helper()
	for _, f := range root.Fields {
		if f.JSONName == jsonName {
			return f
		}
	}
	t.Fatalf("field %q not found", jsonName)
	return nil
}

func TestHintsOverlay(t *testing.T) {
	e, err := Define("h.cmd", hintHandler).
		CLI(CliHints{Fields: map[string]CliFieldHint{
			"age": {Shorthand: "a", Default: 30},
		}}).
		HTTP(HTTPHints{Fields: map[string]HTTPFieldHint{
			"age": {Location: "query", Default: 22},
		}}).
		MCP(MCPHints{Fields: map[string]MCPFieldHint{
			"age": {Default: 25},
		}}).
		Entry()
	if err != nil {
		t.Fatalf("Entry: %v", err)
	}

	age := findField(t, e.Root, "age")
	if age.CLI.Shorthand != "a" {
		t.Fatalf("shorthand = %q, want a", age.CLI.Shorthand)
	}
	if got, ok := age.CLI.Default.(int); !ok || got != 30 {
		t.Fatalf("cli default = %v, want 30", age.CLI.Default)
	}
	if age.HTTP.Location != "query" {
		t.Fatalf("http location = %q, want query", age.HTTP.Location)
	}
	if got, ok := age.HTTP.Default.(int); !ok || got != 22 {
		t.Fatalf("http default = %v, want 22", age.HTTP.Default)
	}

	if got, ok := e.CLIDefaults()["age"].(int); !ok || got != 30 {
		t.Fatalf("CLIDefaults = %v, want age=30", e.CLIDefaults())
	}
	if got, ok := e.HTTPDefaults()["age"].(int); !ok || got != 22 {
		t.Fatalf("HTTPDefaults = %v, want age=22", e.HTTPDefaults())
	}
	if got, ok := e.MCPDefaults()["age"].(int); !ok || got != 25 {
		t.Fatalf("MCPDefaults = %v, want age=25", e.MCPDefaults())
	}

	// Schema default reflects the MCP override, not the global tag value 18.
	if got, ok := e.InputSchema.Properties["age"].Default.(int); !ok || got != 25 {
		t.Fatalf("schema default = %v, want 25 (MCP override)", e.InputSchema.Properties["age"].Default)
	}

	// The global tag binding (required) survives untouched.
	if !findField(t, e.Root, "name").Required {
		t.Fatal("tag-based required lost after hint overlay")
	}
}

func TestHintsInvokeKeepsGlobalDefault(t *testing.T) {
	// Core Invoke applies the GLOBAL default; per-transport defaults are
	// injected by the frontend before calling Invoke, so without injection
	// the tag value 18 must win over the CLI override 30.
	e, err := Define("h2.cmd", hintHandler).
		CLI(CliHints{Fields: map[string]CliFieldHint{"age": {Default: 30}}}).
		Entry()
	if err != nil {
		t.Fatalf("Entry: %v", err)
	}
	out, err := e.Invoke(context.Background(), map[string]any{"name": "ok"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if got := out.(int); got != 18 {
		t.Fatalf("age = %d, want global default 18 (no CLI injection)", got)
	}
}

func TestHintsInvalid(t *testing.T) {
	bad := []struct {
		name string
		cmd  *Command[hintArgs, int]
	}{
		{"unknown key", Define("h3", hintHandler).CLI(CliHints{Fields: map[string]CliFieldHint{"nope": {Shorthand: "x"}}})},
		{"bad shorthand", Define("h4", hintHandler).CLI(CliHints{Fields: map[string]CliFieldHint{"age": {Shorthand: "ab"}}})},
		{"nested key", Define("h5", hintHandler).CLI(CliHints{Fields: map[string]CliFieldHint{"sub.level": {}}})},
		{"bad http location", Define("h6", hintHandler).HTTP(HTTPHints{Fields: map[string]HTTPFieldHint{"age": {Location: "cookie"}}})},
		{"bad default", Define("h7", hintHandler).CLI(CliHints{Fields: map[string]CliFieldHint{"age": {Default: "old"}}})},
	}
	for _, tc := range bad {
		if _, err := tc.cmd.Entry(); err == nil {
			t.Errorf("%s: want error, got nil", tc.name)
		}
	}
}

func TestHintsKeyedByGoName(t *testing.T) {
	// A hint keyed by the Go field name binds to the same field.
	e, err := Define("h8", hintHandler).
		CLI(CliHints{Fields: map[string]CliFieldHint{"Age": {Shorthand: "A"}}}).
		Entry()
	if err != nil {
		t.Fatalf("Entry: %v", err)
	}
	if got := findField(t, e.Root, "age").CLI.Shorthand; got != "A" {
		t.Fatalf("shorthand = %q, want A", got)
	}
}

func TestRegisterDefaultWithoutRegistrar(t *testing.T) {
	old := DefaultRegistrar
	t.Cleanup(func() { DefaultRegistrar = old })
	DefaultRegistrar = nil
	if _, err := Define("h9", hintHandler).RegisterDefault(); err == nil {
		t.Fatal("want error when no default registrar is wired")
	}
}
