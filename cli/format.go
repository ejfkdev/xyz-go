package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"time"
)

// 本文件实现 --format 的两个非默认渲染器：jsonl 与 markdown。
// json 与 text 复用既有路径（json 走 encoding/json 缩进编码，text 走 Render）。
// 三者优先级与自定义 Output 的关系见 xyz-spec §10.6：显式 --format
//（json/jsonl/markdown）绕过命令的 CLIOutputFunc，text（默认）才进入
// Output > §12.7 信封投影 > Render 链。

// FormatAuto / FormatText / FormatJSON / FormatJSONL / FormatMarkdown 是
// --format 的合法取值常量。FormatAuto（"" 等价）= 按 stdout 是否交互式
// 终端（TTY）自动选择：交互式用 FormatInteractive（默认 text），非交互式
//（管道/重定向/被程序调用）用 FormatPiped（默认 jsonl）。
const (
	FormatAuto     = "auto"
	FormatText     = "text"
	FormatJSON     = "json"
	FormatJSONL    = "jsonl"
	FormatMarkdown = "markdown"
)

// ValidFormat 报告 f 是否为合法的 --format 取值（"" 视为 auto）。
func ValidFormat(f string) bool {
	switch f {
	case "", FormatAuto, FormatText, FormatJSON, FormatJSONL, FormatMarkdown:
		return true
	default:
		return false
	}
}

// RenderJSONL 以 JSON Lines 写出结果：切片/数组每个元素一行紧凑 JSON，
// 其余值整体一行。紧凑（不缩进）、每行以 \n 收尾，适合管道与流式消费。
// nil 不输出任何内容。
func RenderJSONL(w io.Writer, v any) error {
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
	enc := json.NewEncoder(w) // 默认紧凑 + 每次 Encode 追加 \n
	if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
		for i := 0; i < rv.Len(); i++ {
			if err := enc.Encode(rv.Index(i).Interface()); err != nil {
				return err
			}
		}
		return nil
	}
	return enc.Encode(v)
}

// RenderMarkdown 以 Markdown 写出结果：
//
//	nil                 -> 无输出
//	string/bool/数字     -> 裸值（转义 | 与换行）
//	time.Time           -> RFC 3339
//	struct, *struct     -> 两列 | Field | Value | 表
//	[]struct            -> 以结构体字段为列的表
//	[]基本类型           -> "- 元素" 无序列表
//	map                 -> 两列 | Key | Value | 表（字符串键排序）
//
// 单元格内的 | 转义为 \|，换行替换为 <br>，保证表格不破行。
func RenderMarkdown(w io.Writer, v any) error {
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
		_, err := fmt.Fprintln(w, mdEscape(formatCell(rv)))
		return err
	case reflect.Struct:
		if t, ok := rv.Interface().(time.Time); ok {
			_, err := fmt.Fprintln(w, t.Format(time.RFC3339))
			return err
		}
		return mdKV(w, rv)
	case reflect.Slice, reflect.Array:
		if rv.Len() == 0 {
			return nil
		}
		if isStruct(rv.Index(0)) {
			return mdTable(w, rv)
		}
		for i := 0; i < rv.Len(); i++ {
			if _, err := fmt.Fprintf(w, "- %s\n", mdEscape(formatCell(rv.Index(i)))); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		return mdMap(w, rv)
	default:
		_, err := fmt.Fprintln(w, mdEscape(formatCell(rv)))
		return err
	}
}

// mdKV 把单个结构体渲染成两列 | Field | Value | 表。
func mdKV(w io.Writer, rv reflect.Value) error {
	keys, vals := kvOf(rv)
	if len(keys) == 0 {
		return nil
	}
	if _, err := io.WriteString(w, "| Field | Value |\n| --- | --- |\n"); err != nil {
		return err
	}
	for i, k := range keys {
		if _, err := fmt.Fprintf(w, "| %s | %s |\n", mdEscape(k), mdEscape(formatCell(vals[i]))); err != nil {
			return err
		}
	}
	return nil
}

// mdTable 把 []struct 渲染成以字段为列的 Markdown 表。
func mdTable(w io.Writer, rv reflect.Value) error {
	elem := rv.Index(0)
	for elem.Kind() == reflect.Ptr {
		elem = elem.Elem()
	}
	keys, _ := kvOf(elem)
	if len(keys) == 0 {
		return nil
	}
	if _, err := io.WriteString(w, "|"); err != nil {
		return err
	}
	for _, k := range keys {
		if _, err := fmt.Fprintf(w, " %s |", mdEscape(k)); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(w, "\n|"); err != nil {
		return err
	}
	for range keys {
		if _, err := io.WriteString(w, " --- |"); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(w, "\n"); err != nil {
		return err
	}
	for i := 0; i < rv.Len(); i++ {
		e := rv.Index(i)
		for e.Kind() == reflect.Ptr {
			if e.IsNil() {
				e = reflect.Value{}
				break
			}
			e = e.Elem()
		}
		if _, err := io.WriteString(w, "|"); err != nil {
			return err
		}
		if e.IsValid() {
			_, fvals := kvOf(e)
			for j := range keys {
				if _, err := fmt.Fprintf(w, " %s |", mdEscape(formatCell(fvals[j]))); err != nil {
					return err
				}
			}
		} else {
			for range keys {
				if _, err := io.WriteString(w, "  |"); err != nil {
					return err
				}
			}
		}
		if _, err := io.WriteString(w, "\n"); err != nil {
			return err
		}
	}
	return nil
}

// mdMap 把 map 渲染成两列 | Key | Value | 表（字符串键排序）。
func mdMap(w io.Writer, rv reflect.Value) error {
	keys := rv.MapKeys()
	if rv.Type().Key().Kind() == reflect.String {
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	}
	if _, err := io.WriteString(w, "| Key | Value |\n| --- | --- |\n"); err != nil {
		return err
	}
	for _, k := range keys {
		if _, err := fmt.Fprintf(w, "| %s | %s |\n",
			mdEscape(fmt.Sprintf("%v", k)), mdEscape(formatCell(rv.MapIndex(k)))); err != nil {
			return err
		}
	}
	return nil
}

// mdEscape 转义会破坏 Markdown 表格的字符：竖线与换行。
func mdEscape(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "<br>")
	s = strings.ReplaceAll(s, "\n", "<br>")
	s = strings.ReplaceAll(s, "|", `\|`)
	return s
}
