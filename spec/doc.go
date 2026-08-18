// Package spec is the single source of truth for a command: one Go struct
// with tags is analyzed once and produces the metadata every frontend needs
// (CLI flags, HTTP bindings, MCP JSON Schema) plus an Invoke closure that
// decodes transport-shaped input (map[string]any) into the typed argument
// struct, applies defaults and validation, and runs the handler.
//
// Frontends never import the typed command: they consume *Entry and its
// metadata, so a new transport can be added without touching the core. The
// contract every frontend implements is "reduce its transport's input to a
// map[string]any, call Entry.Invoke, and render the returned any or coded
// error" — the shared pipeline lives here, the rendering strategies live in
// each frontend.
package spec
