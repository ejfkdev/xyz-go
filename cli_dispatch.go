//go:build !nocli

package xyz

import (
	"context"

	"github.com/ejfkdev/xyz-go/cli"
	"github.com/ejfkdev/xyz-go/registry"
)

// cliFrontend 标记本编译变体是否包含 CLI 前端（用于总览标注）。
const cliFrontend = true

// runCLI 把子命令模式交给 CLI 前端。ctx 流向被调用的 handler（优雅关停）；
// cfg.Format（来自 --xyz.format）作为默认输出格式注入前端。构建时加
// -tags nocli 可剔除该前端。
func runCLI(ctx context.Context, reg *registry.Registry, args []string, cfg Config) int {
	return cli.RunContextWithOptions(ctx, reg, args, cli.Options{Format: cfg.Format})
}
