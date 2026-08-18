package spec

import (
	"fmt"
	"reflect"
	"strings"
)

// applyHints merges Define-time per-transport field configuration into the
// analyzed field tree. Keys are JSON (or Go) names of top-level argument
// fields; zero-valued hint fields keep the struct tag's binding.
func applyHints(root *FieldMeta, cli map[string]CliFieldHint, http map[string]HTTPFieldHint, mcp map[string]MCPFieldHint) error {
	for key, h := range cli {
		f, err := lookupTopField(root, key)
		if err != nil {
			return fmt.Errorf("cli field %q: %w", key, err)
		}
		if err := applyCliHint(f, key, h); err != nil {
			return err
		}
	}
	for key, h := range http {
		f, err := lookupTopField(root, key)
		if err != nil {
			return fmt.Errorf("http field %q: %w", key, err)
		}
		if err := applyHTTPHint(f, key, h); err != nil {
			return err
		}
	}
	for key, h := range mcp {
		f, err := lookupTopField(root, key)
		if err != nil {
			return fmt.Errorf("mcp field %q: %w", key, err)
		}
		if err := applyMCPHint(f, key, h); err != nil {
			return err
		}
	}
	return nil
}

func lookupTopField(root *FieldMeta, key string) (*FieldMeta, error) {
	if strings.Contains(key, ".") {
		return nil, fmt.Errorf("nested field configuration is not supported yet")
	}
	// JSON 名 → Go 名（精确）→ Go 名（忽略大小写）。第三种便于给 json:"-"
	// 的注入字段（如 env/header 专用）配 hints。
	for _, f := range root.Fields {
		if f.JSONName == key || f.Name == key {
			return f, nil
		}
	}
	for _, f := range root.Fields {
		if strings.EqualFold(f.Name, key) {
			return f, nil
		}
	}
	var names []string
	for _, f := range root.Fields {
		names = append(names, f.JSONName)
	}
	return nil, fmt.Errorf("unknown field %q (fields: %v)", key, names)
}

func applyCliHint(f *FieldMeta, key string, h CliFieldHint) error {
	if h.Shorthand != "" {
		if len(h.Shorthand) != 1 {
			return fmt.Errorf("cli field %q: shorthand must be one character, got %q", key, h.Shorthand)
		}
		f.CLI.Shorthand = h.Shorthand
	}
	if h.Positional {
		f.CLI.Positional = true
	}
	if h.Hidden {
		f.CLI.Hidden = true
	}
	if h.Skip {
		f.CLI.Skip = true
	}
	if h.EnvVar != "" {
		f.CLI.EnvVar = h.EnvVar
	}
	if h.Default != nil {
		d, err := normalizeHintDefault(f.Type, h.Default)
		if err != nil {
			return fmt.Errorf("cli field %q: bad default %v: %w", key, h.Default, err)
		}
		f.CLI.Default = d
	}
	return nil
}

func applyHTTPHint(f *FieldMeta, key string, h HTTPFieldHint) error {
	if h.Location != "" && !validHTTPLocation(h.Location) {
		return fmt.Errorf("http field %q: unknown location %q (want query|path|header|form|body)", key, h.Location)
	}
	if h.Location != "" {
		f.HTTP.Location = h.Location
	}
	if h.Name != "" {
		f.HTTP.Name = h.Name
	}
	if h.Default != nil {
		d, err := normalizeHintDefault(f.Type, h.Default)
		if err != nil {
			return fmt.Errorf("http field %q: bad default %v: %w", key, h.Default, err)
		}
		f.HTTP.Default = d
	}
	return nil
}

func applyMCPHint(f *FieldMeta, key string, h MCPFieldHint) error {
	if h.Default != nil {
		d, err := normalizeHintDefault(f.Type, h.Default)
		if err != nil {
			return fmt.Errorf("mcp field %q: bad default %v: %w", key, h.Default, err)
		}
		f.MCP.Default = d
	}
	return nil
}

func validHTTPLocation(s string) bool {
	switch s {
	case "query", "path", "header", "form", "body":
		return true
	}
	return false
}

// normalizeHintDefault converts a Define-time default — a typed value or a
// parseable string, mirroring the default tag — into a value of type t. It
// reuses the same conversion machinery as decoding, so named types,
// time.Time and time.Duration all behave identically.
func normalizeHintDefault(t reflect.Type, d any) (any, error) {
	if d == nil {
		return nil, nil
	}
	f := &FieldMeta{Type: t}
	if err := analyzeType(f, t, 0); err != nil {
		return nil, err
	}
	v, err := decodeValue(f, d)
	if err != nil {
		return nil, err
	}
	return v.Interface(), nil
}
