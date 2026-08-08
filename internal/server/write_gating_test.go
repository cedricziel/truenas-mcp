package server

import (
	"testing"

	"github.com/cedricziel/truenas-mcp/internal/tools"
)

// The write tier is off unless explicitly enabled, and enabling it is not
// something a tool can do.
func TestWritesAbsentByDefault(t *testing.T) {
	srv := NewMCPServer(MCPConfig{Version: "t", Target: "nas"}, stubSession)

	names := registeredToolNames(t, srv)
	for _, w := range tools.WriteOps() {
		if names[w.Name] {
			t.Errorf("%s must not be registered while the write tier is disabled", w.Name)
		}
	}
}

func TestWritesPresentWhenEnabled(t *testing.T) {
	srv := NewMCPServer(MCPConfig{Version: "t", Target: "nas", EnableWrites: true}, stubSession)

	names := registeredToolNames(t, srv)
	for _, w := range tools.WriteOps() {
		if !names[w.Name] {
			t.Errorf("%s should be registered when the write tier is enabled", w.Name)
		}
	}
}

// Rollback is the recovery path for every other app mutation, so it must never
// be absent while they are present.
func TestRollbackAccompaniesAppWrites(t *testing.T) {
	srv := NewMCPServer(MCPConfig{Version: "t", Target: "nas", EnableWrites: true}, stubSession)

	names := registeredToolNames(t, srv)
	if !names[tools.RollbackToolName] {
		t.Fatalf("%s must ship alongside the app mutations it recovers from",
			tools.RollbackToolName)
	}
}

func TestNoAppDeletionInV1(t *testing.T) {
	srv := NewMCPServer(MCPConfig{Version: "t", Target: "nas", EnableWrites: true}, stubSession)

	for name := range registeredToolNames(t, srv) {
		if name == "app_delete" || name == "delete_app" {
			t.Errorf("app deletion is out of scope for v1, found %q", name)
		}
	}
}

// Jobs are readable regardless of the write tier: a caller must be able to
// follow work that is already running.
func TestJobsToolAlwaysPresent(t *testing.T) {
	for _, writes := range []bool{false, true} {
		srv := NewMCPServer(MCPConfig{Version: "t", Target: "nas", EnableWrites: writes}, stubSession)
		if !registeredToolNames(t, srv)["jobs"] {
			t.Errorf("jobs tool missing with EnableWrites=%v", writes)
		}
	}
}
