// 教学导览：展示三通道绑定、默认值分层和 schema 生成的内部视图。
// 与 cmd/example（真实二进制形态）互补。Run with: go run ./cmd/tour
package main

import (
	"context"
	"encoding/json"
	"fmt"

	errs "github.com/ejfkdev/xyz-go/errors"
	"github.com/ejfkdev/xyz-go/registry"
	"github.com/ejfkdev/xyz-go/spec"
)

// AddUserArgs 是唯一的入参定义。
// 全局契约（所有通道生效）放 tag：json/desc/default/required/enum/validate/secret。
// 通道绑定也可以放 tag（http:"query"、cli:"shorthand=m"），
// 或在 Define 时用 CLI().Fields / HTTP().Fields / MCP().Fields 覆盖。
type AddUserArgs struct {
	Name  string   `json:"name" desc:"用户名称" required:"true" validate:"min=2" cli:"positional" http:"path"`
	Age   int      `json:"age" desc:"年龄" default:"18" http:"query"`
	Mode  string   `json:"mode" desc:"部署模式" enum:"fast,slow" cli:"shorthand=m" http:"query"`
	Tags  []string `json:"tags" desc:"标签" http:"query"`
	Limit *int     `json:"limit" desc:"返回条数上限" http:"query"`
	Token string   `json:"-" secret:"true" desc:"令牌" cli:"env=ACM_TOKEN"`
}

type AddUserResp struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Age  int    `json:"age"`
	Mode string `json:"mode"`
}

var idCounter int64

func addUser(_ context.Context, in *AddUserArgs) (*AddUserResp, error) {
	if in.Name == "missing" {
		return nil, errs.New(errs.KindNotFound, "no such user")
	}
	idCounter++
	return &AddUserResp{ID: idCounter, Name: in.Name, Age: in.Age, Mode: in.Mode}, nil
}

type SearchArgs struct {
	Query string   `json:"query" desc:"关键词" required:"true"`
	K     int      `json:"k" desc:"返回条数" default:"10"`
	Tags  []string `json:"tags" desc:"过滤标签"`
}

func search(_ context.Context, in *SearchArgs) ([]string, error) {
	return []string{in.Query, "...", fmt.Sprintf("top %d", in.K)}, nil
}

func main() {
	reg := registry.New()

	// —— 定义一次：三种通道的配置集中在这里 ——
	userEntry, err := spec.
		Define("user.add", addUser).
		Summary("创建用户").
		CLI(spec.CliHints{
			Usage: "add <name>",
			Fields: map[string]spec.CliFieldHint{
				"age":  {Shorthand: "a", Default: 20}, // CLI 专属默认值（覆盖全局 18）
				"mode": {Default: "fast"},             // 只有 CLI 才有这个默认值
			},
		}).
		HTTP(spec.HTTPHints{
			Method: "POST",
			Path:   "/users",
			Fields: map[string]spec.HTTPFieldHint{
				"age": {Default: 21}, // 覆盖全局默认 18，只对 HTTP 生效
			},
		}).
		MCP(spec.MCPHints{Annotations: []string{"write"}}).
		Register(reg)
	must(err)

	searchEntry, err := spec.
		Define("search.query", search).
		Summary("搜索文档").
		CLI(spec.CliHints{
			Fields: map[string]spec.CliFieldHint{
				"k": {Default: 25}, // CLI 覆盖全局默认 10
			},
		}).
		HTTP(spec.HTTPHints{Method: "GET", Path: "/search"}).
		Register(reg)
	must(err)

	ctx := context.Background()

	fmt.Println("== 注册的命令 ==")
	for _, n := range reg.Names() {
		e, _ := reg.Get(n)
		fmt.Printf("  %-14s %s\n", n, e.Summary)
	}

	fmt.Printf("\n== %s 的 JSON Schema（这是 MCP 的契约；OpenAPI 以后也吃它） ==\n", userEntry.Name)
	schemaJSON, err := json.MarshalIndent(userEntry.InputSchema, "", "  ")
	must(err)
	fmt.Println(string(schemaJSON))

	fmt.Println("\n== 每个字段在三个通道的绑定与默认值总览 ==")
	printFields(userEntry)
	printFields(searchEntry)

	fmt.Println("\n== 三种运行形态，各注入自己的通道默认值后走同一条管线 ==")
	fmt.Println("模拟 CLI 前端：flag 解析字符串 + 注入 CLIDefaults")
	show(ctx, userEntry, merge(userEntry.CLIDefaults(), map[string]any{"name": "bob"}))
	fmt.Println("模拟 HTTP 前端：query 解析 + 注入 HTTPDefaults（age 21 覆盖全局 18）")
	show(ctx, userEntry, merge(userEntry.HTTPDefaults(), map[string]any{"name": "curie"}))
	fmt.Println("模拟 MCP 前端：按 schema 直接传参（没有通道默认值注入）")
	show(ctx, userEntry, map[string]any{"name": "ada"})

	fmt.Println("\n== 搜索：全局默认 k=10，CLI 覆盖 k=25 ==")
	show(ctx, searchEntry, map[string]any{"query": "golang"})
	show(ctx, searchEntry, merge(searchEntry.CLIDefaults(), map[string]any{"query": "go"}))

	fmt.Println("\n== 缺必填字段：统一归为 invalid_input ==")
	_, err = userEntry.Invoke(ctx, map[string]any{})
	showErr(err)

	fmt.Println("\n== 业务错误：not_found → HTTP 404 / JSON-RPC -32001 / CLI 退出码 1 ==")
	_, err = userEntry.Invoke(ctx, map[string]any{"name": "missing"})
	showErr(err)
}

// printFields 展示解析后的元数据：一次定义产出了每个通道能直接消费的信息。
func printFields(e *spec.Entry) {
	fmt.Printf("  [%s]\n", e.Name)
	for _, f := range e.Root.Fields {
		if f.Skip {
			continue
		}
		fmt.Printf("    %-8s CLI{短名:%s env:%s positional:%v 默认:%s} HTTP{位置:%s 默认:%s} MCP{默认:%s} 全局默认:%s\n",
			f.JSONName,
			orDash(f.CLI.Shorthand), orDash(f.CLI.EnvVar), f.CLI.Positional,
			orNil(f.CLI.Default),
			orDash(f.HTTP.Location), orNil(f.HTTP.Default),
			orNil(f.MCP.Default),
			orNil(f.Default))
	}
}

func show(ctx context.Context, e *spec.Entry, args map[string]any) {
	out, err := e.Invoke(ctx, args)
	if err != nil {
		showErr(err)
		return
	}
	b, err := json.Marshal(out)
	must(err)
	fmt.Printf("  ok  -> %s\n", b)
}

// merge 是通道前端的职责缩略版：通道默认值先铺底，用户提供值覆盖。
func merge(defaults, provided map[string]any) map[string]any {
	out := make(map[string]any, len(defaults)+len(provided))
	for k, v := range defaults {
		out[k] = v
	}
	for k, v := range provided {
		out[k] = v
	}
	return out
}

func showErr(err error) {
	if err == nil {
		fmt.Println("  ok")
		return
	}
	kind := errs.Classify(err)
	fmt.Printf("  err  -> %v\n", err)
	fmt.Printf("  kind -> %s | HTTP %d | exit %d | JSON-RPC %d\n",
		kind, errs.HTTPStatus(kind), errs.ExitCode(kind), errs.JSONRPCCode(kind))
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func orNil(v any) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%v", v)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
