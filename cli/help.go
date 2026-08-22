package cli

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/ejfkdev/xyz-go/langx"
	"github.com/ejfkdev/xyz-go/spec"
)

func (a *App) printHelp(node *cmdNode, bin string) {
	w := a.out
	// 自定义块：只在叶子命令上生效（中间节点没有 CliHints）。
	if node.leaf && node.entry != nil {
		writeHelpBlock(w, node.entry.CLI.Before)
	}
	desc := node.long
	if desc == "" {
		desc = node.short
	}
	if desc != "" {
		fmt.Fprintf(w, "%s\n\n", desc)
	}
	fmt.Fprintln(w, langx.T("help.usage"))
	fmt.Fprintf(w, "  %s", bin)
	if node.leaf {
		// 自定义 usage 是相对父路径的收尾段（"add <name>"），拼上祖先即可。
		if node.usage != "" {
			segs := strings.Split(node.path, ".")
			if prefix := strings.Join(segs[:len(segs)-1], " "); prefix != "" {
				fmt.Fprintf(w, " %s", prefix)
			}
			fmt.Fprintf(w, " %s [flags]", node.usage)
		} else {
			fmt.Fprintf(w, " %s", strings.Join(strings.Split(node.path, "."), " "))
			for _, f := range node.posF {
				fmt.Fprintf(w, " <%s>", f.JSONName)
				if !f.Required {
					fmt.Fprint(w, "?")
				}
			}
			if len(node.defs) > 0 || len(node.posF) == 0 {
				fmt.Fprint(w, " [flags]")
			}
		}
	} else {
		if node.path != "" {
			fmt.Fprintf(w, " %s", strings.Join(strings.Split(node.path, "."), " "))
		}
		fmt.Fprint(w, " "+langx.T("help.commands_placeholder"))
	}
	fmt.Fprintln(w)

	if len(node.aliases) > 0 {
		fmt.Fprintf(w, "\n"+langx.T("help.aliases")+":\n  %s\n", strings.Join(node.aliases, ", "))
	}
	if !node.leaf {
		fmt.Fprintln(w, "\n"+langx.T("help.commands"))
		width := 0
		for _, name := range node.order {
			if len(name) > width {
				width = len(name)
			}
		}
		for _, name := range node.order {
			child := node.children[name]
			if child.hidden {
				continue
			}
			fmt.Fprintf(w, "  %-*s  %s\n", width, name, child.short)
		}
	} else if len(node.defs) > 0 {
		fmt.Fprintln(w, "\n"+langx.T("help.flags"))
		rows := make([][2]string, 0, len(node.defs))
		for _, d := range node.defs {
			name := "--" + d.long
			if d.short != "" {
				name = "-" + d.short + ", " + name
			}
			typ := flagTypeName(d)
			rows = append(rows, [2]string{name + " " + typ, flagDescription(d.field)})
		}
		printRows(w, rows)
	}
	fmt.Fprintln(w, "\n"+langx.T("help.global_flags"))
	printRows(w, [][2]string{
		{"--json", langx.T("help.json_flag")},
		{"-v, --version", langx.T("help.version_flag")},
		{"-h, --help", langx.T("help.help_flag")},
	})
	if node.leaf && node.entry != nil {
		writeHelpBlock(w, node.entry.CLI.After)
	}
}

// writeHelpBlock 原样输出 -h 的自定义文本块：末尾换行归一；空块不输出。
// （与 overview 的 writeBlock 语义一致——帮助块是用户自己的排版。）
func writeHelpBlock(w io.Writer, s string) {
	if s == "" {
		return
	}
	fmt.Fprintln(w, strings.TrimRight(s, "\n"))
}

// flagTypeName 按字段类型保真渲染帮助里的 flag 类型；切片同时标注可重复。
func flagTypeName(d flagDef) string {
	switch d.kind {
	case fBool:
		return "bool"
	case fSlice:
		return "strings (repeatable)"
	}
	switch d.field.Kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	}
	if d.field.Type == reflect.TypeOf(time.Duration(0)) {
		return "duration"
	}
	if d.field.Type == reflect.TypeOf(time.Time{}) {
		return "time"
	}
	return "string"
}

func printRows(w io.Writer, rows [][2]string) {
	width := 0
	for _, r := range rows {
		if len(r[0]) > width {
			width = len(r[0])
		}
	}
	for _, r := range rows {
		if r[1] == "" {
			fmt.Fprintf(w, "  %s\n", r[0])
		} else {
			fmt.Fprintf(w, "  %-*s  %s\n", width, r[0], r[1])
		}
	}
}

// flagDescription 把默认值、env 回退与枚举一起织进帮助描述里。
func flagDescription(f *spec.FieldMeta) string {
	desc := f.Description
	if def := f.CLI.Default; def != nil {
		desc += fmt.Sprintf(" (default %v)", def)
	} else if def := f.Default; def != nil {
		desc += fmt.Sprintf(" (default %v)", def)
	}
	if f.CLI.EnvVar != "" {
		desc += fmt.Sprintf(" (env %s)", f.CLI.EnvVar)
	}
	if len(f.Enum) > 0 {
		vals := make([]string, 0, len(f.Enum))
		for _, e := range f.Enum {
			vals = append(vals, fmt.Sprintf("%v", e))
		}
		desc += " (oneof " + strings.Join(vals, "|") + ")"
	}
	return desc
}

// printCompletion 输出 shell 补全脚本；支持 bash/zsh/fish（未知 shell 报错 2）。
