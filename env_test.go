package xyz

import (
	"context"
	"testing"

	"github.com/ejfkdev/xyz-go/langx"
)

// 公开环境上下文 API：Language / Interactive / Env / LanguageFromCtx。
func TestEnvContextAPI(t *testing.T) {
	langx.Set(langx.ZhCn, nil)
	defer langx.Set(langx.En, nil)

	if got := Language(); got != "zh-CN" {
		t.Fatalf("Language() = %q, want zh-CN", got)
	}
	env := Env()
	if env.Language != "zh-CN" {
		t.Fatalf("Env().Language = %q, want zh-CN", env.Language)
	}
	if Interactive() != env.Interactive {
		t.Fatalf("Interactive() and Env().Interactive must agree")
	}
	// NoColor 是布尔，仅验证可调用且与 Env 一致。
	if NoColor() != env.NoColor {
		t.Fatalf("NoColor() and Env().NoColor must agree")
	}

	// LanguageFromCtx：ctx 携带的语言优先于进程语言。
	ctx := langx.WithLang(context.Background(), langx.En)
	if got := LanguageFromCtx(ctx); got != "en" {
		t.Fatalf("LanguageFromCtx(en ctx) = %q, want en", got)
	}
	// 未携带 → 回退进程语言（zh-CN）。
	if got := LanguageFromCtx(context.Background()); got != "zh-CN" {
		t.Fatalf("LanguageFromCtx(empty) = %q, want zh-CN (process)", got)
	}
}
