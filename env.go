package xyz

import (
	"context"

	"github.com/ejfkdev/xyz-go/langx"
	"github.com/ejfkdev/xyz-go/termx"
)

// 运行时环境上下文（xyz-spec §14 嵌入面 / §15 体验约定）：把「本次运行的
// 命令行语言、是否交互式终端、是否去色」等环境信息以公开、稳定的接口暴露给
// handler、自定义 Output 函数、Execute 中间件与宿主程序，无需各自重复探测。
//
// 这些是*进程级 / CLI 语境*的语义：派发开始后由根派发器解析落定（语言经
// --xyz.lang > Config.Lang > LANG/LC_ALL > en）。HTTP/MCP 服务语境下
// Interactive 恒为 false（不是终端）、Language 是进程默认；HTTP 的逐请求
// 语言（Accept-Language）另经请求 context 提供（见 httpapi 与 langx.Tctx）。

// EnvContext 是一次环境上下文的快照。字段只增不改语义，便于将来扩展
//（如色彩模式、应用身份、解析后的输出格式）。
type EnvContext struct {
	// Language 是解析后的 UI 语言："en" | "zh-CN"。
	Language string
	// Interactive 报告进程默认输出（os.Stdout）是否为交互式终端（TTY）。
	Interactive bool
	// NoColor 报告环境是否要求去色（NO_COLOR 非空或 TERM=dumb）——样式轴
	// 的输入，与 Interactive 正交（真实终端也可能 NoColor）。
	NoColor bool
}

// Env 返回当前进程的环境上下文快照。
func Env() EnvContext {
	return EnvContext{
		Language:    Language(),
		Interactive: Interactive(),
		NoColor:     termx.NoColor(),
	}
}

// Language 返回解析后的 UI 语言字符串（"en" | "zh-CN"）。派发后是
// --xyz.lang > Config.Lang > LANG/LC_ALL > en 的结果；未派发的纯嵌入场景
// 返回 langx 的当前值（默认 en，可用 langx.Set 预设）。
func Language() string { return langx.Lang().String() }

// LanguageFromCtx 返回 ctx 携带的逐请求语言字符串（"en" | "zh-CN"）。HTTP
// 前端把从 Accept-Language 解析的语言放进请求 ctx（缺省回退进程默认语言），
// handler 由此拿到本次请求该有的语言并本地化自己的输出；ctx 未携带时回退到
// 进程语言（等价 Language()）。
func LanguageFromCtx(ctx context.Context) string {
	l, _ := langx.FromCtx(ctx)
	return l.String()
}

// Interactive 报告进程默认输出（os.Stdout）是否为交互式终端（TTY）。
// 嵌入且使用自定义 writer 时，逐次执行的权威值见 cli.ExecContext.Interactive
//（二者同源于 termx.Interactive）。
func Interactive() bool { return termx.StdoutInteractive() }

// NoColor 报告环境是否要求去色（NO_COLOR 非空或 TERM=dumb）。为将来的彩色
// 渲染器（样式轴）预留；宿主现在即可查询以自行决定上色。
func NoColor() bool { return termx.NoColor() }
