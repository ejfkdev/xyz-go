package xyz

import (
	"fmt"
	"io"
	"strings"

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
	fmt.Fprintln(w, "用法（模式由程序自动判断，定义只有一份）:")
	cliLine := "  <app> [命令] [参数]           CLI 模式（子命令 + flag/位置参数；-h 帮助，-v 版本）"
	if caps.NoCLI {
		cliLine += "（已禁用）"
	} else if !cliFrontend {
		cliLine += "（本二进制未编译）"
	}
	fmt.Fprintln(w, cliLine)
	serveLine := fmt.Sprintf("  <app> %s [--addr :8080]      HTTP 模式（REST 路由 + /openapi.json + 可挂 /mcp）", serve)
	switch {
	case caps.NoHTTP:
		serveLine += "（已禁用）"
	case !httpFrontend:
		serveLine += "（本二进制未编译）"
	}
	fmt.Fprintln(w, serveLine)
	mcpLine := fmt.Sprintf("  <app> %s stdio|sse|http      MCP 模式（官方 SDK；--versions 限定协议版本）", mcpWord)
	if caps.NoMCP {
		mcpLine += "（已禁用）"
	}
	fmt.Fprintln(w, mcpLine)
	fmt.Fprintln(w, "内置参数（代码中的 xyz.Config 或命令行）：--xyz.addr=:8080（默认监听地址） --xyz.bearer=tok1,tok2（serve 与 MCP http/sse 的 Bearer 凭据）")
	// CLI 被禁用时不生成子命令，总览也不再列出命令表（自定义 after 块照打）。
	if len(reg.Names()) == 0 || caps.NoCLI {
		writeBlock(w, helpAfter)
		return
	}
	fmt.Fprintln(w, "\n命令:")
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
