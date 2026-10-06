package mcp

import (
	"context"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCP 服务器上下文：serverInfo.version 接线 + 每次调用结果 _meta.xyz
//（版本/命令/耗时/自定义头/错误上下文）。xyz-spec §12.8。

func TestMCPResultServerMeta(t *testing.T) {
	cs, _ := connectPair(t, buildReg(t), Options{
		Name:            "demoapp",
		Version:         "v9.9.9",
		SDKVersion:      "0.4.2",
		ResponseHeaders: map[string]string{"X-App": "demo"},
	})
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "math.sum",
		Arguments: map[string]any{"a": float64(5)},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	xyz, ok := res.Meta["xyz"].(map[string]any)
	if !ok {
		t.Fatalf("result _meta.xyz missing: %#v", res.Meta)
	}
	if xyz["app_name"] != "demoapp" {
		t.Fatalf("meta app_name = %v", xyz["app_name"])
	}
	if xyz["app_version"] != "v9.9.9" {
		t.Fatalf("meta app_version = %v", xyz["app_version"])
	}
	if xyz["sdk_version"] != "0.4.2" {
		t.Fatalf("meta sdk_version = %v", xyz["sdk_version"])
	}
	if xyz["command"] != "math.sum" {
		t.Fatalf("meta command = %v", xyz["command"])
	}
	if _, ok := xyz["duration_ms"]; !ok {
		t.Fatalf("meta duration_ms missing: %#v", xyz)
	}
	hdrs, _ := xyz["headers"].(map[string]any)
	if hdrs["X-App"] != "demo" {
		t.Fatalf("meta headers = %v", xyz["headers"])
	}
}

func TestMCPErrorMeta(t *testing.T) {
	cs, _ := connectPair(t, buildReg(t), Options{Version: "v1"})
	// sumHandler 在 a==404 时返回 KindNotFound。
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "math.sum",
		Arguments: map[string]any{"a": float64(404)},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected isError result")
	}
	xyz, _ := res.Meta["xyz"].(map[string]any)
	errMeta, _ := xyz["error"].(map[string]any)
	if errMeta["kind"] != "not_found" {
		t.Fatalf("error meta kind = %v (xyz=%#v)", errMeta["kind"], xyz)
	}
}

func TestMCPNoServerMeta(t *testing.T) {
	cs, _ := connectPair(t, buildReg(t), Options{Version: "v1", NoServerMeta: true})
	res, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "math.sum",
		Arguments: map[string]any{"a": float64(5)},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	// SDK 自身仍会注入 io.modelcontextprotocol/serverInfo；NoServerMeta 只
	// 抑制我们自己的 xyz 上下文键。
	if _, ok := res.Meta["xyz"]; ok {
		t.Fatalf("NoServerMeta must suppress _meta.xyz, got %#v", res.Meta["xyz"])
	}
}

func TestMCPImplNameVersion(t *testing.T) {
	if _, version := implName(Options{Version: "v9.9.9"}); version != "v9.9.9" {
		t.Fatalf("implName version = %q", version)
	}
	// 空版本回退 0.0.0：serverInfo.version 永不为空。
	if _, v := implName(Options{}); v != "0.0.0" {
		t.Fatalf("implName default version = %q", v)
	}
}
