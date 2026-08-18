//go:build nocli

package xyz

import (
	"context"
	"fmt"
	"os"

	"github.com/ejfkdev/xyz-go/registry"
)

// cliFrontend 标记本编译变体是否包含 CLI 前端（用于总览标注）。
const cliFrontend = false

// runCLI 是 -tags nocli 构建下的兜底：本二进制没有编译进 CLI 前端，
// 子命令模式整体不可用。
func runCLI(_ context.Context, _ *registry.Registry, _ []string) int {
	fmt.Fprintln(os.Stderr, "xyz: 本二进制未编译 CLI 前端（构建时使用了 -tags nocli）")
	return 1
}
