package langx

import (
	"context"
	"testing"
)

func TestParseAcceptLanguage(t *testing.T) {
	cases := []struct {
		header string
		want   Language
		wantOK bool
	}{
		{"zh-CN,zh;q=0.9,en;q=0.8", ZhCn, true},
		{"en-US,en;q=0.9", En, true},
		{"en;q=0.5,zh-CN;q=0.9", ZhCn, true}, // q 值择优 → zh
		{"zh", ZhCn, true},
		{"fr,de;q=0.9", En, false}, // 无受支持标签
		{"", En, false},
	}
	for _, c := range cases {
		got, ok := ParseAcceptLanguage(c.header)
		if got != c.want || ok != c.wantOK {
			t.Fatalf("ParseAcceptLanguage(%q) = (%v,%v), want (%v,%v)", c.header, got, ok, c.want, c.wantOK)
		}
	}
}

func TestCtxLanguage(t *testing.T) {
	Set(En, nil)
	defer Set(En, nil)
	ctx := WithLang(context.Background(), ZhCn)
	if l, ok := FromCtx(ctx); !ok || l != ZhCn {
		t.Fatalf("FromCtx = (%v,%v), want (ZhCn,true)", l, ok)
	}
	if got := Tctx(ctx, "overview.commands"); got != "命令:" {
		t.Fatalf("Tctx zh = %q, want 命令:", got)
	}
	// 无 ctx 语言 → 回退进程语言（En），第二返回值 false。
	if l, ok := FromCtx(context.Background()); ok || l != En {
		t.Fatalf("FromCtx(empty) = (%v,%v), want (En,false)", l, ok)
	}
	if got := Tctx(context.Background(), "overview.commands"); got != "Commands:" {
		t.Fatalf("Tctx fallback en = %q, want Commands:", got)
	}
	// Tfctx 参数代入按 ctx 语言。
	if got := Tfctx(ctx, "cli.err_positional_count", "cmd", "1", "2", "3"); got == "" || got == "cli.err_positional_count" {
		t.Fatalf("Tfctx zh unresolved: %q", got)
	}
}
