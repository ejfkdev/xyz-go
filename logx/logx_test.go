package logx

import "testing"

func TestParseLevel(t *testing.T) {
	for s, want := range map[string]Level{
		"debug": LevelDebug, "info": LevelInfo, "WARN": LevelWarn, "warning": LevelWarn, "error": LevelError,
	} {
		got, err := ParseLevel(s)
		if err != nil || got != want {
			t.Fatalf("ParseLevel(%q) = %v, %v; want %v", s, got, err, want)
		}
	}
	if _, err := ParseLevel("verbose"); err == nil {
		t.Fatal("unknown level accepted")
	}
}

func TestSetLevelAndEnabled(t *testing.T) {
	old := current.Load()
	defer current.Store(old)
	SetLevel(LevelWarn)
	if !Enabled(LevelWarn) || !Enabled(LevelError) {
		t.Fatal("warn/error should be enabled at warn level")
	}
	if Enabled(LevelInfo) || Enabled(LevelDebug) {
		t.Fatal("info/debug should be silenced at warn level")
	}
	SetLevel(LevelUnset) // 非法值不生效
	if !Enabled(LevelWarn) {
		t.Fatal("unset level must not change current")
	}
}
