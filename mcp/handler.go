package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ejfkdev/xyz-go/cli"
	errs "github.com/ejfkdev/xyz-go/errors"
	"github.com/ejfkdev/xyz-go/spec"
)

func makeHandler(e *spec.Entry, allowed map[string]bool) func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
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
		out, err := e.Invoke(ctx, args)
		if err != nil {
			msg := err
			if cause := errs.Cause(err); cause != nil {
				msg = cause
			}
			return &sdkmcp.CallToolResult{
				IsError: true,
				Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: msg.Error()}},
			}, nil
		}
		return &sdkmcp.CallToolResult{
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
	return buf.String()
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
