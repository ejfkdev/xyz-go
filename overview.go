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

// printOverview 输出无参数/help 模式下的总览：三种形态 + 内置参数提示 + 命令表
// （CLI 被禁用时隐藏命令表）。helpBefore/helpAfter 是 Config 的自定义文本块，
// 分别插在总览开头与结尾（after 即使命令表被隐藏也打印）。
func printOverview(w io.Writer, reg *registry.Registry, serve, mcpWord string, caps Capabilities, helpBefore, helpAfter string) {
	writeBlock(w, helpBefore)
	fmt.Fprintln(w, langx.T("overview.usage_line"))
	cliLine := langx.T("overview.cli_mode")
	if caps.NoCLI {
		cliLine += langx.T("overview.disabled")
	} else if !cliFrontend {
		cliLine += langx.T("overview.not_compiled")
	}
	fmt.Fprintln(w, cliLine)
	serveLine := langx.Tf("overview.serve_mode", serve)
	switch {
	case caps.NoHTTP:
		serveLine += langx.T("overview.disabled")
	case !httpFrontend:
		serveLine += langx.T("overview.not_compiled")
	}
	fmt.Fprintln(w, serveLine)
	mcpLine := langx.Tf("overview.mcp_mode", mcpWord)
	if caps.NoMCP {
		mcpLine += langx.T("overview.disabled")
	}
	fmt.Fprintln(w, mcpLine)
	fmt.Fprintln(w, langx.T("overview.builtins"))
	// CLI 被禁用时不生成子命令，总览也不再列出命令表（自定义 after 块照打）。
	if len(reg.Names()) == 0 || caps.NoCLI {
		writeBlock(w, helpAfter)
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
	writeBlock(w, helpAfter)
}
