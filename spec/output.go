package spec

import (
	"io"
	"net/http"
)

// 逐通道输出函数（extension，deviation register D-go-03）：
// define 链上通过 CliHints/HTTPHints/MCPHints 的 Output 字段设置，让每个
// 通道的输出形态可以完全不同：
//
//   - CLI：开发者全权自定义人类可读输出（富文本、彩色、分页……），
//     替代默认 Render；--json 仍输出原始 JSON（机器模式优先）。
//   - HTTP：全权接管响应（状态码/响应头/任意内容类型）。自定义输出下
//     不再自动套 Content-Type: application/json。
//   - MCP：自定义 textContent 的文本形态（markdown 等）；structuredContent
//     仍由框架从返回值生成，双份契约不变。
//
// 优先级（三端一致）：
//   机器模式标志（--json 等） > Output 自定义 > §12.7 信封投影 > 默认渲染
// 错误路径不经过 Output（分类与状态码/退出码映射保持原样）。
// v 为命令返回值（any），与默认渲染看到的是同一个值。

// CLIOutputFunc 自定义一条命令的 CLI 结果渲染。
type CLIOutputFunc func(w io.Writer, v any) error

// HTTPOutputFunc 自定义一条命令的 HTTP 响应：状态码/响应头/响应体全权
// 交予调用方。
type HTTPOutputFunc func(w http.ResponseWriter, r *http.Request, e *Entry, v any) error

// MCPOutputFunc 自定义一条命令的 MCP 文本内容（textContent 部分）：
// 写入 w 的内容成为唯一的 TextContent；structuredContent 由框架保留。
type MCPOutputFunc func(w io.Writer, v any) error
