package spec

import (
	"fmt"
	"reflect"
)

// Schema is a minimal JSON Schema (draft-07 flavored) document. It is kept
// deliberately small: the MCP inputSchema and the future OpenAPI generation
// only need this subset, and nested structs are inlined rather than put into
// $defs, which MCP servers expect.
type Schema struct {
	Type        string             `json:"type,omitempty"`
	Description string             `json:"description,omitempty"`
	Properties  map[string]*Schema `json:"properties,omitempty"`
	Items       *Schema            `json:"items,omitempty"`
	Required    []string           `json:"required,omitempty"`
	Enum        []any              `json:"enum,omitempty"`
	Default     any                `json:"default,omitempty"`
	Format      string             `json:"format,omitempty"`
}

// buildSchema produces the JSON Schema for the object at the root of the
// argument tree.
func buildSchema(root *FieldMeta) *Schema {
	s := &Schema{Type: "object", Properties: map[string]*Schema{}}
	var required []string
	for _, f := range root.Fields {
		if f.Skip {
			continue
		}
		s.Properties[f.JSONName] = fieldSchema(f)
		if f.Required {
			required = append(required, f.JSONName)
		}
	}
	if len(required) > 0 {
		s.Required = required
	}
	return s
}

func fieldSchema(f *FieldMeta) *Schema {
	switch {
	case f.Type == byteSliceTyp:
		return decorated(f, &Schema{Type: "string", Description: f.Description})
	case f.Type == durationType:
		return decorated(f, &Schema{Type: "string", Format: "duration", Description: f.Description})
	case f.Type == timeType:
		return decorated(f, &Schema{Type: "string", Format: "date-time", Description: f.Description})
	case f.Kind == reflect.Ptr:
		s := fieldSchema(f.Elem)
		if f.Description != "" {
			s.Description = f.Description
		}
		if d := effectiveDefault(f); d != nil {
			s.Default = d
		}
		if len(f.Enum) > 0 {
			s.Enum = f.Enum
		}
		return s
	case f.Kind == reflect.Slice:
		s := &Schema{Type: "array", Description: f.Description}
		s.Items = fieldSchema(f.Elem)
		if d := effectiveDefault(f); d != nil {
			s.Default = d
		}
		return s
	case f.Kind == reflect.Struct:
		s := &Schema{Type: "object", Description: f.Description, Properties: map[string]*Schema{}}
		var required []string
		for _, c := range f.Fields {
			if c.Skip {
				continue
			}
			s.Properties[c.JSONName] = fieldSchema(c)
			if c.Required {
				required = append(required, c.JSONName)
			}
		}
		if len(required) > 0 {
			s.Required = required
		}
		return s
	default:
		switch {
		case isIntKind(f.Kind) || isUintKind(f.Kind):
			return decorated(f, &Schema{Type: "integer", Description: f.Description})
		case isFloatKind(f.Kind):
			return decorated(f, &Schema{Type: "number", Description: f.Description})
		case f.Kind == reflect.Bool:
			return decorated(f, &Schema{Type: "boolean", Description: f.Description})
		default:
			return decorated(f, &Schema{Type: "string", Description: f.Description})
		}
	}
}

// buildOutputSchema 反射命令的返回值类型 R，生成输出 JSON Schema：
// struct→object（复用入参的同款分析管线）、切片→array、时间/标量→基础类型。
// 不支持的类型（接口、map 等）返回错误；调用方可忽略。
func buildOutputSchema(t reflect.Type) (*Schema, error) {
	if t == nil {
		return nil, fmt.Errorf("nil result type")
	}
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch {
	case t == timeType:
		return &Schema{Type: "string", Format: "date-time"}, nil
	case t == durationType:
		return &Schema{Type: "string", Format: "duration"}, nil
	case t == byteSliceTyp:
		return &Schema{Type: "string"}, nil
	case t.Kind() == reflect.Slice:
		items, err := buildOutputSchema(t.Elem())
		if err != nil {
			return nil, err
		}
		return &Schema{Type: "array", Items: items}, nil
	case t.Kind() == reflect.Struct:
		f := &FieldMeta{Name: t.Name(), Type: t, Kind: t.Kind()}
		if err := analyzeStruct(f, t, 0); err != nil {
			return nil, err
		}
		return buildSchema(f), nil
	case isIntKind(t.Kind()) || isUintKind(t.Kind()):
		return &Schema{Type: "integer"}, nil
	case isFloatKind(t.Kind()):
		return &Schema{Type: "number"}, nil
	case t.Kind() == reflect.Bool:
		return &Schema{Type: "boolean"}, nil
	case t.Kind() == reflect.String:
		return &Schema{Type: "string"}, nil
	default:
		return nil, fmt.Errorf("unsupported result kind %s", t.Kind())
	}
}

func decorated(f *FieldMeta, s *Schema) *Schema {
	if d := effectiveDefault(f); d != nil {
		s.Default = d
	}
	if len(f.Enum) > 0 {
		s.Enum = f.Enum
	}
	return s
}

// effectiveDefault hands out the default that belongs in the generated input
// schema: the MCP-specific override wins over the global tag default, since
// InputSchema is the MCP tool's contract.
func effectiveDefault(f *FieldMeta) any {
	if f.MCP.Default != nil {
		return f.MCP.Default
	}
	return f.Default
}

func isIntKind(k reflect.Kind) bool {
	return k >= reflect.Int && k <= reflect.Int64
}

func isUintKind(k reflect.Kind) bool {
	return k >= reflect.Uint && k <= reflect.Uint64
}

func isFloatKind(k reflect.Kind) bool {
	return k == reflect.Float32 || k == reflect.Float64
}
