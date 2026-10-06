// Package langx 是内置界面文本的 i18n 层：enum 语言 + 进程级目录 + 用户
// 覆盖。默认英文（xyz 的规范默认）；中文随库携带。选择顺序（根派发器负责
// 落地）：--xyz.lang flag > Config.Lang > LANG/LC_ALL 环境检测 > 英文。
//
// T(key) 返回该语言下的文本；Tf(key, params...) 用 {0}、{1}… 占位符做
// 参数代入（自研最小格式化器，零第三方依赖）。覆盖表（代码侧配置）优先于
// 内置译文；未命中任何语言的键回退到键名本身（绝不 panic）。
package langx

import (
	"context"
	"os"
	"strconv"
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

// ---- 逐请求语言（context 携带）----
//
// 进程级 current 是 CLI/单语境的默认语言；HTTP 这类并发、逐请求语境需要
// 每个请求带自己的语言（从 Accept-Language 解析）。WithLang 把语言塞进
// context，Tctx/Tfctx 与 FromCtx 据此取值，缺省回退到进程语言。

type langCtxKey struct{}

// WithLang 返回携带指定语言的 context（如 HTTP 从 Accept-Language 解析后）。
func WithLang(ctx context.Context, l Language) context.Context {
	return context.WithValue(ctx, langCtxKey{}, l)
}

// FromCtx 取出 ctx 携带的语言；未携带则回退到当前进程语言，第二返回值为
// false（表示用的是回退值）。
func FromCtx(ctx context.Context) (Language, bool) {
	if ctx != nil {
		if l, ok := ctx.Value(langCtxKey{}).(Language); ok {
			return l, true
		}
	}
	return Lang(), false
}

// Tctx 用 ctx 语言翻译（缺省回退进程语言）：覆盖 > 内置 > 键名回退。
func Tctx(ctx context.Context, key string) string {
	l, _ := FromCtx(ctx)
	mu.RLock()
	ov := overrides
	mu.RUnlock()
	if ov != nil {
		if s, ok := ov[key]; ok {
			return s
		}
	}
	return lookup(l, key)
}

// Tfctx 是带参数的 Tctx：模板中的 {0}、{1}… 依次被 params 替换。
func Tfctx(ctx context.Context, key string, params ...string) string {
	tpl := Tctx(ctx, key)
	for i, p := range params {
		tpl = strings.ReplaceAll(tpl, placeholder(i), p)
	}
	return tpl
}

// ParseAcceptLanguage 解析 HTTP Accept-Language 头，返回最匹配的受支持语言
//（zh* → ZhCn，en* → En），按 q 值择优（缺省 q=1）。无可解析的受支持标签
// 时返回 (En, false)，调用方据此回退到默认语言。示例：
//
//	"zh-CN,zh;q=0.9,en;q=0.8" → (ZhCn, true)
//	"en-US,en;q=0.9"          → (En, true)
//	"fr,de;q=0.9"             → (En, false)  // 无受支持标签
func ParseAcceptLanguage(header string) (Language, bool) {
	bestQ := -1.0
	best := En
	found := false
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tag := part
		q := 1.0
		if semi := strings.Index(part, ";"); semi >= 0 {
			tag = part[:semi]
			for _, param := range strings.Split(part[semi+1:], ";") {
				param = strings.TrimSpace(param)
				if v, ok := strings.CutPrefix(param, "q="); ok {
					if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
						q = f
					}
				}
			}
		}
		tag = strings.ToLower(strings.TrimSpace(tag))
		var l Language
		switch {
		case strings.HasPrefix(tag, "zh"):
			l = ZhCn
		case strings.HasPrefix(tag, "en"):
			l = En
		default:
			continue // 不受支持的标签跳过
		}
		if q > bestQ {
			bestQ, best, found = q, l, true
		}
	}
	return best, found
}

