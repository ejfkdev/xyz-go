// 完整示例：一条 xyz.Define(...)...Run() 链 = 整个程序。
// 覆盖常用写法：位置参数、短名/别名、env 注入与 secret 字段、枚举、
// 指针与切片、命名标量、time.Duration/[]byte、required/validate、
// 全局默认值与 CLI/HTTP/MCP 三层覆盖、表格输出、基础类型返回、
// 错误分类（not_found/invalid_input/unauthorized）、MCP 注解与版本限定。
//
// 试用：
//
//	go run ./cmd/example user add bob -a 20 -m fast --tags a,b
//	APP_TOKEN=t0ken go run ./cmd/example user add alice --verbose
//	go run ./cmd/example user ua carol            # 别名
//	go run ./cmd/example user rm alice            # 成功；user rm bob → not_found
//	go run ./cmd/example user list                # []struct → 表格
//	go run ./cmd/example search query --query golang  # CLI 专属默认 k=25（--q 是未知 flag）
//	go run ./cmd/example math sum --a 1 --b 2     # 基础类型 int
//	go run ./cmd/example math div --a 10 --b 4    # float64；--b 0 → invalid_input
//	go run ./cmd/example time now                 # time.Time
//	go run ./cmd/example sys sleep --d 300ms      # time.Duration；>5s 报错
//	go run ./cmd/example sys port -p 9090         # 命名标量 type Port
//	go run ./cmd/example file hash --data hello   # []byte
//	NEXT_KEY=k go run ./cmd/example net head      # header/httpName + env
//	go run ./cmd/example mcp stdio                # MCP：命令即工具
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/ejfkdev/xyz-go"
	errs "github.com/ejfkdev/xyz-go/errors"
)

// ---- user.add：最完整的定义——三个通道的配置全在这里 ----

type AddUserArgs struct {
	Name    string   `json:"name" desc:"用户名称" required:"true" validate:"min=2,max=32" cli:"positional" http:"path"`
	Email   string   `json:"email" desc:"邮箱" validate:"omitempty,email"`
	Age     int      `json:"age" desc:"年龄" default:"18" http:"query"`
	Mode    string   `json:"mode" desc:"部署模式" enum:"fast,slow" http:"query"`
	Tags    []string `json:"tags" desc:"标签" http:"query"`
	Limit   *int     `json:"limit" desc:"分页上限" http:"query"`
	Token   string   `json:"-" secret:"true" desc:"API 令牌（仅 env 注入，不进 schema）"`
	Verbose bool     `json:"verbose" desc:"打印详细信息"`
}

type AddUserResp struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Age      int    `json:"age"`
	TokenSet bool   `json:"token_set"`
}

var idCounter int64

func addUser(_ context.Context, in *AddUserArgs) (*AddUserResp, error) {
	if in.Name == "missing" {
		return nil, errs.New(errs.KindNotFound, "no such user")
	}
	idCounter++
	return &AddUserResp{ID: idCounter, Name: in.Name, Age: in.Age, TokenSet: in.Token != ""}, nil
}

// ---- user.list / user.rm ----

type ListArgs struct{}

func listUsers(_ context.Context, _ *ListArgs) ([]AddUserResp, error) {
	return []AddUserResp{
		{ID: 1, Name: "alice", Age: 18},
		{ID: 2, Name: "bob", Age: 25},
	}, nil
}

type RmArgs struct {
	Name  string `json:"name" desc:"要删除的用户" required:"true" cli:"positional"`
	Force bool   `json:"force" desc:"强制删除（隐藏 flag）" cli:"hidden"`
}

func rmUser(_ context.Context, in *RmArgs) (string, error) {
	if in.Name != "alice" {
		return "", errs.New(errs.KindNotFound, fmt.Sprintf("user %q not found", in.Name))
	}
	msg := "user alice removed"
	if in.Force {
		msg += " (forced)"
	}
	return msg, nil
}

// ---- search.query：三层默认值（全局 10 / CLI 25 / MCP 15） ----

type SearchArgs struct {
	Query string   `json:"query" desc:"关键词" required:"true"`
	K     int      `json:"k" desc:"返回条数" default:"10"`
	Tags  []string `json:"tags" desc:"过滤标签"`
}

func search(_ context.Context, in *SearchArgs) ([]string, error) {
	return []string{in.Query, "...", fmt.Sprintf("top %d", in.K)}, nil
}

// ---- math.sum / math.div：required 标量与基础类型返回 ----

type SumArgs struct {
	A int `json:"a" desc:"左操作数" required:"true"`
	B int `json:"b" desc:"右操作数" required:"true"`
}

func sum(_ context.Context, in *SumArgs) (int, error) { return in.A + in.B, nil }

type DivArgs struct {
	A float64 `json:"a" desc:"被除数" required:"true"`
	B float64 `json:"b" desc:"除数" required:"true"`
}

func div(_ context.Context, in *DivArgs) (float64, error) {
	if in.B == 0 {
		return 0, errs.New(errs.KindInvalidInput, "divisor is zero")
	}
	return in.A / in.B, nil
}

// ---- time.now ----

type ClockArgs struct{}

func now(_ context.Context, _ *ClockArgs) (time.Time, error) { return time.Now().UTC(), nil }

