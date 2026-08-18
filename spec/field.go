package spec

import (
	"fmt"
	"reflect"
	"strings"
	"time"
)

var (
	timeType     = reflect.TypeOf(time.Time{})
	durationType = reflect.TypeOf(time.Duration(0))
	byteSliceTyp = reflect.TypeOf([]byte(nil))
)

// maxAnalyzeDepth guards against recursive types (e.g. a linked-list node
// referencing itself), which cannot map onto wiring formats.
const maxAnalyzeDepth = 10

// FieldMeta describes one field of the argument struct tree. Struct fields
// populate Fields; pointer and slice element types populate Elem. Only
// declared fields carry Description/Default/Enum/Validate.
type FieldMeta struct {
	Name     string // Go field name
	JSONName string // wire name from the json tag
	Type     reflect.Type
	Kind     reflect.Kind
	Index    []int // index path from the owning struct, for FieldByIndex

	Description string
	Required    bool
	Secret      bool // redact this value in help text, logs and error echoes
	Validate    string
	rules       []vrule // Validate 的解析结果（库内 validator 使用）
	Enum        []any
	Default     any // parsed, typed default; nil means no default

	Skip bool // json:"-": excluded from binding and schema

	CLI  CliField
	HTTP HTTPField
	MCP  MCPField

	Elem   *FieldMeta   // element or pointee type when Kind is Ptr or Slice
	Fields []*FieldMeta // children when Kind is Struct
}

// CliField holds the field-level bindings the CLI frontend reads.
type CliField struct {
	Shorthand  string // single character, e.g. "n"
	Positional bool   // consumes the next positional argument
	Hidden     bool   // excluded from --help listings
	Skip       bool   // cli:"-": invisible to the CLI frontend
	EnvVar     string // fall back to this environment variable when unset
	Default    any    // CLI-only default; overrides the global default tag for the CLI frontend
}

// HTTPField holds the field-level bindings the HTTP frontend reads.
type HTTPField struct {
	Location string // "", query, path, header, form, body
	Name     string // httpName override, e.g. "X-Custom-Header"
	Default  any    // HTTP-only default; overrides the global default tag for the HTTP frontend
}

// MCPField holds the field-level bindings the MCP frontend reads.
type MCPField struct {
	Default any // MCP-only default; also replaces the global default in the input schema
}

// analyzeStruct walks an argument struct and fills node.Fields.
func analyzeStruct(node *FieldMeta, t reflect.Type, depth int) error {
	if depth > maxAnalyzeDepth {
		return fmt.Errorf("argument struct nesting exceeds %d levels (recursive type?)", maxAnalyzeDepth)
	}
	node.Type = t
	node.Kind = t.Kind()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		if sf.Anonymous {
			return fmt.Errorf("field %s: embedded structs are not supported yet", sf.Name)
		}
		f := &FieldMeta{Name: sf.Name, Type: sf.Type, Kind: sf.Type.Kind(), Index: sf.Index}
		if err := parseFieldTags(sf, f); err != nil {
			return fmt.Errorf("field %s: %w", sf.Name, err)
		}
		if f.Skip && f.Required {
			return fmt.Errorf("field %s: required conflicts with json:\"-\"", sf.Name)
		}
		if err := analyzeKind(f, depth); err != nil {
			return fmt.Errorf("field %s: %w", sf.Name, err)
		}
		node.Fields = append(node.Fields, f)
	}
	return nil
}

// analyzeKind classifies a declared field's type and recurses into pointer,
// slice and struct shapes.
func analyzeKind(f *FieldMeta, depth int) error {
	switch {
	case f.Type == timeType || f.Type == durationType || f.Type == byteSliceTyp:
		return nil // typed leaves
	case f.Kind == reflect.Ptr:
		elem := &FieldMeta{Type: f.Type.Elem()}
		if err := analyzeType(elem, f.Type.Elem(), depth+1); err != nil {
			return fmt.Errorf("element type: %w", err)
		}
		f.Elem = elem
		return nil
	case f.Kind == reflect.Slice:
		elem := &FieldMeta{Type: f.Type.Elem()}
		if err := analyzeType(elem, f.Type.Elem(), depth+1); err != nil {
			return fmt.Errorf("element type: %w", err)
		}
		f.Elem = elem
		return nil
	case f.Kind == reflect.Struct:
		nested := &FieldMeta{Name: f.Name, Type: f.Type, Kind: f.Kind}
		if err := analyzeStruct(nested, f.Type, depth+1); err != nil {
			return err
		}
		f.Fields = nested.Fields
		return nil
	case unsupportedKind(f.Kind):
		return fmt.Errorf("unsupported kind %s", f.Kind)
	default:
		return nil // scalar leaf
	}
}

// analyzeType walks element and pointee types; these carry no struct tags.
func analyzeType(f *FieldMeta, t reflect.Type, depth int) error {
	if depth > maxAnalyzeDepth {
		return fmt.Errorf("argument type nesting exceeds %d levels (recursive type?)", maxAnalyzeDepth)
	}
	f.Type = t
	f.Kind = t.Kind()
	switch {
	case t == timeType || t == durationType || t == byteSliceTyp:
		return nil
	case f.Kind == reflect.Ptr, f.Kind == reflect.Slice:
		elem := &FieldMeta{Type: t.Elem()}
		if err := analyzeType(elem, t.Elem(), depth+1); err != nil {
			return err
		}
		f.Elem = elem
		return nil
	case f.Kind == reflect.Struct:
		nested := &FieldMeta{Type: t, Kind: t.Kind()}
		if err := analyzeStruct(nested, t, depth+1); err != nil {
			return err
		}
		f.Fields = nested.Fields
		return nil
	case unsupportedKind(f.Kind):
		return fmt.Errorf("unsupported kind %s", f.Kind)
	default:
		return nil
	}
}

