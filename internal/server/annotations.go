package server

import "github.com/modelcontextprotocol/go-sdk/mcp"

// Annotations are the only signal a client has for deciding whether to prompt
// before running a tool, and the spec's defaults are pessimistic:
// DestructiveHint and OpenWorldHint both default to true. Leaving a field unset
// therefore does not mean "unknown" — it means "assume the worst", and a read
// tool that stays silent gets treated as destructive.
//
// That failure is not cosmetic. Prompting on safe operations is precisely what
// teaches a user to click through the prompts that matter, so every tool here
// declares its full annotation set explicitly rather than relying on defaults.

// readAnnotations describes a tool that only reads. Repeating a read has no
// effect, and everything here reaches the TrueNAS target, so the world is open.
func readAnnotations(title string) *mcp.ToolAnnotations {
	no := false
	open := true
	return &mcp.ToolAnnotations{
		Title:           title,
		ReadOnlyHint:    true,
		DestructiveHint: &no,
		IdempotentHint:  true,
		OpenWorldHint:   &open,
	}
}

// writeAnnotations describes a mutating tool. Destructive and idempotent are
// per-operation facts, not defaults.
func writeAnnotations(title string, destructive, idempotent bool) *mcp.ToolAnnotations {
	d := destructive
	open := true
	return &mcp.ToolAnnotations{
		Title:           title,
		ReadOnlyHint:    false,
		DestructiveHint: &d,
		IdempotentHint:  idempotent,
		OpenWorldHint:   &open,
	}
}
