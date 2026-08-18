//go:build !nohttp

package xyz

import (
	"testing"
	"time"
)

func TestParseServeArgs(t *testing.T) {
	// 裸名 flag（模式词即命名空间）与代码 Config 的叠加语义。
	cfg := Config{Addr: ":7070", BearerTokens: []string{"code-tok"}}
	got := parseServeArgs([]string{"--addr", ":8080", "--bearer=a,b", "--bearer=c",
		"--timeout", "45s", "--tls-cert", "a.pem", "--tls-key=k.pem", "--cors=x,y"}, cfg)
	if got.Addr != ":8080" {
		t.Fatalf("addr = %q, want :8080 (local flag wins)", got.Addr)
	}
	if len(got.BearerTokens) != 4 {
		t.Fatalf("tokens = %v, want code preset + a + b + c", got.BearerTokens)
	}
	if got.Timeout != 45*time.Second || got.CertFile != "a.pem" || got.KeyFile != "k.pem" ||
		len(got.CORSOrigins) != 2 {
		t.Fatalf("serve config = %+v", got)
	}
	// 无局部 flag 时回落到代码/--xyz 预置。
	got2 := parseServeArgs(nil, cfg)
	if got2.Addr != ":7070" || len(got2.BearerTokens) != 1 || got2.BearerTokens[0] != "code-tok" {
		t.Fatalf("fallback = %+v", got2)
	}
}
