package mcp

import (
	"encoding/base64"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ejfkdev/xyz-go/block"
)

// blockCallResult 把 §12.7 信封投影为 MCP 结果：块原样进 Content
// （text → TextContent，image → ImageContent），信封进 StructuredContent。
func blockCallResult(env *block.Envelope, structured any) (*sdkmcp.CallToolResult, error) {
	content := make([]sdkmcp.Content, 0, len(env.Content))
	for _, it := range env.Content {
		switch it.Type {
		case "text":
			content = append(content, &sdkmcp.TextContent{Text: it.Text})
		case "image":
			data, err := base64.StdEncoding.DecodeString(it.Data)
			if err != nil {
				return nil, err
			}
			content = append(content, &sdkmcp.ImageContent{
				Data:     data,
				MIMEType: it.MIMEType,
			})
		}
	}
	return &sdkmcp.CallToolResult{Content: content, StructuredContent: structured}, nil
}
