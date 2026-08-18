// 迁移示例：已有 Gin 服务时，把 xyz-go 的命令挂成路由。
//
// 三种共存方式的示范：单条命令处理器（HandlerFor）、中间件积木复用
// （Bearer/CORS/Gzip 包外层）、整表挂载（Handler，含 /openapi.json 与
// /healthz）。要求 gin ≥ v1.9（PathValue 支持路径参数绑定）。
//
//	go run .                       # 监听 :8080
//	curl -X POST localhost:8080/users/alice -d '{"age":9}'
//	curl -X POST localhost:8080/secure/alice -H 'Authorization: Bearer s3cret' -d '{}'
//	curl localhost:8080/api/openapi.json
package main

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ejfkdev/xyz-go/registry"

	"github.com/ejfkdev/xyz-go/httpapi"
	"github.com/ejfkdev/xyz-go/spec"
)

type AddHTTPArgs struct {
	Name string `json:"name" desc:"用户名" required:"true" validate:"min=2" http:"path"`
	Age  int    `json:"age" desc:"年龄" default:"18" http:"query"`
}

type AddHTTPResp struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func addUser(_ context.Context, in *AddHTTPArgs) (*AddHTTPResp, error) {
	return &AddHTTPResp{Name: in.Name, Age: in.Age}, nil
}

func main() {
	// 挂载场景只需要定义与 Entry：RegisterDefault 直接返回注册好的 Entry。
	addEntry, err := spec.Define("user.add", addUser).Summary("Add a user").
		HTTP(spec.HTTPHints{Method: "POST", Path: "/users/{name}"}).
		RegisterDefault()
	if err != nil {
		panic(err)
	}

	router := gin.Default()
	// 桥接 shim：xyz 的 http:"path" 绑定读 r.PathValue，gin 的 :name 参数需要
	// 手动桥进 Request（WrapH 不自动做）。一行中间件即可全局生效。
	router.Use(func(c *gin.Context) {
		for _, p := range c.Params {
			c.Request.SetPathValue(p.Key, p.Value)
		}
		c.Next()
	})

	// 方式一：单条命令的完整绑定处理器（query/path/header/body + 错误映射），
	// 用 gin.WrapH 挂进你自己的路由表。
	router.POST("/users/:name", gin.WrapH(httpapi.HandlerFor(addEntry)))

	// 方式二：复用 xyz 的中间件积木（Bearer/CORS/Gzip 各自独立），
	// 包在 HandlerFor 外侧即可组合自己的安全策略。
	secured := httpapi.Bearer([]string{"s3cret"}, httpapi.HandlerFor(addEntry))
	router.POST("/secure/:name", gin.WrapH(secured))

	// 方式三：整表挂载（所有注册路由 + /openapi.json + /healthz），
	// 像挂一个子路由一样透传；你自己的 gin 中间件可以继续包外侧。
	api, err := httpapi.Handler(registry.Default)
	if err != nil {
		panic(err)
	}
	// 子路由挂载：剥掉 /api 前缀，让内部 mux 看到原始路径（/openapi.json、/healthz）。
	router.Any("/api/*any", gin.WrapH(http.StripPrefix("/api", api)))

	_ = router.Run(":8080")
}
