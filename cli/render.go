package cli

import (
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"time"
)

// Render writes the CLI frontend's default human-readable form of a command
// result:
//
//	nil                 -> nothing
//	string              -> the raw string
//	bool, int, float    -> the bare value
//	time.Time           -> RFC 3339
//	[]basic             -> one element per line
//	struct, *struct     -> aligned "key  value" pairs from json-tagged fields
//	[]struct            -> an aligned table (header + separator + rows)
//	map                 -> sorted "key  value" pairs
//
// The result is never wrapped in an envelope like {"data": ...}: --json
// emits the raw JSON value instead of calling Render.
func Render(w io.Writer, v any) error {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		_, err := fmt.Fprintln(w, rv.Interface())
		return err
	case reflect.Struct:
		if t, ok := rv.Interface().(time.Time); ok {
			_, err := fmt.Fprintln(w, t.Format(time.RFC3339))
			return err
		}
		return renderKV(w, rv)
	case reflect.Slice, reflect.Array:
		if rv.Len() == 0 {
			return nil
		}
		if isStruct(rv.Index(0)) {
			return renderTable(w, rv)
		}
		for i := 0; i < rv.Len(); i++ {
			if _, err := fmt.Fprintln(w, formatCell(rv.Index(i))); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		return renderMap(w, rv)
	default:
		_, err := fmt.Fprintln(w, rv.Interface())
		return err
	}
}

func isStruct(rv reflect.Value) bool {
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return false
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return false
	}
	_, isTime := rv.Interface().(time.Time)
	return !isTime
}

func renderKV(w io.Writer, rv reflect.Value) error {
	keys, vals := kvOf(rv)
	if len(keys) == 0 {
		return nil
	}
	width := 0
	for _, k := range keys {
		if len(k) > width {
			width = len(k)
		}
	}
	for i, k := range keys {
		if _, err := fmt.Fprintf(w, "%-*s  %s\n", width, k, formatCell(vals[i])); err != nil {
			return err
		}
	}
	return nil
}

// kvOf extracts exported json-tagged fields as printable pairs.
func kvOf(rv reflect.Value) ([]string, []reflect.Value) {
	t := rv.Type()
	keys := make([]string, 0, t.NumField())
	vals := make([]reflect.Value, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		name := sf.Name
		if jt, ok := sf.Tag.Lookup("json"); ok {
			if jt == "-" {
				continue
			}
			if n, _, _ := strings.Cut(jt, ","); n != "" {
				name = n
			}
		}
		keys = append(keys, name)
		vals = append(vals, rv.Field(i))
	}
	return keys, vals
}

func renderTable(w io.Writer, rv reflect.Value) error {
	elem := rv.Index(0)
	for elem.Kind() == reflect.Ptr {
		elem = elem.Elem()
	}
	keys, _ := kvOf(elem)
	if len(keys) == 0 {
		return nil
	}
	widths := make([]int, len(keys))
	for i, k := range keys {
		widths[i] = len(k)
	}
	rows := make([][]string, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		e := rv.Index(i)
		for e.Kind() == reflect.Ptr {
			if e.IsNil() {
				e = reflect.Value{}
				break
			}
			e = e.Elem()
		}
		row := make([]string, len(keys))
		if e.IsValid() {
			_, fvals := kvOf(e)
			for j := range keys {
				row[j] = formatCell(fvals[j])
				if len(row[j]) > widths[j] {
					widths[j] = len(row[j])
				}
			}
		}
		rows[i] = row
	}
	if err := writeRow(w, keys, widths); err != nil {
		return err
	}
	dashes := make([]string, len(keys))
	for i, wd := range widths {
		dashes[i] = strings.Repeat("-", wd)
	}
	if err := writeRow(w, dashes, widths); err != nil {
		return err
	}
	for _, row := range rows {
		if err := writeRow(w, row, widths); err != nil {
			return err
		}
	}
	return nil
}

func writeRow(w io.Writer, cells []string, widths []int) error {
	for i, c := range cells {
		if i > 0 {
			if _, err := io.WriteString(w, "  "); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "%-*s", widths[i], c); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

func renderMap(w io.Writer, rv reflect.Value) error {
	keys := rv.MapKeys()
	if rv.Type().Key().Kind() == reflect.String {
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	}
	width := 0
	for _, k := range keys {
		if l := len(fmt.Sprintf("%v", k)); l > width {
			width = l
		}
	}
	for _, k := range keys {
		if _, err := fmt.Fprintf(w, "%-*s  %s\n", width, fmt.Sprintf("%v", k), formatCell(rv.MapIndex(k))); err != nil {
			return err
		}
	}
	return nil
}

// formatCell renders one cell/key-value: pointers deref, time is RFC 3339,
// slices bracket-join, everything else prints via %v.
func formatCell(rv reflect.Value) string {
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return ""
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return ""
	}
	if t, ok := rv.Interface().(time.Time); ok {
		return t.Format(time.RFC3339)
	}
	if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
		var sb strings.Builder
		sb.WriteByte('[')
		for i := 0; i < rv.Len(); i++ {
			if i > 0 {
				sb.WriteByte(' ')
			}
			sb.WriteString(formatCell(rv.Index(i)))
		}
		sb.WriteByte(']')
		return sb.String()
	}
	return fmt.Sprintf("%v", rv.Interface())
}
