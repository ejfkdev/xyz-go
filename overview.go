package xyz

import (
	"fmt"
	"io"

	"github.com/ejfkdev/xyz-go/registry"
)

// printOverview 输出无参数/help 模式下的总览：三种形态 + 内置参数提示 + 命令表
// （CLI 被禁用时隐藏命令表）。
func printOverview(w io.Writer, reg *registry.Registry, serve, mcpWord string, caps Capabilities) {
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
	// CLI 被禁用时不生成子命令，总览也不再列出命令表。
	if len(reg.Names()) == 0 || caps.NoCLI {
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
}
