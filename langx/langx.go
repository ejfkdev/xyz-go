// Package langx 是内置界面文本的 i18n 层：enum 语言 + 进程级目录 + 用户
// 覆盖。默认英文（xyz 的规范默认）；中文随库携带。选择顺序（根派发器负责
// 落地）：--xyz.lang flag > Config.Lang > LANG/LC_ALL 环境检测 > 英文。
//
// T(key) 返回该语言下的文本；Tf(key, params...) 用 {0}、{1}… 占位符做
// 参数代入（自研最小格式化器，零第三方依赖）。覆盖表（代码侧配置）优先于
// 内置译文；未命中任何语言的键回退到键名本身（绝不 panic）。
package langx

import (
	"os"
	"strings"
	"sync"
)

// Language 是受支持的语言。零值 En（规范默认）。
type Language int

const (
	En Language = iota
	ZhCn
)

// Parse 把 --xyz.lang 的取值映射成语言；未知值返回 false（注册期报错）。
func Parse(s string) (Language, bool) {
	switch s {
	case "en":
		return En, true
	case "zh-CN":
		return ZhCn, true
	default:
		return En, false
	}
}

// String 返回 --xyz.lang 的取值形态。
func (l Language) String() string {
	switch l {
	case ZhCn:
		return "zh-CN"
	default:
		return "en"
	}
}

// Detect 按 LANG/LC_ALL 环境做语言检测：zh 前缀 → 中文，其余（C/POSIX/
// 缺失）→ 英文。
func Detect() Language {
	for _, key := range []string{"LC_ALL", "LANG"} {
		if v := os.Getenv(key); v != "" {
			lower := strings.ToLower(v)
			if strings.HasPrefix(lower, "zh") {
				return ZhCn
			}
			if lower == "c" || lower == "posix" {
				return En
			}
			// 其他地域仍按前缀规则；无 zh 前缀即英文
			return En
		}
	}
	return En
}

var (
	mu        sync.RWMutex
	current   = En
	overrides map[string]string // 当前语言的用户覆盖（nil = 无）
)

// Set 设置进程级语言与可选覆盖表（override 键覆盖内置译文；可为 nil）。
// 根派发器在解析完配置后调用；嵌入场景可自行调用。
func Set(l Language, override map[string]string) {
	mu.Lock()
	defer mu.Unlock()
	current = l
	overrides = override
}