// ---- sys.sleep：time.Duration 入参 ----

type SleepArgs struct {
	D time.Duration `json:"d" desc:"睡眠时长" default:"100ms"`
}

func sleep(_ context.Context, in *SleepArgs) (string, error) {
	if in.D > 5*time.Second {
		return "", errs.New(errs.KindInvalidInput, "sleep too long (max 5s)")
	}
	time.Sleep(in.D)
	return fmt.Sprintf("slept %s", in.D), nil
}

// ---- sys.port：命名标量类型 ----

type Port int

type PortArgs struct {
	Port Port `json:"port" desc:"监听端口" default:"8080" cli:"shorthand=p"`
}

func listen(_ context.Context, in *PortArgs) (string, error) {
	return fmt.Sprintf("listening on :%d", in.Port), nil
}

// ---- file.hash：[]byte 入参 ----

type HashArgs struct {
	Data []byte `json:"data" desc:"原始内容" required:"true"`
}

func hashData(_ context.Context, in *HashArgs) (string, error) {
	sum := sha256.Sum256(in.Data)
	return hex.EncodeToString(sum[:]), nil
}

// ---- net.head：HTTP header 绑定 + httpName + secret + env ----

type HeadArgs struct {
	Key string `json:"-" secret:"true" desc:"API Key（header/env 注入）" http:"header" httpName:"X-Api-Key" cli:"env=NEXT_KEY"`
}

func head(_ context.Context, in *HeadArgs) (string, error) {
	if in.Key == "" {
		return "", errs.New(errs.KindUnauthorized, "missing X-Api-Key (set env NEXT_KEY)")
	}
	return fmt.Sprintf("api key accepted (%d bytes)", len(in.Key)), nil
}

func main() {
	xyz.Define("user.add", addUser).
		Summary("创建用户").
		Description("创建新用户并返回内部 ID。\n三种通道的全部配置都在这一个定义里："+
			"位置参数、短名、别名、env 注入的 secret、枚举、指针与切片、校验规则。",
		).
		CLI(xyz.CliHints{
			Usage:   "add <name>",
			Aliases: []string{"ua", "new"},
			Fields: map[string]xyz.CliFieldHint{
				"age":     {Shorthand: "a", Default: 20}, // CLI 专属默认值（覆盖全局 18）
				"mode":    {Shorthand: "m"},
				"tags":    {Shorthand: "t"},
				"token":   {EnvVar: "APP_TOKEN"}, // json:"-" 字段：只走 env 注入，无 flag
				"verbose": {Shorthand: "V"},
			},
		}).
		HTTP(xyz.HTTPHints{Method: "POST", Path: "/users/{name}"}).
		MCP(xyz.MCPHints{Annotations: []string{"write", "title:创建用户"}}).
		Also(
			xyz.Define("user.list", listUsers).
				Summary("列出用户").
				Description("返回用户切片：CLI 渲染成对齐表格，结构化通道保持原始数组。").
				HTTP(xyz.HTTPHints{Method: "GET", Path: "/users"}),

			xyz.Define("user.rm", rmUser).
				Summary("删除用户").
				CLI(xyz.CliHints{Usage: "rm <name>", Aliases: []string{"del"}}).
				MCP(xyz.MCPHints{Annotations: []string{"destructive"}}),

			xyz.Define("search.query", search).
				Summary("搜索文档").
				CLI(xyz.CliHints{
					Fields: map[string]xyz.CliFieldHint{
						"k": {Default: 25}, // CLI 覆盖全局 10
					},
				}).
				HTTP(xyz.HTTPHints{Method: "GET", Path: "/search"}).
				MCP(xyz.MCPHints{Fields: map[string]xyz.MCPFieldHint{
					"k": {Default: 15}, // MCP 覆盖全局 10，并反映进 inputSchema
				}}),

			xyz.Define("math.sum", sum).
				Summary("两数求和").
				MCP(xyz.MCPHints{Annotations: []string{"read", "idempotent"}}),

			xyz.Define("math.div", div).Summary("两数相除"),

			xyz.Define("time.now", now).Summary("当前 UTC 时间"),

			xyz.Define("sys.sleep", sleep).
				Summary("睡眠指定时长").
				CLI(xyz.CliHints{Fields: map[string]xyz.CliFieldHint{"d": {Shorthand: "d"}}}),

			xyz.Define("sys.port", listen).
				Summary("监听端口").
				CLI(xyz.CliHints{Fields: map[string]xyz.CliFieldHint{"port": {Shorthand: "p"}}}),

			xyz.Define("file.hash", hashData).
				Summary("计算 SHA-256").
				CLI(xyz.CliHints{Fields: map[string]xyz.CliFieldHint{"data": {Shorthand: "d"}}}),

			xyz.Define("net.head", head).
				Summary("探测 API Key 注入").
				HTTP(xyz.HTTPHints{Method: "GET", Path: "/headers"}),
		).
		// 可选的能力开关示例：
		//   .Configure(xyz.Config{Capabilities: xyz.Capabilities{NoMCP: true}}) // 禁用 mcp 模式
		Run() // 注册全部命令、派发并按结果 os.Exit
}
