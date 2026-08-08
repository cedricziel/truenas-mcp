package server

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Annotations are the only signal a client has for deciding whether to prompt.
// Leaving one unset means the client falls back to its own default -- and the
// spec defaults are DestructiveHint=true, OpenWorldHint=true. A read tool that
// omits them is therefore treated as destructive, which produces prompts on
// safe operations, which is what trains users to click through the prompts that
// actually matter.
func TestEveryToolDeclaresACompleteAnnotationSet(t *testing.T) {
	srv := NewMCPServer(MCPConfig{Version: "t", Target: "nas", EnableWrites: true}, stubSession)

	for name, tool := range registeredTools(t, srv) {
		a := tool.Annotations
		if a == nil {
			t.Errorf("%s: no annotations at all", name)
			continue
		}
		if a.Title == "" {
			t.Errorf("%s: no Title; clients show the raw tool name in consent prompts without one", name)
		}
		if a.OpenWorldHint == nil {
			t.Errorf("%s: OpenWorldHint unset; every tool here reaches an external system", name)
		}
		if a.DestructiveHint == nil {
			t.Errorf("%s: DestructiveHint unset; the spec default is true, so silence reads as destructive", name)
		}
	}
}

// Every tool talks to the TrueNAS target, which is by definition an open world.
func TestAllToolsDeclareAnOpenWorld(t *testing.T) {
	srv := NewMCPServer(MCPConfig{Version: "t", Target: "nas", EnableWrites: true}, stubSession)

	for name, tool := range registeredTools(t, srv) {
		if tool.Annotations == nil || tool.Annotations.OpenWorldHint == nil {
			continue // reported by the completeness test
		}
		if !*tool.Annotations.OpenWorldHint {
			t.Errorf("%s claims a closed world, but it reaches the TrueNAS target", name)
		}
	}
}

// A read tool must say so explicitly rather than relying on a client's default.
func TestReadToolsDeclareNonDestructiveExplicitly(t *testing.T) {
	srv := NewMCPServer(MCPConfig{Version: "t", Target: "nas", EnableWrites: true}, stubSession)
	all := registeredTools(t, srv)

	for _, name := range []string{
		"storage", "system", "apps", "jobs",
		"server_info", "system_info", "search_methods", "describe_method",
	} {
		tool, ok := all[name]
		if !ok {
			t.Errorf("read tool %q missing", name)
			continue
		}
		if !readOnlyAndExplicitlySafe(tool) {
			t.Errorf("%s must declare readOnly=true, destructive=false, idempotent=true explicitly", name)
		}
	}
}

func readOnlyAndExplicitlySafe(tool *mcp.Tool) bool {
	a := tool.Annotations
	return a != nil &&
		a.ReadOnlyHint &&
		a.DestructiveHint != nil && !*a.DestructiveHint &&
		a.IdempotentHint
}

// Titles are shown to a human in a consent dialog, so they must read as
// language rather than as identifiers.
func TestTitlesAreHumanReadable(t *testing.T) {
	srv := NewMCPServer(MCPConfig{Version: "t", Target: "nas", EnableWrites: true}, stubSession)

	for name, tool := range registeredTools(t, srv) {
		if tool.Annotations == nil || tool.Annotations.Title == "" {
			continue // reported by the completeness test
		}
		if tool.Annotations.Title == name {
			t.Errorf("%s: Title just repeats the tool name; it should read as a phrase", name)
		}
	}
}
