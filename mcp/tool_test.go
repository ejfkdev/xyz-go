package mcp

import (
	"context"
	"encoding/json"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ejfkdev/xyz-go/registry"
	"github.com/ejfkdev/xyz-go/spec"
)

// MCP 工具元数据富化 + 逐命令/逐字段覆盖（xyz-spec §12.4）。
func TestMCPToolEnrichment(t *testing.T) {
	reg := registry.New()
	type richArgs struct {
		Q string `json:"q" desc:"原始字段描述"`
	}
	if _, err := spec.Define("rich.tool", func(_ context.Context, in *richArgs) (string, error) {
		return in.Q, nil
	}).Summary("摘要").Description("详述").
		MCP(spec.MCPHints{
			Title:       "富工具",
			Description: "覆盖的描述",
			Meta:        map[string]any{"group": "demo"},
			Annotations: []string{"read"},
			Fields:      map[string]spec.MCPFieldHint{"q": {Description: "覆盖字段描述"}},
		}).Register(reg); err != nil {
		t.Fatal(err)
	}
	cs, _ := connectPair(t, reg, Options{})
	var tool *sdkmcp.Tool
	for tl, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatalf("Tools: %v", err)
		}
		if tl.Name == "rich.tool" {
			tool = tl
			break
		}
	}
	if tool == nil {
		t.Fatal("rich.tool not listed")
	}
	// Description 覆盖优先于 summary+description 合并。
	if tool.Description != "覆盖的描述" {
		t.Fatalf("description = %q, want the override", tool.Description)
	}
	// Title 落到 annotations.title，且不丢 read 注解。
	if tool.Annotations == nil || tool.Annotations.Title != "富工具" {
		t.Fatalf("annotations.title = %+v", tool.Annotations)
	}
	if !tool.Annotations.ReadOnlyHint {
		t.Fatalf("read annotation lost: %+v", tool.Annotations)
	}
	// 自定义 _meta。
	if tool.Meta["group"] != "demo" {
		t.Fatalf("tool _meta = %v", tool.Meta)
	}
	// 字段描述覆盖进 inputSchema。
	raw, _ := json.Marshal(tool.InputSchema)
	var sch map[string]any
	if err := json.Unmarshal(raw, &sch); err != nil {
		t.Fatalf("inputSchema not JSON: %v", err)
	}
	props, _ := sch["properties"].(map[string]any)
	q, _ := props["q"].(map[string]any)
	if q == nil || q["description"] != "覆盖字段描述" {
		t.Fatalf("field description override missing: %v", q)
	}
}

// 无覆盖时：description = summary + description 合并，字段描述取自 desc tag。
func TestMCPToolDefaults(t *testing.T) {
	reg := registry.New()
	type dArgs struct {
		Q string `json:"q" desc:"关键词"`
	}
	if _, err := spec.Define("plain.tool", func(_ context.Context, in *dArgs) (string, error) {
		return in.Q, nil
	}).Summary("摘要").Description("详述").Register(reg); err != nil {
		t.Fatal(err)
	}
	cs, _ := connectPair(t, reg, Options{})
	for tl, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		if tl.Name != "plain.tool" {
			continue
		}
		if tl.Description != "摘要\n\n详述" {
			t.Fatalf("default description = %q", tl.Description)
		}
		raw, _ := json.Marshal(tl.InputSchema)
		var sch map[string]any
		_ = json.Unmarshal(raw, &sch)
		q := sch["properties"].(map[string]any)["q"].(map[string]any)
		if q["description"] != "关键词" {
			t.Fatalf("field description from tag = %v", q["description"])
		}
		return
	}
	t.Fatal("plain.tool not listed")
}
