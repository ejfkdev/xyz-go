package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ejfkdev/xyz-go/block"
	"github.com/ejfkdev/xyz-go/cli"
	errs "github.com/ejfkdev/xyz-go/errors"
	"github.com/ejfkdev/xyz-go/spec"
)

func makeHandler(e *spec.Entry, allowed map[string]bool, opts Options, appName, appVersion string) func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	defaults := opts.Defaults
	sdkVersion := opts.SDKVersion
	headers := opts.ResponseHeaders
	disabled := opts.NoServerMeta
	command := e.Name
	// buildMeta 闭包捕获每个工具恒定的身份字段，调用点只传耗时与可选错误体。
	// 构造一次调用结果的 _meta.xyz 服务器上下文（xyz-spec §12.8），对齐 HTTP
	// 的 X-App-*/X-XYZ-* 头：应用身份 + xyz 库版本 + 命令 + 耗时 + 自定义头，
	// errBody 非 nil 时再并入 error 上下文（kind/code/detail），让 MCP 客户端
	// 无需解析文本即可按领域语义分支。disabled 时返回 nil（不写 _meta.xyz）。
	buildMeta := func(dur time.Duration, errBody *errs.Body) sdkmcp.Meta {
		if disabled {
			return nil
		}
		xyz := map[string]any{
			"app_name":    appName,
			"app_version": appVersion,
			"sdk_version": sdkVersion,
			"command":     command,
			"duration_ms": dur.Milliseconds(),
		}
		if len(headers) > 0 {
			xyz["headers"] = headers
		}
		if errBody != nil {
			em := map[string]any{"kind": string(errBody.Kind)}
			if errBody.Code != "" {
				em["code"] = errBody.Code
			}
			if len(errBody.Detail) > 0 {
				em["detail"] = errBody.Detail
			}
			xyz["error"] = em
		}
		return sdkmcp.Meta{"xyz": xyz}
	}
	return func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		if pv := req.ProtocolVersion(); pv != "" && !allowed[pv] {
			return nil, fmt.Errorf("tool %q: protocol version %q is not enabled on this server", e.Name, pv)
		}
		args := map[string]any{}
		if len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return nil, errs.Wrap(errs.KindInvalidInput, err)
			}
		}
		// 接口默认值只补「客户端未提供」的键；显式入参优先（与 CLI/HTTP 一致），
		// 不能覆盖调用方传来的值。
		for k, v := range e.MCPDefaults() {
			if _, ok := args[k]; !ok {
				args[k] = v
			}
		}
		// 通道级默认参数（--default k=v）：只补缺席键。
		for k, v := range defaults {
			if _, ok := args[k]; !ok {
				args[k] = v
			}
		}
		start := time.Now()
		out, err := e.Invoke(ctx, args)
		dur := time.Since(start)
		if err != nil {
			msg := err
			if cause := errs.Cause(err); cause != nil {
				msg = cause
			}
			body := errs.ErrorBody(err)
			return &sdkmcp.CallToolResult{
				Meta:    buildMeta(dur, &body),
				IsError: true,
				Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: msg.Error()}},
			}, nil
		}
		if raw, err := json.Marshal(out); err == nil {
			if env, ok := block.DetectJSON(raw); ok {
				res, err := blockCallResult(env, toStructured(out))
				if err != nil {
					return &sdkmcp.CallToolResult{
						Meta:    buildMeta(dur, nil),
						IsError: true,
						Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: err.Error()}},
					}, nil
				}
				res.Meta = buildMeta(dur, nil)
				return res, nil
			}
		}
		if e.MCP.Output != nil {
			var buf bytes.Buffer
			if err := e.MCP.Output(&buf, out); err != nil {
				return nil, err
			}
			return &sdkmcp.CallToolResult{
				Meta:              buildMeta(dur, nil),
				Content:           []sdkmcp.Content{&sdkmcp.TextContent{Text: buf.String()}},
				StructuredContent: toStructured(out),
			}, nil
		}
		return &sdkmcp.CallToolResult{
			Meta:              buildMeta(dur, nil),
			Content:           []sdkmcp.Content{&sdkmcp.TextContent{Text: renderText(out)}},
			StructuredContent: toStructured(out),
		}, nil
	}
}

func renderText(v any) string {
	var buf bytes.Buffer
	if err := cli.Render(&buf, v); err != nil {
		return fmt.Sprintf("%v", v)
	}
	// 规范（xyz-spec §12.5）裁决：textContent 不带尾随换行——渲染器按行收尾，
	// 但 MCP 客户端把 textContent 当字符串读，尾随 \n 是噪音。
	return strings.TrimRight(buf.String(), "\n")
}

// toStructured converts a result into a plain JSON-compatible value
// (map[string]any / slices / primitives) for StructuredContent.
func toStructured(v any) any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

// toolName resolves the tool name offered to MCP clients: the per-command
// MCPHints.Name override wins over the dotted entry name, so the MCP
// namespace can differ from the CLI/HTTP naming.
func toolName(e *spec.Entry) string {
	if e.MCP.Name != "" {
		return e.MCP.Name
	}
	return e.Name
}

func toolDescription(e *spec.Entry) string {
	if e.Summary != "" && e.Description != "" {
		return e.Summary + "\n\n" + e.Description
	}
	if e.Description != "" {
		return e.Description
	}
	return e.Summary
}

func parseAnnotations(e *spec.Entry) *sdkmcp.ToolAnnotations {
	if len(e.MCP.Annotations) == 0 {
		return nil
	}
	var ann sdkmcp.ToolAnnotations
	for _, a := range e.MCP.Annotations {
		key, val, _ := strings.Cut(a, ":")
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "read":
			ann.ReadOnlyHint = true
		case "write":
			ann.ReadOnlyHint = false
		case "destructive":
			ann.DestructiveHint = boolPtr(true)
		case "idempotent":
			ann.IdempotentHint = true
		case "openworld":
			ann.OpenWorldHint = boolPtr(true)
		case "title":
			ann.Title = strings.TrimSpace(val)
		}
	}
	return &ann
}

func boolPtr(b bool) *bool { return &b }

func implName(opts Options) (string, string) {
	name := opts.Name
	if name == "" {
		name = filepath.Base(os.Args[0])
	}
	version := opts.Version
	if version == "" {
		version = "0.0.0"
	}
	return name, version
}
