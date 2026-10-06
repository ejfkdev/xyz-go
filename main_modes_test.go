package xyz

import "testing"

// 模式词解析：四词默认 + http 词 + 成对互异校验。
func TestResolveModesFourWords(t *testing.T) {
	m, err := resolveModes(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if m.serve != "serve" || m.http != "http" || m.mcp != "mcp" || m.help != "help" {
		t.Fatalf("default modes = %+v", m)
	}
	// 自定义 http 词。
	m2, err := resolveModes(Config{Modes: ModeWords{HTTP: "rest"}})
	if err != nil || m2.http != "rest" {
		t.Fatalf("custom http word: %+v err=%v", m2, err)
	}
	// 重复词报错。
	if _, err := resolveModes(Config{Modes: ModeWords{Serve: "serve", HTTP: "serve"}}); err == nil {
		t.Fatalf("duplicate mode words should error")
	}
}

// matchMode：xyz.<词> 恒命中；裸词未遮蔽时命中。
func TestMatchModeNamespacedAlways(t *testing.T) {
	m, _ := resolveModes(Config{})
	noShadow := map[string]bool{}
	for _, tc := range []struct {
		token string
		want  modeKind
	}{
		{"serve", modeServe}, {"http", modeHTTP}, {"mcp", modeMCP}, {"help", modeHelp},
		{"xyz.serve", modeServe}, {"xyz.http", modeHTTP}, {"xyz.mcp", modeMCP}, {"xyz.help", modeHelp},
		{"bogus", modeNone}, {"", modeNone},
	} {
		if got := matchMode(tc.token, m, noShadow); got != tc.want {
			t.Fatalf("matchMode(%q) = %v, want %v", tc.token, got, tc.want)
		}
	}
	// 即便裸词被遮蔽，xyz.<词> 仍命中内建。
	shadow := map[string]bool{"serve": true, "http": false, "mcp": false, "help": false}
	if got := matchMode("serve", m, shadow); got != modeNone {
		t.Fatalf("shadowed bare serve should not match, got %v", got)
	}
	if got := matchMode("xyz.serve", m, shadow); got != modeServe {
		t.Fatalf("xyz.serve must match even when bare is shadowed, got %v", got)
	}
}

func TestHasHelpFlag(t *testing.T) {
	if !hasHelpFlag([]string{"-h"}) || !hasHelpFlag([]string{"--addr", ":8080", "--help"}) {
		t.Fatalf("should detect -h/--help")
	}
	if hasHelpFlag([]string{"--addr", ":8080"}) {
		t.Fatalf("no help flag present")
	}
	// "--" 之后的 -h 是位置参数，不算帮助旗标。
	if hasHelpFlag([]string{"--", "-h"}) {
		t.Fatalf("-h after -- must not count as help")
	}
}
