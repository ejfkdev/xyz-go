# xyz-go — 一次定义，三通道调用

> 🌐 语言 / Language: [English](README.md) · **中文（当前页）**

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev/dl/)
[![MCP Protocol](https://img.shields.io/badge/MCP-2024%E2%80%932026--07--28-0764e0?style=flat)](https://modelcontextprotocol.io/specification/2026-07-28)
[![Dependencies](https://img.shields.io/badge/核心包-零第三方依赖-2ea44f?style=flat)](#依赖原则与体积)

一个 Go 命令工具箱：**只定义一次**命令（入参结构 + 校验 + 各通道细节），同一个二进制自动获得 **CLI 子命令**、**HTTP REST 服务**（附 OpenAPI 文档）与 **MCP 工具服务**（官方 SDK）三种运行形态，模式由库自动判断。

```go
import "github.com/ejfkdev/xyz-go" // 包名是 xyz，引用名自动为 xyz

func main() {
	xyz.Define("user.add", addUser).
		Summary("创建用户").
		CLI(xyz.CliHints{Usage: "add <name>", Fields: map[string]xyz.CliFieldHint{
			"age": {Shorthand: "a", Default: 20},
		}}).
		HTTP(xyz.HTTPHints{Method: "POST", Path: "/users/{name}"}).
		MCP(xyz.MCPHints{Annotations: []string{"write"}}).
		Also(xyz.Define("math.sum", sum).Summary("两数求和")).
		Run() // 注册 + 派发 + 按结果退出，到此为止
}
```

```console
$ example user add bob -a 25          # CLI：子命令 + 短名 flag
id    1
name  bob
age   25

$ example user list                   # []struct 渲染成对齐表格
id  name   age
--  -----  ---
1   alice  18

$ curl -s -X POST localhost:8080/users/alice -d '{"age":9}'
{"id":2,"name":"alice","age":9}

$ example mcp stdio                   # MCP：注册的命令即工具（stdio/SSE/streamable HTTP）
```

## 特性

- **一次定义，零样板**：整个 main 就是一条 `xyz.Define(...)...Run()` 链，无需 `os.Exit`、无需手动构造注册表、无需分发开关。
- **三通道共用一条管线**：CLI（字符串）、HTTP（JSON）、MCP arguments 先归约成同一输入形状，再走「解码 → 默认值 → 校验 → handler」的唯一路径，行为不漂移。
- **逐通道精细配置**：短名、别名、env 注入、绑定位置、**通道专属默认值**（全局默认 → 通道覆盖两级分层）。
- **无信封响应**：基础类型裸输出、struct 键值对齐、`[]struct` 表格、`--json` 翻转；HTTP 裸 JSON；MCP `structuredContent` + `textContent` 双份。
- **统一错误分类**：一条 `errs.New(errs.KindNotFound, ...)` 同时驱动 CLI 退出码、HTTP 状态码、MCP 错误码。
- **依赖洁癖**：核心包（spec/registry/errors/cli/httpapi/根包）**零第三方依赖**；唯一第三方树是官方 MCP SDK，可用 `-tags nomcp` 整体剔除；按通道裁剪后最小约 3.9M。
- **协议版本可控**：MCP 支持 2024-11-05 至 2026-07-28 五个规范版本，`--versions` 一键限定；工具同时携带反射生成的 `outputSchema`（OpenAPI 响应 schema 同源）。
- **生产友好**：SIGINT/SIGTERM 优雅关停（context 贯穿到 handler）、`/healthz` 探活、gzip、CLI 帮助内联默认值/env/枚举、`completion bash|zsh|fish`。
- **内置参数开箱即用**：凭据（`--bearer`）、默认地址、日志级别、超时、TLS、CORS 统一在 `xyz.Config` 与 `--xyz.*` 命名空间，模式词即命名空间（`serve --bearer=...`）。

## 安装

```bash
go get github.com/ejfkdev/xyz-go
```

要求 Go ≥ 1.25（由官方 MCP SDK 的版本指令决定）。完整可运行示例见 [cmd/example](cmd/example/main.go)（11 条命令覆盖全部常用写法），内部机制导览见 [cmd/tour](cmd/tour/main.go)。

## 一次定义：入参 struct 与全局契约

入参 struct 上的标签是**所有通道共享**的契约（名称、描述、默认值、必填、枚举、校验、机密性）：

| 标签 | 含义 | 示例 |
|---|---|---|
| `json:"name"` | 线格式字段名；`-` 表示排除出绑定与 schema（仍可经 env/header 按 Go 字段名注入） | `json:"user_name"` |
| `desc:"..."` | 字段描述（CLI 帮助、JSON Schema description 通用） | `desc:"用户名"` |
| `default:"..."` | 全局默认值，按字段类型解析，可被通道覆盖 | `default:"18"` |
| `required:"true"` | 必须提供 | `required:"true"` |
| `enum:"a,b"` | 只允许列举值（写入 schema 并在解码层强制） | `enum:"fast,slow"` |
| `validate:"..."` | 校验规则（库内 validator，见下方支持集） | `validate:"min=2,email"` |
| `secret:"true"` | 敏感字段：help/日志/错误回显需打码 | `secret:"true"` |
| `cli:"..."` | CLI 绑定：`shorthand=a`、`positional`、`hidden`、`env=VAR`、`-` | `cli:"shorthand=a,env=TOKEN"` |
| `http:"query"` | HTTP 绑定：`query`（**未标注时默认**）/ `path` / `header` / `form` / `body` | `http:"header"` |
| `httpName:"X-Key"` | HTTP 通道线上名覆盖（常用作 header 名） | `httpName:"X-Api-Key"` |

`validate` 支持：`required`、`omitempty`、`min`、`max`、`len`、`gt`、`gte`、`lt`、`lte`、`oneof`、`email`（go-playground 语法的兼容子集；不支持的规则在**注册期**报错，不会静默忽略）。

类型支持：全部标量及命名标量（`type Port int`）、`[]T`、`[]byte`、`*T`、嵌套 struct、`time.Time`、`time.Duration`；map/接口/匿名嵌入/递归类型在注册期明确拒绝。

## 通道精细配置与默认值分层

`Define` 链上的 `CLI()/HTTP()/MCP()` 既配命令级细节，也可通过 `Fields` 映射按字段覆盖 tag（两层自动合并，覆盖层零值 = 沿用 tag）：

```go
CLI(xyz.CliHints{
	Usage:   "add <name>",             // 帮助里的用法行
	Aliases: []string{"ua", "new"},    // 等同子命令名
	Fields: map[string]xyz.CliFieldHint{
		"age":   {Shorthand: "a"},        // 短名（也可写 tag）
		"mode":  {Default: "fast"},       // 只有 CLI 有默认值
		"token": {EnvVar: "APP_TOKEN"},   // env 回退
	},
})
```

**同一字段的默认值优先级**（以 CLI 为例）：

```
flag 显式传值 > env 回退 > CLI 专属默认值 > 全局 tag 默认值（Invoke 补齐）> 零值
```

机制：各前端在调 `Invoke` 前注入自己的覆盖默认（`Entry.CLIDefaults()/HTTPDefaults()/MCPDefaults()`），核心管线只有一条。MCP 的覆盖默认同时替换 `inputSchema` 里的 `default`（schema 是 MCP 的契约）。

## 三种运行形态

```
example [命令] [参数]           CLI：子命令树、短名/别名/-h/-v/--json/位置参数/env
example serve --addr :8080      HTTP：REST 路由 + /openapi.json + 同端口 /mcp
example mcp stdio|sse|http      MCP：官方 SDK，三种传输（--versions 限定协议版本）
example completion bash|zsh|fish  内置 shell 补全脚本
```

**CLI**（纯标准库实现）：注册名 `user.add` 生成两级子命令 `user add`；`-h/--help` 逐命令帮助（内联 `(default …)`/`(env …)`/`(oneof …)` 提示），`-v/--version` 输出版本（`xyz.Version` 变量，可用 `-ldflags "-X github.com/ejfkdev/xyz-go.Version=v1.2.3"` 注入）；`completion bash|zsh|fish` 生成补全脚本。

**HTTP**（纯标准库实现）：路由即 `HTTPHints{Method, Path}`（`{name}` 为路径参数）；未标注 `http:` 的字段默认从查询串绑定，JSON body 合并为入参基底；状态码由错误分类映射（400/401/403/404/409/500），错误响应 `{"error":"..."}`；`GET /openapi.json` 输出由同一 `InputSchema` 生成的 OpenAPI 3 文档（含同源的响应 schema）；`GET /healthz` 探活、`Accept-Encoding: gzip` 自动压缩。未声明路由的命令不会出现在 REST 里。

**MCP**（官方 SDK）：命令即工具，`tools/list` 直接下发反射生成的 inputSchema 与 **outputSchema**；成功返回双份内容——`structuredContent`（裸 JSON）+ `textContent`（CLI 同款文本）；失败返回 `isError:true` + 分类消息。http/sse 传输与 `serve` 一样支持 `--xyz.bearer` 凭据校验。支持的规范版本：`2024-11-05`、`2025-03-26`、`2025-06-18`、`2025-11-25`、`2026-07-28`（最新为无握手 `server/discover` 世代），`mcp http --versions 2025-06-18,2026-07-28` 可限定；内建约束：SSE 传输只服务 ≤2025-11-25，streamable HTTP 服务 2026-07-28 需 `--stateless`。

### 响应呈现（无信封）

| 返回类型 | CLI | `--json` / HTTP / MCP structuredContent |
|---|---|---|
| `nil` / nil 指针 | 无输出 | JSON `null` |
| `string` / `bool` / 数字 | 裸值一行 | 裸 JSON 值 |
| `time.Time` | RFC3339 | RFC3339 字符串 |
| `[]基础类型` | 每行一个 | JSON 数组 |
| `struct` | 对齐 `key  value` 两列 | JSON 对象 |
| `[]struct` | 对齐表格（表头+分隔线） | JSON 数组 |
| `map` | 按键排序键值对 | JSON 对象 |

### 逐通道输出函数（自定义渲染）

每个通道可通过 channel hints 上的 `Output` 字段**独立**覆盖渲染——三个
通道输出形态可以完全不同，且无需动核心。三端优先级一致：**机器模式
（`--json` 等）> Output 自定义 > §12.7 信封投影 > 默认渲染**；错误路径
不经过 Output（状态码/退出码仍按分类映射）。`v` 与默认渲染看到的是同一
个返回值。

```go
xyz.Define("user.add", addUser).Summary("创建用户").
	CLI(xyz.CliHints{
		Output: func(w io.Writer, v any) error { // 富文本/彩色/分页，随你发挥
			return 自定义渲染(w, v)
		},
	}).
	HTTP(xyz.HTTPHints{
		Method: "POST", Path: "/users/{name}",
		Output: func(w http.ResponseWriter, r *http.Request, e *spec.Entry, v any) error {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(201)
			_, err := fmt.Fprintf(w, "created: %v", v)
			return err
		},
	}).
	MCP(xyz.MCPHints{
		Output: func(w io.Writer, v any) error { // 自定义 textContent（如 markdown）
			_, err := fmt.Fprintf(w, "## result: %v", v)
			return err
		}, // structuredContent 仍由框架生成，双份契约不变
	}).
	Run()
```

被程序调用（管道、非 `--json`）时，让 Output 自行回落纯文本（例如改为
调用默认 `cli.Render`）；更稳妥的做法是把各前端的机器形态（`--json`、
HTTP 响应体、MCP structuredContent）当作稳定契约，人类形态仅作呈现层。

## 错误分类

handler 返回 `errs "github.com/ejfkdev/xyz-go/errors"` 分类错误，一次分类驱动三个通道：

| Kind | HTTP | CLI 退出码 | JSON-RPC / MCP |
|---|---|---|---|
| `invalid_input`（解码/校验失败自动归类） | 400 | 2 | -32602 |
| `unauthorized` | 401 | 1 | -32010 |
| `forbidden` | 403 | 1 | -32011 |
| `not_found` | 404 | 1 | -32001 |
| `conflict` | 409 | 1 | -32009 |
| `unavailable` | 503 | 1 | -32603 |
| 未分类（兜底 `internal`） | 500 | 1 | -32603 |

## 配置：模式词与能力开关

所有派发配置集中在 `xyz.Config`，零值即默认；链式用 `.Configure(cfg)`，函数式用 `MainConfig/RunConfig`：

```go
xyz.MainConfig(xyz.Config{
	// serve/mcp/help 是保留词，可整体改名（原词解禁，可当普通命令）
	Modes: xyz.ModeWords{Serve: "httpd", MCP: "protocol", Help: "assist"},
	// 禁用通道只移除其运行路径：mcp/serve/help/-v 壳能力永远保留
	Capabilities: xyz.Capabilities{NoCLI: true, NoMCP: true, NoHTTP: true},
})
```

`NoCLI` 禁用后不再生成用户子命令，但 `mcp stdio` / `serve` / `help` / `-v` 照常可用（总览会标注「已禁用」并隐藏命令表）；被禁用通道的 `CLI()/HTTP()/MCP()` 配置照常编译与执行，只是不再被消费。没有任何已注册命令的 registry 是静默空操作（退出 0）。

## 内置配置（--xyz.* 与 Config 字段）

库自身的配置集中在 `xyz.Config` 字段与命令行 `--xyz.*` 命名空间，三个来源按优先级：**命令行 > 代码 Config > 内置默认**。

**已实现的内置参数：**

| 参数 | 代码字段 | 语义 |
|---|---|---|
| `--bearer=tok1,tok2`（或 `--bearer tok`） | `Config.BearerTokens` | 开启 **serve REST** 与 **MCP http/sse** 传输的 Bearer 凭据校验：`Authorization: Bearer <tok>` 命中任一 token 放行，否则 401 + `{"error":"unauthorized"}`；空 = 不校验。stdio 为本地进程不受影响（会打印提醒） |
| `--addr=:8080` | `Config.Addr` | serve 与 mcp(http/sse) 的默认监听地址 |
| `--log-level=debug`（或 `--xyz.log-level`） | `Config.LogLevel` | 库自身诊断日志（stderr，`xyz[level]:` 前缀）：`debug`/`info`/`warn`/`error`，默认 `info`。命令结果与用法错误不受影响 |
| `--timeout=45s` | `Config.Timeout` | serve 的读/写/空闲超时；0 = 仅 10s 请求头超时 |
| `--tls-cert/--tls-key` | `Config.CertFile/KeyFile` | serve 同时给定则改为 TLS 监听 |
| `--cors=https://a,b`（或 `*`） | `Config.CORSOrigins` | serve 与 MCP http/sse 的 CORS 白名单；预检在鉴权之前应答（浏览器预检不带凭据） |
| `--session-timeout=30m`（仅 mcp） | `mcp.Options.SessionTimeout` | 流式 HTTP 的空闲会话过期（官方 SDK SessionTimeout） |

命名规范：**`serve`/`mcp` 是模式词，本身就构成命名空间**，所以内置参数用裸名（`--bearer`/`--addr`/`--cors`/…）；这些都可用全局前缀形式（`--xyz.*`）写在任意位置（等效，方便脚本统一注入）。参数优先级：**模式局部 flag > 全局 --xyz.* / 代码 Config > 内置默认**。模式词可改名（见上一节），改名后命名空间随之迁移——库内文案不写死 `serve`/`mcp` 字样。

```bash
example serve --bearer=s3cret                        # 裸名：REST/openapi/mcp 全部要求凭据
example mcp http --addr :9000 --bearer a,b           # MCP 独立服务同款
example mcp http --xyz.bearer a,b                    # 前缀形式等效
```

```go
xyz.MainConfig(xyz.Config{BearerTokens: []string{"s3cret"}, Addr: ":8080"})
```

**各前端已有的自有参数**：CLI `--json`、`-h`/`-v`；MCP `--versions/--name/--server-version/--addr/--json-response/--stateless`；HTTP `serve --addr`。

**已实现**（对应上表）：~~日志级别~~ ✅、~~HTTP/MCP 会话超时~~ ✅、~~TLS~~ ✅、~~CORS~~ ✅。

**待审议清单**（保持内核最小、按需补丁）：日志输出轮转（目前 stderr 直出、无文件）、基础限流。需要哪项直接说，逐项加即可。

## 界面语言

所有内置界面文本（总览、帮助标签、用法错误、诊断）的语言按此顺序选择：
`--xyz.lang=en|zh-CN` flag > `Config.Lang` > `LANG`/`LC_ALL` 环境检测
（小写 `zh` 前缀即中文）> **英文（默认）**。两种语言随库携带。更多多语言
内容用 `Config.Translations` 配置（语言 →（消息键 → 文本））；键名与英文
措辞即 xyz-spec §15.5 的规范目录：

```go
xyz.MainConfig(xyz.Config{
    Lang: "zh-CN", // 或 LANG=zh_CN ./app；或 --xyz.lang=zh-CN
    Translations: map[string]map[string]string{
        "en": {"help.help_flag": "show this help"},
    },
})
```

错误分类消息（§8）保持英文；用户内容（summary、description、帮助块）永不翻译。

## 命令通道、长驻命令与可组合派发

- **单命令通道开关**：`CliHints{Skip}`、`HTTPHints{Skip}`、`MCPHints{Skip}`
  只把该命令从被标记的通道移除（不建子命令节点 / 不路由 / 不成为工具）。
  CLI 的 `Skip` 同时移除别名与 completion 词——比 `Hidden` 更强。
- **长驻命令**：`CliHints{Daemon: true}` 声明长驻生命周期——handler 阻塞到
  `ctx.Done()`，CLI 优雅退出 0，返回值不渲染，命令隐含 CLI-only。

  ```go
  spec.Define("watch", watch).CLI(spec.CliHints{Daemon: true})
  func watch(ctx context.Context, in *args) (*resp, error) { <-ctx.Done(); return nil, nil }
  ```

- **通道默认参数**：`--default k=v`（可重复；逗号分隔对）——serve/mcp 启动
  时注入，只补缺席的请求/工具键；优先级：显式 > env > 接口默认 > **通道
  默认** > 全局默认 > 零值。serve/mcp 里任何未识别的 `--key value` 都是它的
  简写：`gs serve --index ./wiki` 等价 `--default index=./wiki`。代码侧：
  `Config.ChannelDefaults`。

- **可组合派发**：`code, handled := xyz.TryRun(reg, args)`——完整派发管线，
  但 CLI 顶层词未命中时静默返回 `(0, false)`，宿主接着路由自己的命令；
  `TryRunConfig` 可带自定义 `Config`。

## 自定义帮助块

纯文本自由块，多行原样输出（末尾多余换行归一为一个）；空块零影响：

```go
// 总览开头/结尾（Config）——程序名、描述、版本、仓库地址等自己拼
xyz.MainConfig(xyz.Config{
    HelpBefore: "udf v1.0.0 — 磁盘镜像查看工具\nhttps://github.com/example/udf",
    HelpAfter:  "更多示例: https://github.com/example/udf#examples",
})
// 每条命令的 -h（CliHints）：
CLI(xyz.CliHints{Before: "extract — 解包镜像", After: "仓库: https://…"})
```

块位置：`Before` 在 `-h` 最前（description 之前）、`After` 在最末（Global
Flags 之后），仅叶子命令生效；`HelpBefore`/`HelpAfter` 环绕 `help` 总览
（after 块在命令表被隐藏时也打印）。不规定命名块种类——示例、版本行、
仓库地址等一切内容都是用户自己的文本。

## 嵌入式与多注册表

单例链之外，还有显式注册表的纯函数路径（返回退出码、不结束进程，适合嵌入自己的服务、单元测试、多注册表）：

```go
reg := registry.New()
spec.Define("user.add", addUser).Summary("...").Register(reg) // spec 路径同理
os.Exit(xyz.Run(reg, os.Args[1:]))                             // 需要 defer 清理时这样写

srv := mcp.Server(reg, mcp.Options{Versions: []string{mcp.ProtocolV2026_07_28}})
h, _ := httpapi.Handler(reg)      // 整表路由（自带 /healthz 与 /openapi.json）；单条用 httpapi.HandlerFor(entry) 挂任意路由器
app, _ := cli.NewWithOptions(reg, cli.Options{Out: w, ErrOut: ew}) // 可注入输出流；app.SetOutput / app.Use(执行中间件) 同理

mcp.RunContext(ctx, reg, args)
cli.RunContext(ctx, reg, args)
```

已在用 Cobra / Gin / Echo / chi？见[迁移指南](docs/adapters.md)，配套可运行示例 [examples/cobra](examples/cobra) 与 [examples/gin](examples/gin)：三种共存档位（换前端、挂单条处理器、借中间件积木），无需触碰核心。

## 依赖原则与体积

- **核心包零第三方依赖**：`go.mod` 的直接依赖只有官方 MCP SDK（`github.com/modelcontextprotocol/go-sdk`）一棵树；CLI 与 HTTP 前端均为标准库实现，无 mimetype、无语言包。
- **构建标签自由裁剪**（任意组合）：

| 构建标签 | 通道 | 体积（`-s -w -trimpath` 剥离后，cmd/example 实测） |
|---|---|---|
| （默认） | CLI + HTTP + MCP | 8.3M |
| `-tags nomcp` | CLI + HTTP | 6.5M |
| `-tags nocli` | HTTP + MCP | 8.3M |
| `-tags nohttp` | CLI + MCP | 7.9M |
| `-tags nomcp,nohttp` | 仅 CLI | 4.1M |
| `-tags nocli,nomcp,nohttp` | 纯嵌入 | 3.9M |

```bash
go build -ldflags "-s -w" -trimpath -o example ./cmd/example
go build -tags nomcp,nohttp -ldflags "-s -w" -trimpath -o example ./cmd/example
```

体积构成（剥离后）：Go 运行时地板 ≈1.1M（自包含静态链接的设计取舍，任何 Go 程序都有）+ 库本体（fmt/json/反射/示例自身）≈2.8M + HTTP（net/http、TLS 与 gzip）≈2M + MCP SDK ≈3~5M（随命令数波动）。裁剪掉的通道对应整块移除；被裁剪通道的调用会明确报错并退出 1。

## 项目结构

| 包 | 职责 | 依赖 |
|---|---|---|
| `/`（根包 `xyz`） | 链式 Builder、模式分派、能力开关、内置参数（`--xyz.*`）、版本 | 标准库 |
| `/spec` | 泛型定义、字段反射、解码管线、校验、JSON Schema | 标准库 |
| `/registry` | 命令表：注册、冲突检测、默认单例 | 标准库 |
| `/errors` | 错误分类及三通道映射 | 标准库 |
| `/cli` | CLI 前端：命令树、flag 解析、帮助、渲染 | 标准库 |
| `/httpapi` | HTTP 前端：路由、入参绑定、openapi.json | 标准库 |
| `/logx` | 分级诊断日志（stderr，`xyz[level]:` 前缀） | 标准库 |
| `/mcp` | MCP 前端：三种传输、协议版本 | 官方 SDK |
| `/cmd/example`、`/cmd/tour` | 示例与导览 | — |

## 设计原则

1. **默认路径零样板，显式路径不缺席**：链式单例是主入口；注册表参数化的纯函数是嵌入与测试的后门——两条路共用同一派发与调用管线。
2. **注册期即报错**：名字/类型/标签/路由冲突/不支持的校验规则全部在启动即表面化，不拖到运行时。
3. **配置是数据，通道是消费方**：`CLI()/HTTP()/MCP()` 只是往元数据里存数据，因此构建标签与能力开关任意裁剪都不影响代码编译。
4. **渲染没有信封、有自然形态**：机器读 JSON、人类读表格，同一份返回值。
5. **壳能力不可裁**：`help`/`-v`/模式词/`completion` 在任何组合下可用。
6. **模式词即命名空间**：`serve`/`mcp` 下的内置参数用裸名（`--bearer`/`--addr`/`--cors`），任意位置可用 `--xyz.*` 全局形式；优先级 = 模式局部 > 全局 / 代码 Config > 内置默认。库内文案不写死模式词——改名后命名空间随之迁移。
7. **取消语义贯通**：分发入口持有信号 ctx，一路流入 CLI/HTTP/MCP 的 handler；HTTP 服务在退出前优雅排空。

## 开发

```bash
go vet ./... && go test ./...     # 单测覆盖全部 8 个库包
go test -tags "nocli nomcp nohttp" ./...   # 各构建变体同样通过
go run ./cmd/example              # 全家桶示例
go run ./cmd/tour                 # 内部机制导览
```

输出契约：命令结果一律走 stdout；错误与库诊断一律走 stderr（后者带 `xyz[level]:` 前缀，级别见 `--log-level`），stdio 传输下 stdout 仅供协议帧使用。handler 收到的 `context.Context` 在收到 SIGINT/SIGTERM 时取消（serve 会先 `Shutdown` 排空在途请求再退出）。

### 发布

```bash
git tag v0.1.0 && git push origin v0.1.0   # 消费者 go get github.com/ejfkdev/xyz-go@v0.1.0
```