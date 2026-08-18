//go:build nohttp

package xyz

import (
	"context"
	"fmt"
	"os"

	"github.com/ejfkdev/xyz-go/registry"
)

// httpFrontend 标记本编译变体是否包含 HTTP 前端（用于总览标注）。
const httpFrontend = false

// runServe 是 -tags nohttp 构建下的兜底：本二进制没有编译进 HTTP 前端。
func runServe(_ context.Context, _ *registry.Registry, _ []string, _ Config) int {
	fmt.Fprintln(os.Stderr, "xyz: 本二进制未编译 HTTP 前端（构建时使用了 -tags nohttp）")
	return 1
}
