package spec

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"time"
)

// decodeTree binds args into dst (an addressable struct value) according to
// node's metadata: applies defaults for absent fields, enforces required
// presence, checks enum membership. args may be nil.
func decodeTree(node *FieldMeta, args map[string]any, dst reflect.Value) error {
	for _, f := range node.Fields {
		if f.Skip {
			// json:"-" 字段不进常规绑定与 schema，但通道注入的值
			// （CLI env、HTTP header 等）以 Go 字段名作为键仍可送达。
			if raw, ok := args[f.Name]; ok && raw != nil {
				nv, err := decodeValue(f, raw)
				if err != nil {
					return fmt.Errorf("field %s: %w", f.Name, err)
				}
				dst.FieldByIndex(f.Index).Set(nv)
			}
			continue
		}
		raw, present := args[f.JSONName]
		fv := dst.FieldByIndex(f.Index)
		if present && raw != nil {
			nv, err := decodeValue(f, raw)
			if err != nil {
				return fmt.Errorf("field %q: %w", f.JSONName, err)
			}
			if len(f.Enum) > 0 &&
				!slices.ContainsFunc(f.Enum, func(e any) bool { return reflect.DeepEqual(e, nv.Interface()) }) {
				return fmt.Errorf("field %q: value must be one of %v", f.JSONName, f.Enum)
			}
			fv.Set(nv)
			continue
		}
		if f.Default != nil {
			nv, err := decodeValue(f, f.Default)
			if err != nil {
				return fmt.Errorf("field %q: invalid default: %w", f.JSONName, err)
			}
			fv.Set(nv)
			continue
		}
		if f.Required {
			return fmt.Errorf("field %q is required", f.JSONName)
		}
	}
	return nil
}

// decodeValue converts raw — any transport's value representation — into a
// value of f.Type, recursively for pointers, slices and nested structs.
// Accepted scalar sources are strings (CLI), Go numbers, and JSON-decoded
// float64/json.Number (HTTP body, MCP).
func decodeValue(f *FieldMeta, raw any) (reflect.Value, error) {
	t := f.Type
	if raw == nil {
		return reflect.Zero(t), nil
	}
	if rtv := reflect.TypeOf(raw); rtv.AssignableTo(t) {
		return reflect.ValueOf(raw), nil
	}
	switch {
	case t == timeType:
		s, ok := raw.(string)
		if !ok {
			return reflect.Value{}, fmt.Errorf("expect RFC3339 string, got %T", raw)
		}
		tm, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(tm), nil
	case t == durationType:
		switch v := raw.(type) {
		case string:
			d, err := time.ParseDuration(v)
			if err != nil {
				return reflect.Value{}, err
			}
			return reflect.ValueOf(d), nil
		case float64:
			return reflect.ValueOf(time.Duration(v)), nil
		case json.Number:
			i, err := v.Int64()
			if err != nil {
				return reflect.Value{}, err
			}
			return reflect.ValueOf(time.Duration(i)), nil
		default:
			return reflect.Value{}, fmt.Errorf("expect duration string, got %T", raw)
		}
	case t == byteSliceTyp:
		if v, ok := raw.(string); ok {
			return reflect.ValueOf([]byte(v)), nil
		}
		rv := reflect.ValueOf(raw)
		if rv.Kind() != reflect.Slice {
			return reflect.Value{}, fmt.Errorf("expect string or byte array, got %T", raw)
		}
		out := make([]byte, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			b, err := byteFrom(rv.Index(i).Interface())
			if err != nil {
				return reflect.Value{}, fmt.Errorf("index %d: %w", i, err)
			}
			out[i] = b
		}
		return reflect.ValueOf(out), nil
	}
	switch t.Kind() {
	case reflect.Ptr:
		out := reflect.New(t.Elem())
		ev, err := decodeValue(f.Elem, raw)
		if err != nil {
			return reflect.Value{}, err
		}
		out.Elem().Set(ev)
		return out, nil
	case reflect.Slice:
		rv := reflect.ValueOf(raw)
		if rv.Kind() != reflect.Slice {
			return reflect.Value{}, fmt.Errorf("expect array, got %T", raw)
		}
		out := reflect.MakeSlice(t, rv.Len(), rv.Len())
		for i := 0; i < rv.Len(); i++ {
			ev, err := decodeValue(f.Elem, rv.Index(i).Interface())
			if err != nil {
				return reflect.Value{}, fmt.Errorf("index %d: %w", i, err)
			}
			out.Index(i).Set(ev)
		}
		return out, nil
	case reflect.Struct:
		m, ok := raw.(map[string]any)
		if !ok {
			return reflect.Value{}, fmt.Errorf("expect object, got %T", raw)
		}
		out := reflect.New(t).Elem()
		if err := decodeTree(f, m, out); err != nil {
			return reflect.Value{}, err
		}
		return out, nil
	default:
		return scalarValue(t, raw)
	}
}

func byteFrom(raw any) (byte, error) {
	switch v := raw.(type) {
	case byte:
		return v, nil
	case int:
		if v < 0 || v > 255 {
			return 0, fmt.Errorf("byte out of range: %d", v)
		}
		return byte(v), nil
	case int64:
		if v < 0 || v > 255 {
			return 0, fmt.Errorf("byte out of range: %d", v)
		}
		return byte(v), nil
	case float64:
		if v < 0 || v > 255 || v != math.Trunc(v) {
			return 0, fmt.Errorf("byte out of range: %v", v)
		}
		return byte(v), nil
	case json.Number:
		i, err := v.Int64()
		if err != nil || i < 0 || i > 255 {
			return 0, fmt.Errorf("byte out of range: %v", raw)
		}
		return byte(i), nil
	default:
		return 0, fmt.Errorf("expect number, got %T", raw)
	}
}

