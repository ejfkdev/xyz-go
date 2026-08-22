package mcp

import (
	"errors"
	"fmt"
	"github.com/ejfkdev/xyz-go/langx"
	"strings"
	"time"
)

func parseArgs(args []string) (string, Options, error) {
	var opts Options
	transport := ""
	positional := 0
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json-response":
			opts.JSONResponse = true
		case a == "--stateless":
			opts.Stateless = true
		case strings.HasPrefix(a, "--addr="):
			opts.Addr = strings.TrimPrefix(a, "--addr=")
		case a == "--addr" && i+1 < len(args):
			i++
			opts.Addr = args[i]
		case strings.HasPrefix(a, "--versions="):
			opts.Versions = splitVersions(strings.TrimPrefix(a, "--versions="))
		case a == "--versions" && i+1 < len(args):
			i++
			opts.Versions = splitVersions(args[i])
		case a == "--default" && i+1 < len(args):
			i++
			if opts.Defaults == nil {
				opts.Defaults = map[string]string{}
			}
			for _, pair := range strings.Split(args[i], ",") {
				if k, v, ok := strings.Cut(strings.TrimSpace(pair), "="); ok && strings.TrimSpace(k) != "" {
					opts.Defaults[strings.TrimSpace(k)] = v
				}
			}
		case a == "--bearer" && i+1 < len(args):
			i++
			opts.BearerTokens = mergeTokenList(opts.BearerTokens, args[i])
		case strings.HasPrefix(a, "--default="):
			if opts.Defaults == nil {
				opts.Defaults = map[string]string{}
			}
			for _, pair := range strings.Split(strings.TrimPrefix(a, "--default="), ",") {
				if k, v, ok := strings.Cut(strings.TrimSpace(pair), "="); ok && strings.TrimSpace(k) != "" {
					opts.Defaults[strings.TrimSpace(k)] = v
				}
			}
		case strings.HasPrefix(a, "--bearer="):
			opts.BearerTokens = mergeTokenList(opts.BearerTokens, strings.TrimPrefix(a, "--bearer="))
		case a == "--session-timeout" && i+1 < len(args):
			i++
			d, err := time.ParseDuration(args[i])
			if err != nil {
				return "", opts, fmt.Errorf("invalid --session-timeout %q: %w", args[i], err)
			}
			opts.SessionTimeout = d
		case strings.HasPrefix(a, "--session-timeout="):
			d, err := time.ParseDuration(strings.TrimPrefix(a, "--session-timeout="))
			if err != nil {
				return "", opts, fmt.Errorf("invalid --session-timeout: %w", err)
			}
			opts.SessionTimeout = d
		case a == "--cors" && i+1 < len(args):
			i++
			opts.CORSOrigins = mergeTokenList(opts.CORSOrigins, args[i])
		case strings.HasPrefix(a, "--cors="):
			opts.CORSOrigins = mergeTokenList(opts.CORSOrigins, strings.TrimPrefix(a, "--cors="))
		case strings.HasPrefix(a, "--name="):
			opts.Name = strings.TrimPrefix(a, "--name=")
		case a == "--name" && i+1 < len(args):
			i++
			opts.Name = args[i]
		case strings.HasPrefix(a, "--server-version="):
			opts.Version = strings.TrimPrefix(a, "--server-version=")
		case a == "--server-version" && i+1 < len(args):
			i++
			opts.Version = args[i]
		case strings.HasPrefix(a, "-"):
			// 未识别的 --key v / --key=v 透传为通道默认参数（与 serve 一致）。
			raw := strings.TrimPrefix(a, "-")
			raw = strings.TrimPrefix(raw, "-")
			if k, v, ok := strings.Cut(raw, "="); ok {
				if opts.Defaults == nil {
					opts.Defaults = map[string]string{}
				}
				opts.Defaults[strings.TrimSpace(k)] = v
			} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				if opts.Defaults == nil {
					opts.Defaults = map[string]string{}
				}
				opts.Defaults[strings.TrimSpace(raw)] = args[i+1]
				i++
			} else {
				return "", opts, fmt.Errorf("flag needs an argument: --%s", raw)
			}
		default:
			if positional == 0 {
				transport = a
			}
			positional++
		}
	}
	if transport == "" {
		return "", opts, errors.New(langx.T("mcp.err_missing_transport"))
	}
	if positional > 1 {
		return "", opts, fmt.Errorf("%s", langx.Tf("mcp.err_usage_extra_arg", args[len(args)-1]))
	}
	return transport, opts, nil
}

func splitVersions(s string) []string {
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// HTTPHandler exposes the registry's tools as a streamable-HTTP handler,
// for mounting into an existing http.Server (e.g. at /mcp).
func (o *Options) mergeDefaults(base Options) {
	if o.Addr == "" {
		o.Addr = base.Addr
	}
	if o.Name == "" {
		o.Name = base.Name
	}
	if o.Version == "" {
		o.Version = base.Version
	}
	if len(o.Versions) == 0 {
		o.Versions = base.Versions
	}
	if o.Instructions == "" {
		o.Instructions = base.Instructions
	}
	if len(o.BearerTokens) == 0 {
		o.BearerTokens = base.BearerTokens
	}
	if o.SessionTimeout == 0 {
		o.SessionTimeout = base.SessionTimeout
	}
	if len(o.CORSOrigins) == 0 {
		o.CORSOrigins = base.CORSOrigins
	}
	if len(o.Defaults) == 0 {
		o.Defaults = base.Defaults
	}
	o.JSONResponse = o.JSONResponse || base.JSONResponse
	o.Stateless = o.Stateless || base.Stateless
}

// mergeTokenList 合并逗号分隔的 token 列表并去重。
func mergeTokenList(existing []string, flag string) []string {
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
