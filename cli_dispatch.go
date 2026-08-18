//go:build !nocli

package xyz

import (
	"context"

	"github.com/ejfkdev/xyz-go/cli"
	"github.com/ejfkdev/xyz-go/registry"
)

// cliFrontend 标记本编译变体是否包含 CLI 前端（用于总览标注）。
const cliFrontend = true

// runCLI 把子命令模式交给 CLI 前端。ctx 流向被调用的 handler（优雅关停）。
// 构建时加 -tags nocli 可剔除该前端。
func runCLI(ctx context.Context, reg *registry.Registry, args []string) int {
	return cli.RunContext(ctx, reg, args)
}