// scalarValue converts raw into the scalar type t. It handles named scalar
// types (e.g. type Port int) and rejects lossy numeric conversions
// (3.7 -> int) with a clear error.
func scalarValue(t reflect.Type, raw any) (reflect.Value, error) {
	switch t.Kind() {
	case reflect.String:
		var s string
		switch v := raw.(type) {
		case string:
			s = v
		case json.Number:
			s = v.String()
		default:
			return reflect.Value{}, fmt.Errorf("expect string, got %T", raw)
		}
		return reflect.ValueOf(s).Convert(t), nil
	case reflect.Bool:
		var b bool
		switch v := raw.(type) {
		case bool:
			b = v
		case string:
			var err error
			b, err = strconv.ParseBool(v)
			if err != nil {
				return reflect.Value{}, fmt.Errorf("expect boolean, got %q", v)
			}
		default:
			return reflect.Value{}, fmt.Errorf("expect boolean, got %T", raw)
		}
		return reflect.ValueOf(b).Convert(t), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := rawInt64(raw)
		if err != nil {
			return reflect.Value{}, err
		}
		if z := reflect.Zero(t); z.OverflowInt(i) {
			return reflect.Value{}, fmt.Errorf("value %d overflows %s", i, t)
		}
		return reflect.ValueOf(i).Convert(t), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := rawUint64(raw)
		if err != nil {
			return reflect.Value{}, err
		}
		if z := reflect.Zero(t); z.OverflowUint(u) {
			return reflect.Value{}, fmt.Errorf("value %d overflows %s", u, t)
		}
		return reflect.ValueOf(u).Convert(t), nil
	case reflect.Float32, reflect.Float64:
		fl, err := rawFloat64(raw)
		if err != nil {
			return reflect.Value{}, err
		}
		if z := reflect.Zero(t); z.OverflowFloat(fl) {
			return reflect.Value{}, fmt.Errorf("value %v overflows %s", fl, t)
		}
		return reflect.ValueOf(fl).Convert(t), nil
	default:
		return reflect.Value{}, fmt.Errorf("unsupported scalar kind %s", t.Kind())
	}
}

func rawInt64(raw any) (int64, error) {
	switch v := raw.(type) {
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case uint:
		if uint64(v) > math.MaxInt64 {
			return 0, fmt.Errorf("value %d out of int64 range", v)
		}
		return int64(v), nil
	case uint64:
		if v > math.MaxInt64 {
			return 0, fmt.Errorf("value %d out of int64 range", v)
		}
		return int64(v), nil
	case float64:
		if v != math.Trunc(v) {
			return 0, fmt.Errorf("expect integer, got %v", v)
		}
		if v > math.MaxInt64 || v < math.MinInt64 {
			return 0, fmt.Errorf("value %v out of int64 range", v)
		}
		return int64(v), nil
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return 0, fmt.Errorf("expect integer, got %v", v)
		}
		return i, nil
	case string:
		i, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("expect integer, got %q", v)
		}
		return i, nil
	default:
		return 0, fmt.Errorf("expect integer, got %T", raw)
	}
}

func rawUint64(raw any) (uint64, error) {
	switch v := raw.(type) {
	case uint:
		return uint64(v), nil
	case uint8:
		return uint64(v), nil
	case uint16:
		return uint64(v), nil
	case uint32:
		return uint64(v), nil
	case uint64:
		return v, nil
	case int8:
		if v < 0 {
			return 0, fmt.Errorf("expect unsigned integer, got %d", v)
		}
		return uint64(v), nil
	case int16:
		if v < 0 {
			return 0, fmt.Errorf("expect unsigned integer, got %d", v)
		}
		return uint64(v), nil
	case int32:
		if v < 0 {
			return 0, fmt.Errorf("expect unsigned integer, got %d", v)
		}
		return uint64(v), nil
	case int:
		if v < 0 {
			return 0, fmt.Errorf("expect unsigned integer, got %d", v)
		}
		return uint64(v), nil
	case int64:
		if v < 0 {
			return 0, fmt.Errorf("expect unsigned integer, got %d", v)
		}
		return uint64(v), nil
	case float64:
		if v != math.Trunc(v) || v < 0 {
			return 0, fmt.Errorf("expect unsigned integer, got %v", v)
		}
		if v > math.MaxUint64 {
			return 0, fmt.Errorf("value %v out of uint64 range", v)
		}
		return uint64(v), nil
	case json.Number:
		u, err := strconv.ParseUint(v.String(), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("expect unsigned integer, got %v", v)
		}
		return u, nil
	case string:
		u, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("expect unsigned integer, got %q", v)
		}
		return u, nil
	default:
		return 0, fmt.Errorf("expect unsigned integer, got %T", raw)
	}
}

func rawFloat64(raw any) (float64, error) {
	switch v := raw.(type) {
	case float32:
		return float64(v), nil
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case json.Number:
		fl, err := v.Float64()
		if err != nil {
			return 0, fmt.Errorf("expect number, got %v", v)
		}
		return fl, nil
	case string:
		fl, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, fmt.Errorf("expect number, got %q", v)
		}
		return fl, nil
	default:
		return 0, fmt.Errorf("expect number, got %T", raw)
	}
}
