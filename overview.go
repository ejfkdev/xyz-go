package xyz

import (
	"fmt"
	"io"
	"strings"

	"github.com/ejfkdev/xyz-go/langx"
	"github.com/ejfkdev/xyz-go/registry"
)

// writeBlock 原样输出自定义帮助块：去掉末尾多余换行后整段 + 单个换行，
// 与后续内容自然分行；空块不输出。
func writeBlock(w io.Writer, s string) {
	if s == "" {
		return
	}
	fmt.Fprintln(w, strings.TrimRight(s, "\n"))
}

// printOverview 输出无参数/help 模式下的总览：各形态用法 + 内置参数提示 +
// 命令表（CLI 被禁用时隐藏命令表）。模式行按「是否被用户命令遮蔽」条件显示：
// 被遮蔽的裸词让位给用户命令（出现在 Commands 表里），其内建模式仍经
// xyz.<词> 可达但不在总览显示（xyz-spec §13.1）。helpBefore/helpAfter 是
// Config 的自定义文本块，分别插在总览开头与结尾（after 即使命令表被隐藏也打印）。
func printOverview(w io.Writer, reg *registry.Registry, m modes, shadowed map[string]bool, cfg Config) {
	writeBlock(w, cfg.HelpBefore)
	caps := cfg.Capabilities
	fmt.Fprintln(w, langx.T("overview.usage_line"))
	// CLI 模式行总是显示（标注禁用/未编译）。
	cliLine := langx.T("overview.cli_mode")
	if caps.NoCLI {
		cliLine += langx.T("overview.disabled")
	} else if !cliFrontend {
		cliLine += langx.T("overview.not_compiled")
	}
	fmt.Fprintln(w, cliLine)
	// serve / http / mcp / help 模式行：被遮蔽则不显示。
	if !shadowed[m.serve] {
		fmt.Fprintln(w, annotateHTTP(langx.Tf("overview.serve_mode", m.serve), caps))
	}
	if !shadowed[m.http] {
		fmt.Fprintln(w, annotateHTTP(langx.Tf("overview.http_mode", m.http), caps))
	}
	if !shadowed[m.mcp] {
		line := langx.Tf("overview.mcp_mode", m.mcp)
		if caps.NoMCP {
			line += langx.T("overview.disabled")
		}
		fmt.Fprintln(w, line)
	}
	if !shadowed[m.help] {
		fmt.Fprintln(w, langx.Tf("overview.help_mode", m.help))
	}
	fmt.Fprintln(w, langx.T("overview.builtins"))
	// CLI 被禁用时不生成子命令，总览也不再列出命令表（自定义 after 块照打）。
	if len(reg.Names()) == 0 || caps.NoCLI {
		writeBlock(w, cfg.HelpAfter)
		return
	}
	fmt.Fprintln(w, "\n"+langx.T("overview.commands"))
	width := 0
	for _, n := range reg.Names() {
		if len(n) > width {
			width = len(n)
		}
	}
	for _, n := range reg.Names() {
		e, _ := reg.Get(n)
		fmt.Fprintf(w, "  %-*s  %s\n", width, n, e.Summary)
	}
	writeBlock(w, cfg.HelpAfter)
}

// annotateHTTP 给 HTTP 系模式行追加禁用/未编译标注。
func annotateHTTP(line string, caps Capabilities) string {
	switch {
	case caps.NoHTTP:
		return line + langx.T("overview.disabled")
	case !httpFrontend:
		return line + langx.T("overview.not_compiled")
	}
	return line
}