// 目录：en 为规范（xyz-spec §15.8 的键表），zh-CN 为随库译文。
// 未收录的键回退键名本身。
var enTexts = map[string]string{
	"overview.usage_line":   "Usage (the mode is detected automatically; definitions live in one place):",
	"overview.cli_mode":     "  <app> [command] [args]         CLI mode (subcommands + flags/positionals; -h help, -v version)",
	"overview.serve_mode":   "  <app> {0} [--addr :8080]    HTTP mode (REST routes + /openapi.json + /mcp)",
	"overview.http_mode":    "  <app> {0} [--addr :8080]     HTTP mode (REST routes + /openapi.json only; no /mcp)",
	"overview.mcp_mode":     "  <app> {0} stdio|sse|http    MCP mode (official SDK; --versions pins revisions)",
	"overview.help_mode":    "  <app> {0} [command|mode]    detailed help for a command or a mode",
	"overview.builtins":     "Built-in parameters (xyz.Config in code or on the command line): --xyz.addr=:8080 (default listen address) --xyz.bearer=tok1,tok2 (Bearer credentials for serve and MCP http)",
	"overview.commands":     "Commands:",
	"overview.disabled":     " (disabled)",
	"overview.not_compiled": " (not compiled into this binary)",

	"mode_help.serve": "{0}: start the HTTP server — REST routes + /openapi.json + the streamable-HTTP MCP endpoint at /mcp.\n\nFlags (also as --xyz.<name> anywhere):\n  --addr :8080        listen address\n  --bearer tok1,tok2  require Authorization: Bearer <tok>\n  --timeout 45s       read/write/idle timeout\n  --tls-cert/--tls-key  both set → serve HTTPS\n  --cors a,b | *      CORS allowlist\n  --default k=v       channel default (repeatable)\n  --xyz.header k=v    extra response header (repeatable)\n  --xyz.no-server-headers  suppress X-App-*/X-XYZ-* headers\n\nFor REST only (no /mcp), use the http mode. -h/--help prints this.",
	"mode_help.http":  "{0}: start the standalone HTTP server — REST routes + /openapi.json only (no /mcp endpoint).\n\nFlags (also as --xyz.<name> anywhere):\n  --addr :8080        listen address\n  --bearer tok1,tok2  require Authorization: Bearer <tok>\n  --timeout 45s       read/write/idle timeout\n  --tls-cert/--tls-key  both set → serve HTTPS\n  --cors a,b | *      CORS allowlist\n  --default k=v       channel default (repeatable)\n  --xyz.header k=v    extra response header (repeatable)\n  --xyz.no-server-headers  suppress X-App-*/X-XYZ-* headers\n\nFor REST + the /mcp endpoint together, use the serve mode. -h/--help prints this.",
	"mode_help.mcp":   "{0}: start the MCP server (official SDK); one tool per registered command.\n\nUsage: {0} stdio|sse|http [flags]\n  stdio  local process transport\n  sse    legacy HTTP+SSE (revisions ≤ 2025-11-25; absent if the SDK removed it)\n  http   streamable HTTP (2026-07-28 needs --stateless)\n\nFlags: --addr :8080, --versions v1,v2, --name N, --server-version V,\n  --bearer tok1,tok2, --cors a,b, --session-timeout 30m, --default k=v.\n-h/--help prints this.",
	"mode_help.help":  "{0}: print help.\n\nUsage: {0} [command|mode]\n  {0}                 the overview (modes + commands)\n  {0} user.add        detailed help for a command (same as `user add -h`)\n  {0} serve|http|mcp  help for a mode\n\nDotted or space-separated command paths both work (`{0} user.add` == `{0} user add`).",

	"help.usage":                "Usage:",
	"help.aliases":              "Aliases:",
	"help.commands":             "Commands:",
	"help.flags":                "Flags:",
	"help.global_flags":         "Global Flags:",
	"help.commands_placeholder": "[command]",
	"help.flags_placeholder":    "[flags]",
	"help.json_flag":            "output JSON instead of the human-readable form",
	"help.format_flag":          "output format: auto|text|json|jsonl|markdown (auto: text when interactive, jsonl when piped; --json is an alias for --format json)",
	"help.version_flag":         "print version information",
	"help.help_flag":            "print help",
	"cli.err_positional_count":  "{0}: positional argument count mismatch (want {1} to {2}, got {3})",
	"http.err_invalid_json":     "invalid JSON body",
	"http.err_not_found":        "not found",

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
	"overview.serve_mode":   "  <app> {0} [--addr :8080]      HTTP 模式（REST 路由 + /openapi.json + /mcp）",
	"overview.http_mode":    "  <app> {0} [--addr :8080]       HTTP 模式（仅 REST 路由 + /openapi.json，不挂 /mcp）",
	"overview.mcp_mode":     "  <app> {0} stdio|sse|http      MCP 模式（官方 SDK；--versions 限定协议版本）",
	"overview.help_mode":    "  <app> {0} [命令|模式]         查看某命令或某模式的详细帮助",
	"overview.builtins":     "内置参数（代码中的 xyz.Config 或命令行）：--xyz.addr=:8080（默认监听地址） --xyz.bearer=tok1,tok2（serve 与 MCP http/sse 的 Bearer 凭据）",
	"overview.commands":     "命令:",
	"overview.disabled":     "（已禁用）",
	"overview.not_compiled": "（本二进制未编译）",

	"mode_help.serve": "{0}：启动 HTTP 服务器——REST 路由 + /openapi.json + 挂在 /mcp 的流式 HTTP MCP 端点。\n\n旗标（亦可用 --xyz.<名> 置于任意位置）：\n  --addr :8080        监听地址\n  --bearer tok1,tok2  要求 Authorization: Bearer <tok>\n  --timeout 45s       读/写/空闲超时\n  --tls-cert/--tls-key  两者都设 → HTTPS\n  --cors a,b | *      CORS 白名单\n  --default k=v       通道默认参数（可重复）\n  --xyz.header k=v    附加响应头（可重复）\n  --xyz.no-server-headers  抑制 X-App-*/X-XYZ-* 头\n\n只要 REST（不挂 /mcp）请用 http 模式。-h/--help 打印本帮助。",
	"mode_help.http":  "{0}：启动单独的 HTTP 服务器——仅 REST 路由 + /openapi.json（不挂 /mcp 端点）。\n\n旗标（亦可用 --xyz.<名> 置于任意位置）：\n  --addr :8080        监听地址\n  --bearer tok1,tok2  要求 Authorization: Bearer <tok>\n  --timeout 45s       读/写/空闲超时\n  --tls-cert/--tls-key  两者都设 → HTTPS\n  --cors a,b | *      CORS 白名单\n  --default k=v       通道默认参数（可重复）\n  --xyz.header k=v    附加响应头（可重复）\n  --xyz.no-server-headers  抑制 X-App-*/X-XYZ-* 头\n\n要 REST + /mcp 端点一起请用 serve 模式。-h/--help 打印本帮助。",
	"mode_help.mcp":   "{0}：启动 MCP 服务器（官方 SDK）；每个已注册命令一个工具。\n\n用法：{0} stdio|sse|http [旗标]\n  stdio  本地进程传输\n  sse    旧版 HTTP+SSE（修订 ≤ 2025-11-25；若 SDK 已移除则不可用）\n  http   流式 HTTP（2026-07-28 需 --stateless）\n\n旗标：--addr :8080、--versions v1,v2、--name N、--server-version V、\n  --bearer tok1,tok2、--cors a,b、--session-timeout 30m、--default k=v。\n-h/--help 打印本帮助。",
	"mode_help.help":  "{0}：打印帮助。\n\n用法：{0} [命令|模式]\n  {0}                 总览（模式 + 命令）\n  {0} user.add        某命令的详细帮助（等同 `user add -h`）\n  {0} serve|http|mcp  某模式的帮助\n\n命令路径点分或空格分隔皆可（`{0} user.add` == `{0} user add`）。",

	"help.usage":                "Usage:",
	"help.aliases":              "Aliases:",
	"help.commands":             "命令:",
	"help.flags":                "Flags:",
	"help.global_flags":         "Global Flags:",
	"help.commands_placeholder": "[命令]",
	"help.flags_placeholder":    "[flags]",
	"help.json_flag":            "输出 JSON 而不是人类可读格式",
	"help.format_flag":          "输出格式：auto|text|json|jsonl|markdown（auto：交互式出 text、管道出 jsonl；--json 等价 --format json）",
	"help.version_flag":         "输出版本信息",
	"help.help_flag":            "打印帮助",
	"cli.err_positional_count":  "{0}: 位置参数数量不符（需要 {1} 到 {2} 个，收到 {3} 个）",
	"http.err_invalid_json":     "无效的 JSON 请求体",
	"http.err_not_found":        "未找到",

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
