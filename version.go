package xyz

// SDKVersion 是 xyz-go 库自身的版本（与 git tag 一致）。它在每次发布时
// 手工同步（见 RELEASING.md），用于 HTTP 的 X-XYZ-Version 响应头与 MCP 的
// _meta.xyz.sdk_version / serverInfo，让调用方知道是哪个 xyz 构建在服务。
// 这与「使用 xyz 的应用程序自己的版本」是两回事——后者见 Version。
const SDKVersion = "0.4.2"

// Version 是*应用程序*（用 xyz 构建的那个程序）的版本，由根派发器的
// -v/--version、HTTP 的 X-App-Version 响应头与 MCP 的 serverInfo.version /
// _meta.xyz.app_version 报告。两种设置方式：
//
//   - 代码里写死或运行时赋值：xyz.Version = "myapp 2.1.3"
//     （或 xyz.Config{Version: "myapp 2.1.3"}，后者优先级更高）；
//   - 构建期注入：-ldflags "-X github.com/ejfkdev/xyz-go.Version=v1.2.3"。
//
// 默认 "dev"。注意它位于 xyz-go 包内、但承载的是*应用*的版本——这是为了让
// 应用能用单条 ldflags 注入而无需自己再声明变量。cli 前端为直接嵌入场景
// 另保留一个 cli.Version。
var Version = "dev"