func unsupportedKind(k reflect.Kind) bool {
	switch k {
	case reflect.Map, reflect.Chan, reflect.Func, reflect.Interface,
		reflect.UnsafePointer, reflect.Complex64, reflect.Complex128, reflect.Uintptr:
		return true
	}
	return false
}

func parseFieldTags(sf reflect.StructField, f *FieldMeta) error {
	st := sf.Tag
	f.JSONName = sf.Name
	if v, ok := st.Lookup("json"); ok && v == "-" {
		f.Skip = true
	} else if ok {
		if n, _, _ := strings.Cut(v, ","); n != "" {
			f.JSONName = n
		}
	}
	if v, ok := st.Lookup("desc"); ok {
		f.Description = v
	}
	if v, ok := st.Lookup("secret"); ok {
		if v != "true" {
			return fmt.Errorf(`secret tag accepts only "true", got %q`, v)
		}
		f.Secret = true
	}
	if v, ok := st.Lookup("required"); ok {
		if v != "true" {
			return fmt.Errorf(`required tag accepts only "true", got %q`, v)
		}
		f.Required = true
	}
	if v, ok := st.Lookup("validate"); ok {
		f.Validate = v
		rules, err := parseValidateTag(v)
		if err != nil {
			return fmt.Errorf("validate tag: %w", err)
		}
		f.rules = rules
	}
	if v, ok := st.Lookup("cli"); ok {
		if err := parseCliTag(v, f); err != nil {
			return err
		}
	}
	if v, ok := st.Lookup("http"); ok {
		if err := parseHTTPTag(v, f); err != nil {
			return err
		}
	}
	if v, ok := st.Lookup("httpName"); ok {
		f.HTTP.Name = v
	}
	if v, ok := st.Lookup("default"); ok {
		d, err := parseDefault(f.Type, v)
		if err != nil {
			return fmt.Errorf("bad default %q: %w", v, err)
		}
		f.Default = d
	}
	if v, ok := st.Lookup("enum"); ok {
		if err := parseEnum(f, v); err != nil {
			return err
		}
	}
	return nil
}

func parseCliTag(v string, f *FieldMeta) error {
	if v == "-" {
		f.CLI.Skip = true
		return nil
	}
	for _, tok := range strings.Split(v, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		switch {
		case tok == "positional":
			f.CLI.Positional = true
		case tok == "hidden":
			f.CLI.Hidden = true
		case strings.HasPrefix(tok, "shorthand="):
			sh := strings.TrimPrefix(tok, "shorthand=")
			if len(sh) != 1 {
				return fmt.Errorf("cli shorthand must be one character, got %q", sh)
			}
			f.CLI.Shorthand = sh
		case strings.HasPrefix(tok, "env="):
			f.CLI.EnvVar = strings.TrimPrefix(tok, "env=")
			if f.CLI.EnvVar == "" {
				return fmt.Errorf("cli env requires a variable name")
			}
		default:
			return fmt.Errorf("unknown cli option %q (want positional|hidden|shorthand=X|env=X|-)", tok)
		}
	}
	return nil
}

func parseHTTPTag(v string, f *FieldMeta) error {
	if !validHTTPLocation(v) {
		return fmt.Errorf("unknown http location %q (want query|path|header|form|body)", v)
	}
	f.HTTP.Location = v
	return nil
}

// parseDefault converts a default tag string into a typed value of t.
func parseDefault(t reflect.Type, s string) (any, error) {
	switch {
	case t == timeType:
		return time.Parse(time.RFC3339, s)
	case t == durationType:
		return time.ParseDuration(s)
	case t == byteSliceTyp:
		return []byte(s), nil
	case t.Kind() == reflect.Ptr:
		return parseDefault(t.Elem(), s)
	case t.Kind() == reflect.Slice:
		parts := strings.Split(s, ",")
		out := make([]any, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			v, err := scalarValue(t.Elem(), p)
			if err != nil {
				return nil, fmt.Errorf("element %q: %w", p, err)
			}
			out = append(out, v.Interface())
		}
		return out, nil
	default:
		v, err := scalarValue(t, s)
		if err != nil {
			return nil, err
		}
		return v.Interface(), nil
	}
}

func parseEnum(f *FieldMeta, v string) error {
	if f.Kind == reflect.Ptr || f.Kind == reflect.Struct || f.Kind == reflect.Slice ||
		f.Type == durationType || f.Type == timeType {
		return fmt.Errorf("enum is only supported on scalar fields, got %s", f.Type)
	}
	parts := strings.Split(v, ",")
	f.Enum = make([]any, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return fmt.Errorf("enum contains an empty value")
		}
		s, err := scalarValue(f.Type, p)
		if err != nil {
			return fmt.Errorf("enum value %q: %w", p, err)
		}
		f.Enum = append(f.Enum, s.Interface())
	}
	return nil
}
