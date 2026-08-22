package xyz

import (
	"time"

	"github.com/ejfkdev/xyz-go/logx"
)

// 派发配置：模式词、能力开关与内置参数。零值即默认可直接使用；
// 命令行 --xyz.* 与各模式的裸名 flag 优先级高于这里的字段值。

// ModeWords renames the built-in mode keywords. Fields left empty keep
// their defaults (serve, mcp, help).
type ModeWords struct {
	Serve string // 默认 "serve"
	MCP   string // 默认 "mcp"
	Help  string // 默认 "help"
}

// Capabilities switches the channels on and off at runtime (independently
// of build tags). The zero value keeps every channel enabled. Disabling a
// channel only removes its own runtime path: the mode words (serve, mcp,
// help) and -v/--version keep working, and the disabled mode answers with a
// clear error. Disabled config methods still compile — they merely stop
// being consumed.
type Capabilities struct {
	NoCLI  bool // 不在命令注册表上生成子命令（mcp/serve/help/-v 仍可用）
	NoMCP  bool // mcp 模式不可用（stdio/sse/http 都拒绝）
	NoHTTP bool // serve 模式不可用
}

// Config adjusts the dispatcher. The zero value keeps every default.
type Config struct {
	Modes        ModeWords
	Capabilities Capabilities

	// Addr 是 serve 与 mcp(http/sse) 模式的默认监听地址（各模式自己的
	// --addr flag 优先）。
	Addr string
	// BearerTokens 开启 serve REST 与 MCP http/sse 传输的 Bearer 凭据校验，
	// 每个元素是一个可接受的 token；空表示不校验。命令行写法：
	// --xyz.bearer=tok1,tok2（stdio 传输为本地进程，不受影响）。
	BearerTokens []string

	// LogLevel 是库自身诊断的日志级别（logx 输出到 stderr）。
	// 零值（LevelUnset）保持默认 Info。命令行：--xyz.log-level=debug|info|warn|error。
	LogLevel logx.Level
	// Timeout 是 serve 模式的读/写/空闲超时；0 表示只保留 10s 的请求头超时。
	Timeout time.Duration
	// CertFile/KeyFile 同时给定则 serve 以 TLS 监听（--xyz.tls-cert/--xyz.tls-key）。
	CertFile string
	KeyFile  string
	// CORSOrigins 非空则开启 CORS：逐个 Origin 放行（"*" 表示任意来源），
	// OPTIONS 预检在鉴权之前应答。命令行：--xyz.cors=origin1,origin2。
	CORSOrigins []string

	// Lang 覆盖界面语言：""=自动（--xyz.lang flag > 本字段 > LANG/LC_ALL
	// 环境检测 > 英文默认）。取值 "en" | "zh-CN"。
	Lang string
	// Translations 是用户的多语言内容覆盖表：语言 → (消息键 → 文本)。
	// 键名见 langx 目录（xyz-spec §15.8 的规范键表）；只覆盖内置键亦可。
	Translations map[string]map[string]string

	// ChannelDefaults 是 serve/mcp 启动时注入的一批通道级默认参数
	// （字段线上名 → 字符串值）：请求/调用未显式提供时自动补上，优先级
	// 高于全局 default tag、低于显式入参与接口默认。命令行：
	// --default k=v（可重复/逗号分隔对），代码侧写入本表。
	ChannelDefaults map[string]string

	// HelpBefore/HelpAfter 是 help 总览的自定义文本块：前者原样插在总览
	// 开头（程序名/描述/版本/仓库地址等自己拼），后者插在结尾（命令表之后，
	// 即使命令表被隐藏也打印）。空 = 不插入。
	HelpBefore string
	HelpAfter  string
}
