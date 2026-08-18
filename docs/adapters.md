# 迁移指南：在已有框架里使用 xyz-go

xyz-go 的核心契约是：**所有前端最终都汇入 `Entry.Invoke(ctx, map[string]any) (any, error)` + 统一错误分类**。任何框架的接入都是同一套路——你自己的解析/绑定 → `map[string]any` → `Invoke` → 你想要的渲染与中间件。因此兼容性不需要改动核心，只需要三种「接入档位」：

| 档位 | 用什么 | 你需要放弃什么 | 保留什么 |
|---|---|---|---|
| A. 全替代（你想用回自己的框架） | 定义层 + `Invoke` 脊柱 | 我们的 CLI/HTTP 前端 | 入参定义、校验、默认值分层、错误分类、Schema |
| B. 局部替代（前端各自摘用） | `cli.NewWithOptions` / `httpapi.HandlerFor` / `mcp.Server` | 只有不用的那一两个前端 | 其余前端原样 |
| C. 积木复用（只借零件） | `httpapi.Bearer/CORS/Gzip`、`Render`、`InputSchema/OutputSchema` | 一切 | 只借你需要的零件 |

运行中的行为契约（所有档位通用）：

- `Entry.Root` 是可读的字段树：每个字段的 JSON 名、Go 名、短名、位置参数、env、CLI/HTTP/MCP 绑定与三层默认值都在里面；
- `Entry.Invoke` 是唯一入口：它负责解码（字符串/JSON 都能进）、补默认、校验（含枚举）、执行 handler；
- 错误分类（`errs.Classify`）在你自己框架里继续工作，映射表见主 README；
- `json:"-"` 的注入字段（env/header 专用）以 **Go 字段名**为键送达。

## 档位 A 示例：已有 Cobra 工程

完整可运行代码在 [`examples/cobra`](../examples/cobra)，要点摘录：

```go
// 短名/位置参数/env/描述全部来自 Entry.Root；flag 归约成 map 后交给 Invoke。
func entryToCobra(e *spec.Entry) *cobra.Command {
	cmd := &cobra.Command{Use: lastSeg(e.Name), Short: e.Summary, Aliases: e.CLI.Aliases}
	for _, f := range e.Root.Fields {
		if f.CLI.Positional { /* cobra.Args 计数 */ }
		// Bool → BoolVarP；切片 → StringSliceVarP；其余 → StringVarP
		// 短名 f.CLI.Shorthand、描述 f.Description、默认值 f.CLI.Default/f.Default
	}
	cmd.RunE = func(c *cobra.Command, args []string) error {
		m := map[string]any{}
		// flag 值(Changed)、env(f.CLI.EnvVar)、位置参数 → m
		out, err := e.Invoke(c.Context(), m)
		if err != nil { return err }
		return cli.Render(c.OutOrStdout(), out) // 或你自己的渲染
	}
	return cmd
}
```

迁移检查单：

1. `Use` 取注册名的**最后一段**（cobra 的 Name() 取第一个词，`user.add` 要拆开）；
2. 位置参数用 `cobra.RangeArgs(min, max)`，required 必须是前缀（与 xyz 前端同一约束）；
3. 默认值分两层：`f.Default`（全局）在 Invoke 里自动补，`f.CLI.Default`（CLI 专属）需要你在适配器里接管；
4. `json:"-"` 的字段不生成 flag，env 值以 Go 字段名为键注入；
5. 错误原样 return 给 cobra 即可（`errs.ExitCode` 可自行映射退出码）。

## 档位 B 示例：已有 Gin 服务

完整可运行代码在 [`examples/gin`](../examples/gin)。三条挂载路径：

```go
addEntry, _ := spec.Define("user.add", addUser).
	HTTP(spec.HTTPHints{Method: "POST", Path: "/users/{name}"}).RegisterDefault()

// 1. 单条命令的完整绑定处理器（query/path/header/body + 错误映射）
r.POST("/users/:name", gin.WrapH(httpapi.HandlerFor(addEntry)))

// 2. 复用中间件积木：Bearer/CORS/Gzip 各自独立，按你的安全策略组合
r.POST("/secure/:name", gin.WrapH(httpapi.Bearer([]string{"s3cret"}, httpapi.HandlerFor(addEntry))))

// 3. 整表挂载（含 /openapi.json 与 /healthz）：注意剥前缀
r.Any("/api/*any", gin.WrapH(http.StripPrefix("/api", must(httpapi.Handler(registry.Default)))))
```

### 各框架注意事项

- **gin（≥1.9）**：`http:"path"` 绑定依赖 `r.PathValue`，而 gin 的 `:name` 参数在 `WrapH` 里不会自动写入 Request——加一个全局桥接中间件即可（examples/gin 里有现成的三行 shim：`c.Request.SetPathValue(p.Key, p.Value)`）。
- **echo**：echo 不设置 `r.PathValue`，只能 `c.Param("name")`。用档位 A 打法：在你的 echo handler 里读 `c.Param`、组 map、调 `e.Invoke`，渲染与错误映射自己接（映射表见主 README）。
- **chi / 标准库 ServeMux**：完全兼容——它们本来就是 `http.Handler` 世界，`HandlerFor` / `Handler` + `http.StripPrefix` 直接可挂，无 shim。
- **Nested router 前缀**：任何路径前缀（如 `/api`）都要 `http.StripPrefix` 剥掉再交给 `Handler`，否则内部路由匹配不到 `/openapi.json`。

## 档位 C：借我们的零件

```go
h := httpapi.Bearer([]string{"k"}, httpapi.CORS([]string{"*"}, myHandler)) // 自己的 mux
schema := entry.InputSchema   // 直接消费（OpenAPI 文档、前端表单生成…）
outSchema := entry.OutputSchema
errs.HTTPStatus(errs.Classify(err)) // 你的错误管线里复用映射
```

## 在 xyz 前端本身上扩展（不换框架）

- **注入输出流**：`cli.NewWithOptions(reg, cli.Options{Out: w, ErrOut: ew})` 或 `app.SetOutput(...)`（嵌入大程序/测试必备）。
- **执行中间件**：`app.Use(ExecFunc)` 洋葱链——可以看到解析后的 `args`、改写入参、wrapper 计时/追踪，或**短路自绘**（不调 `next` 即接管渲染）：

```go
app.Use(func(ctx context.Context, ec *cli.ExecContext, args map[string]any, next func() error) error {
	if dryRun { // 自定义输出格式，跳过 Invoke+内置渲染
		fmt.Fprintln(ec.Out, "would invoke", ec.Path, args)
		return nil
	}
	return next()
})
```

- **MCP 底层全开放**：`mcp.Server` 返回官方 SDK 裸 `*Server`，`AddPrompt`/任何 SDK 选项随意加。