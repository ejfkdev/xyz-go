package xyz

import (
	"fmt"
	"strings"
	"time"

	"github.com/ejfkdev/xyz-go/logx"
)

// 内置参数解析：全局 --xyz.*（任意位置）与 serve 模式的裸名 flag（模式词即
// 命名空间），统一折叠进 Config。优先级：局部 flag > 全局/代码 Config > 默认。

// stripXYZFlags 提取 --xyz.* 内置参数，把它们从参数列表中移除并写回 cfg；
// 命令行值覆盖代码里的 Config。剩余参数原样返回。
func stripXYZFlags(args []string, cfg *Config) ([]string, error) {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		value := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag %s needs an argument", a)
			}
			i++
			return args[i], nil
		}
		switch {
		case a == "--xyz.addr":
			if v, err := value(); err != nil {
				return nil, err
			} else {
				cfg.Addr = v
			}
		case strings.HasPrefix(a, "--xyz.addr="):
			cfg.Addr = strings.TrimPrefix(a, "--xyz.addr=")
		case a == "--xyz.bearer":
			if v, err := value(); err != nil {
				return nil, err
			} else {
				cfg.BearerTokens = mergeTokens(cfg.BearerTokens, v)
			}
		case strings.HasPrefix(a, "--xyz.bearer="):
			cfg.BearerTokens = mergeTokens(cfg.BearerTokens, strings.TrimPrefix(a, "--xyz.bearer="))
		case a == "--xyz.log-level":
			if v, err := value(); err != nil {
				return nil, err
			} else if lv, err := logx.ParseLevel(v); err != nil {
				return nil, err
			} else {
				cfg.LogLevel = lv
			}
		case strings.HasPrefix(a, "--xyz.log-level="):
			lv, err := logx.ParseLevel(strings.TrimPrefix(a, "--xyz.log-level="))
			if err != nil {
				return nil, err
			}
			cfg.LogLevel = lv
		case a == "--xyz.timeout":
			if v, err := value(); err != nil {
				return nil, err
			} else if d, err := time.ParseDuration(v); err != nil {
				return nil, fmt.Errorf("invalid --xyz.timeout %q: %w", v, err)
			} else {
				cfg.Timeout = d
			}
		case strings.HasPrefix(a, "--xyz.timeout="):
			d, err := time.ParseDuration(strings.TrimPrefix(a, "--xyz.timeout="))
			if err != nil {
				return nil, fmt.Errorf("invalid --xyz.timeout: %w", err)
			}
			cfg.Timeout = d
		case a == "--xyz.tls-cert":
			if v, err := value(); err != nil {
				return nil, err
			} else {
				cfg.CertFile = v
			}
		case strings.HasPrefix(a, "--xyz.tls-cert="):
			cfg.CertFile = strings.TrimPrefix(a, "--xyz.tls-cert=")
		case a == "--xyz.tls-key":
			if v, err := value(); err != nil {
				return nil, err
			} else {
				cfg.KeyFile = v
			}
		case strings.HasPrefix(a, "--xyz.tls-key="):
			cfg.KeyFile = strings.TrimPrefix(a, "--xyz.tls-key=")
		case a == "--xyz.cors":
			if v, err := value(); err != nil {
				return nil, err
			} else {
				cfg.CORSOrigins = mergeTokens(cfg.CORSOrigins, v)
			}
		case strings.HasPrefix(a, "--xyz.cors="):
			cfg.CORSOrigins = mergeTokens(cfg.CORSOrigins, strings.TrimPrefix(a, "--xyz.cors="))
		default:
			out = append(out, a)
		}
	}
	return out, nil
}

// parseServeArgs 解析 serve 模式的裸名 flag（--addr/--bearer/--timeout/
// --tls-*/--cors）；全局 --xyz.* 与代码 Config 已由根派发器折叠进 cfg。
// 返回扩展后的 Config（值语义）。
func parseServeArgs(args []string, cfg Config) Config {
	if cfg.Addr == "" {
		cfg.Addr = ":8080"
	}
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--addr" && i+1 < len(args):
			i++
			cfg.Addr = args[i]
		case strings.HasPrefix(args[i], "--addr="):
			cfg.Addr = strings.TrimPrefix(args[i], "--addr=")
		case args[i] == "--bearer" && i+1 < len(args):
			i++
			cfg.BearerTokens = mergeTokens(cfg.BearerTokens, args[i])
		case strings.HasPrefix(args[i], "--bearer="):
			cfg.BearerTokens = mergeTokens(cfg.BearerTokens, strings.TrimPrefix(args[i], "--bearer="))
		case args[i] == "--timeout" && i+1 < len(args):
			i++
			cfg.Timeout, _ = time.ParseDuration(args[i])
		case strings.HasPrefix(args[i], "--timeout="):
			cfg.Timeout, _ = time.ParseDuration(strings.TrimPrefix(args[i], "--timeout="))
		case args[i] == "--tls-cert" && i+1 < len(args):
			i++
			cfg.CertFile = args[i]
		case strings.HasPrefix(args[i], "--tls-cert="):
			cfg.CertFile = strings.TrimPrefix(args[i], "--tls-cert=")
		case args[i] == "--tls-key" && i+1 < len(args):
			i++
			cfg.KeyFile = args[i]
		case strings.HasPrefix(args[i], "--tls-key="):
			cfg.KeyFile = strings.TrimPrefix(args[i], "--tls-key=")
		case args[i] == "--cors" && i+1 < len(args):
			i++
			cfg.CORSOrigins = mergeTokens(cfg.CORSOrigins, args[i])
		case strings.HasPrefix(args[i], "--cors="):
			cfg.CORSOrigins = mergeTokens(cfg.CORSOrigins, strings.TrimPrefix(args[i], "--cors="))
		}
	}
	return cfg
}

// mergeTokens 合并逗号分隔的 token 列表并去重（代码预置在前，命令行追加在后）。
func mergeTokens(existing []string, flag string) []string {
	seen := make(map[string]bool, len(existing))
	out := make([]string, 0, len(existing)+2)
	for _, t := range existing {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	for _, t := range strings.Split(flag, ",") {
		if t = strings.TrimSpace(t); t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}
