package xyz

// Version is the version reported by the -v/--version handling in Run /
// Main. Override it in code, or inject at build time with
// -ldflags "-X github.com/ejfkdev/xyz-go.Version=v1.2.3". (The cli frontend
// keeps its own Version for direct embedding via cli.Run.)
var Version = "dev"
