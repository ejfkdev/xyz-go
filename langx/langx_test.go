package langx

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	for s, want := range map[string]Language{"en": En, "zh-CN": ZhCn} {
		if got, ok := Parse(s); !ok || got != want {
			t.Fatalf("Parse(%q) = %v,%v want %v,true", s, got, ok, want)
		}
	}
	for _, s := range []string{"fr", "", "zn-CN"} {
		if _, ok := Parse(s); ok {
			t.Fatalf("Parse(%q) should fail", s)
		}
	}
}

func TestDetect(t *testing.T) {
	for _, kv := range [][2]string{
		{"LC_ALL", "zh_CN.UTF-8"},
		{"LANG", "zh_TW"},
	} {
		t.Setenv("LC_ALL", "")
		t.Setenv("LANG", "")
		t.Setenv(kv[0], kv[1])
		if Detect() != ZhCn {
			t.Fatalf("Detect with %s=%s should be ZhCn", kv[0], kv[1])
		}
	}
	t.Setenv("LC_ALL", "C")
	t.Setenv("LANG", "")
	if Detect() != En {
		t.Fatal("Detect with LC_ALL=C should be En")
	}
	t.Setenv("LC_ALL", "")
	t.Setenv("LANG", "")
	if Detect() != En {
		t.Fatal("Detect with no locale should be En")
	}
}

func TestTBasicsAndOverrides(t *testing.T) {
	Set(En, nil)
	if !strings.HasPrefix(T("overview.usage_line"), "Usage") {
		t.Fatalf("en overview: %q", T("overview.usage_line"))
	}
	Set(ZhCn, nil)
	if !strings.HasPrefix(T("overview.usage_line"), "用法") {
		t.Fatalf("zh overview: %q", T("overview.usage_line"))
	}
	// 覆盖优先
	Set(En, map[string]string{"help.help_flag": "show me help"})
	if T("help.help_flag") != "show me help" {
		t.Fatalf("override lost: %q", T("help.help_flag"))
	}
	// 未知键回退键名（绝不 panic）
	if T("no.such.key") != "no.such.key" {
		t.Fatal("unknown key should fall back to itself")
	}
	Set(En, nil)
}

func TestTfPlaceholders(t *testing.T) {
	Set(En, nil)
	got := Tf("warn.mode_disabled", "serve", "HTTP")
	if !strings.Contains(got, "serve") || !strings.Contains(got, "HTTP") {
		t.Fatalf("Tf: %q", got)
	}
}