// Lang 返回当前语言（零配置下为 En）。
func Lang() Language {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// T 返回当前语言下 key 的文本：覆盖 > 内置 > 键名回退。
func T(key string) string {
	mu.RLock()
	l, ov := current, overrides
	mu.RUnlock()
	if ov != nil {
		if s, ok := ov[key]; ok {
			return s
		}
	}
	return lookup(l, key)
}

// Tf 是带参数的 T：模板中的 {0}、{1}… 依次被 params 替换。
func Tf(key string, params ...string) string {
	tpl := T(key)
	for i, p := range params {
		tpl = strings.ReplaceAll(tpl, placeholder(i), p)
	}
	return tpl
}

func placeholder(i int) string {
	switch i {
	case 0:
		return "{0}"
	case 1:
		return "{1}"
	case 2:
		return "{2}"
	case 3:
		return "{3}"
	default:
		// 常规消息最多 4 个参数；超出按字面保留。
		return "{}"
	}
}

// lookup 取内置译文（en 为规范文案，zh 为参考译文）。目录未收录的键
// MUST 回退键名本身（绝不返回空串、绝不 panic）。
func lookup(l Language, key string) string {
	table := enTexts
	if l == ZhCn {
		table = zhTexts
	}
	if s, ok := table[key]; ok {
		return s
	}
	return key
}

// 目录：en 为规范（xyz-spec §15.8 的键表），zh-CN 为随库译文。
// 未收录的键回退键名本身。
var enTexts = map[string]string{
	"overview.usage_line":   "Usage (the mode is detected automatically; definitions live in one place):",
	"overview.cli_mode":     "  <app> [command] [args]         CLI mode (subcommands + flags/positionals; -h help, -v version)",
	"overview.serve_mode":   "  <app> {0} [--addr :8080]    HTTP mode (REST routes + /openapi.json + optional /mcp)",
	"overview.mcp_mode":     "  <app> {0} stdio|sse|http    MCP mode (official SDK; --versions pins revisions)",
	"overview.builtins":     "Built-in parameters (xyz.Config in code or on the command line): --xyz.addr=:8080 (default listen address) --xyz.bearer=tok1,tok2 (Bearer credentials for serve and MCP http)",
	"overview.commands":     "Commands:",
	"overview.disabled":     " (disabled)",
	"overview.not_compiled": " (not compiled into this binary)",

	"help.usage":                "Usage:",
	"help.aliases":              "Aliases:",
	"help.commands":             "Commands:",
	"help.flags":                "Flags:",
	"help.global_flags":         "Global Flags:",
	"help.commands_placeholder": "[command]",
	"help.flags_placeholder":    "[flags]",
	"help.json_flag":            "output JSON instead of the human-readable form",
	"help.version_flag":         "print version information",
	"help.help_flag":            "print help",
	"cli.err_positional_count":  "{0}: positional argument count mismatch (want {1} to {2}, got {3})",

	"warn.mode_disabled": "{0} mode was disabled (Config.Capabilities.No{1})",
	"warn.no_cli":        "subcommands unavailable: CLI is disabled (Config.Capabilities.NoCLI; {0}/{1}/help/-v remain available)",
	"warn.bearer_stdio":  "Bearer credential checks only apply to the http/sse transports; stdio is a local process and is not protected",
	"stub.not_compiled":  "this binary was built without the {0} frontend",

	"log.serve_listening": "listening on {0}://{1} (REST + /openapi.json{2})",
	"log.graceful":        "gracefully shut down (ctx cancelled)",
	"log.mcp_listening":   "MCP listening on {0}",
	"log.cors_on":         "CORS enabled: {0}",
	"log.debug_dispatch":  "dispatch: mode word='{0}' addr={1} tokens={2} timeout={3} cors={4}",

	"mcp.usage":                  "usage: mcp stdio|sse|http [--addr :8080] [--versions 2025-06-18,2026-07-28] [--name N] [--server-version V]",
	"mcp.err_missing_transport":  "missing transport",
	"mcp.err_unknown_transport":  "unknown transport {0} (want stdio|sse|http)",
	"mcp.err_sse_removed":        "this SDK removed the legacy HTTP+SSE transport with the 2026-07-28 revision (available: stdio|http)",
	"mcp.err_unknown_version":    "unknown protocol version {0} (known: {1})",
	"mcp.err_empty_version":      "empty protocol version in --versions",
	"mcp.err_transport_versions": "transport {0} cannot serve any of the requested versions {1}",
	"mcp.err_usage_extra_arg":    "unexpected argument {0}",
}

var zhTexts = map[string]string{
	"overview.usage_line":   "用法（模式由程序自动判断，定义只有一份）:",
	"overview.cli_mode":     "  <app> [命令] [参数]           CLI 模式（子命令 + flag/位置参数；-h 帮助，-v 版本）",
	"overview.serve_mode":   "  <app> {0} [--addr :8080]      HTTP 模式（REST 路由 + /openapi.json + 可挂 /mcp）",
	"overview.mcp_mode":     "  <app> {0} stdio|sse|http      MCP 模式（官方 SDK；--versions 限定协议版本）",
	"overview.builtins":     "内置参数（代码中的 xyz.Config 或命令行）：--xyz.addr=:8080（默认监听地址） --xyz.bearer=tok1,tok2（serve 与 MCP http/sse 的 Bearer 凭据）",
	"overview.commands":     "命令:",
	"overview.disabled":     "（已禁用）",
	"overview.not_compiled": "（本二进制未编译）",

	"help.usage":                "Usage:",
	"help.aliases":              "Aliases:",
	"help.commands":             "命令:",
	"help.flags":                "Flags:",
	"help.global_flags":         "Global Flags:",
	"help.commands_placeholder": "[命令]",
	"help.flags_placeholder":    "[flags]",
	"help.json_flag":            "输出 JSON 而不是人类可读格式",
	"help.version_flag":         "输出版本信息",
	"help.help_flag":            "打印帮助",
	"cli.err_positional_count":  "{0}: 位置参数数量不符（需要 {1} 到 {2} 个，收到 {3} 个）",

	"warn.mode_disabled": "{0} 模式已被禁用（Config.Capabilities.No{1}）",
	"warn.no_cli":        "子命令不可用：CLI 已禁用（Config.Capabilities.NoCLI；{0}/{1}/help/-v 仍可用）",
	"warn.bearer_stdio":  "Bearer 凭据校验只作用于 http/sse 传输，stdio 为本地进程不受保护",
	"stub.not_compiled":  "本二进制未编译 {0} 前端",

	"log.serve_listening": "监听 {0}://{1}（REST + /openapi.json{2}）",
	"log.graceful":        "已优雅关停（ctx 取消）",
	"log.mcp_listening":   "MCP 监听 {0}",
	"log.cors_on":         "CORS 开启：{0}",
	"log.debug_dispatch":  "dispatch: mode word={0} addr={1} tokens={2} timeout={3} cors={4}",

	"mcp.usage":                  "用法: mcp stdio|sse|http [--addr :8080] [--versions 2025-06-18,2026-07-28] [--name N] [--server-version V]",
	"mcp.err_missing_transport":  "missing transport",
	"mcp.err_unknown_transport":  "unknown transport {0} (want stdio|sse|http)",
	"mcp.err_sse_removed":        "本 SDK 已随 2026-07-28 修订移除 HTTP+SSE 传输（可用 stdio|http）",
	"mcp.err_unknown_version":    "unknown protocol version {0} (known: {1})",
	"mcp.err_empty_version":      "empty protocol version in --versions",
	"mcp.err_transport_versions": "transport {0} cannot serve any of the requested versions {1}",
	"mcp.err_usage_extra_arg":    "unexpected argument {0}",
}
