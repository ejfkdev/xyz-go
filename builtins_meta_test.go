package xyz

import "testing"

// 派发层接线：--xyz.header / --xyz.no-server-headers 解析与 resolvedVersion
// 回退（Config.Version 覆盖 > 包级 xyz.Version）。

func TestStripXYZHeaderFlag(t *testing.T) {
	var cfg Config
	rest, err := stripXYZFlags([]string{
		"--xyz.header=X-App=demo,X-Env=prod", "serve", "--xyz.header", "X-Trace=1",
	}, &cfg)
	if err != nil {
		t.Fatalf("stripXYZFlags: %v", err)
	}
	if len(rest) != 1 || rest[0] != "serve" {
		t.Fatalf("rest = %v, want [serve]", rest)
	}
	want := map[string]string{"X-App": "demo", "X-Env": "prod", "X-Trace": "1"}
	for k, v := range want {
		if cfg.ResponseHeaders[k] != v {
			t.Fatalf("ResponseHeaders[%q] = %q, want %q", k, cfg.ResponseHeaders[k], v)
		}
	}
}

func TestStripXYZHeaderKeepsEqualsInValue(t *testing.T) {
	var cfg Config
	if _, err := stripXYZFlags([]string{"--xyz.header=Cookie=a=b"}, &cfg); err != nil {
		t.Fatalf("stripXYZFlags: %v", err)
	}
	if cfg.ResponseHeaders["Cookie"] != "a=b" {
		t.Fatalf("header value with '=' = %q, want a=b", cfg.ResponseHeaders["Cookie"])
	}
}

func TestStripXYZHeaderInvalid(t *testing.T) {
	if _, err := stripXYZFlags([]string{"--xyz.header=novalue"}, &Config{}); err == nil {
		t.Fatalf("expected error for header without '='")
	}
}

func TestStripXYZNoServerHeaders(t *testing.T) {
	var cfg Config
	if _, err := stripXYZFlags([]string{"--xyz.no-server-headers", "serve"}, &cfg); err != nil {
		t.Fatalf("stripXYZFlags: %v", err)
	}
	if !cfg.NoServerHeaders {
		t.Fatalf("NoServerHeaders should be true")
	}
	var cfg2 Config
	if _, err := stripXYZFlags([]string{"--xyz.no-server-headers=false"}, &cfg2); err != nil {
		t.Fatalf("stripXYZFlags: %v", err)
	}
	if cfg2.NoServerHeaders {
		t.Fatalf("NoServerHeaders should be false")
	}
}

func TestResolvedVersion(t *testing.T) {
	if got := (Config{Version: "v2.0.0"}).resolvedVersion(); got != "v2.0.0" {
		t.Fatalf("resolvedVersion override = %q", got)
	}
	orig := Version
	Version = "vPkg"
	defer func() { Version = orig }()
	if got := (Config{}).resolvedVersion(); got != "vPkg" {
		t.Fatalf("resolvedVersion fallback = %q, want vPkg", got)
	}
}

func TestResolvedName(t *testing.T) {
	// Config.Name 覆盖优先。
	if got := (Config{Name: "myapp"}).resolvedName(); got != "myapp" {
		t.Fatalf("resolvedName override = %q", got)
	}
	// 空则回退到二进制 basename（非空、非 "." / "/"）。
	if got := (Config{}).resolvedName(); got == "" {
		t.Fatalf("resolvedName fallback must not be empty")
	}
}

func TestStripXYZFormat(t *testing.T) {
	var cfg Config
	rest, err := stripXYZFlags([]string{"--xyz.format=jsonl", "user", "list"}, &cfg)
	if err != nil {
		t.Fatalf("stripXYZFlags: %v", err)
	}
	if cfg.Format != "jsonl" {
		t.Fatalf("Format = %q, want jsonl", cfg.Format)
	}
	if len(rest) != 2 || rest[0] != "user" || rest[1] != "list" {
		t.Fatalf("rest = %v, want [user list]", rest)
	}
	// 空格分隔形式。
	var cfg2 Config
	if _, err := stripXYZFlags([]string{"--xyz.format", "markdown", "x"}, &cfg2); err != nil {
		t.Fatalf("stripXYZFlags: %v", err)
	}
	if cfg2.Format != "markdown" {
		t.Fatalf("Format = %q, want markdown", cfg2.Format)
	}
	// 非法值在派发层即报错（指名 --xyz.format）。
	if _, err := stripXYZFlags([]string{"--xyz.format=xml"}, &Config{}); err == nil {
		t.Fatalf("expected error for invalid --xyz.format")
	}
	// auto 合法，且 --xyz.format 标记 formatFromFlag（命令行层，高于逐命令 hint）。
	var cfg3 Config
	if _, err := stripXYZFlags([]string{"--xyz.format=auto"}, &cfg3); err != nil {
		t.Fatalf("stripXYZFlags: %v", err)
	}
	if cfg3.Format != "auto" || !cfg3.formatFromFlag {
		t.Fatalf("Format=%q formatFromFlag=%v, want auto/true", cfg3.Format, cfg3.formatFromFlag)
	}
}
