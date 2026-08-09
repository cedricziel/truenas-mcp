package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cedricziel/truenas-mcp/internal/tools"
)

// The write tools used to share one input struct, so every tool advertised
// `redeploy` and `snapshot_name` whether or not it consumed them. A model
// deciding what to pass had no way to tell a decorative parameter from a real
// one, and a careful caller who noticed the identical schemas on app_redeploy
// and app_pull_images had reason to distrust both. Each tool's schema must now
// carry only the options its WriteOp actually declares.
func TestWriteSchemasCarryOnlyTheirDeclaredOptions(t *testing.T) {
	srv := NewMCPServer(MCPConfig{Version: "t", Target: "nas", EnableWrites: true}, stubSession)
	all := registeredTools(t, srv)

	for _, w := range tools.WriteOps() {
		tool, ok := all[w.Name]
		if !ok {
			t.Errorf("%s is missing", w.Name)
			continue
		}

		props := schemaProperties(t, w.Name, tool.InputSchema)
		if _, has := props["target"]; !has {
			t.Errorf("%s: schema is missing target", w.Name)
		}

		wantRedeploy := hasOption(w.Options, "redeploy")
		if _, has := props["redeploy"]; has != wantRedeploy {
			t.Errorf("%s: redeploy in schema = %v, want %v (declared options: %v)",
				w.Name, has, wantRedeploy, w.Options)
		}

		wantSnapshotName := hasOption(w.Options, "snapshot_name")
		if _, has := props["snapshot_name"]; has != wantSnapshotName {
			t.Errorf("%s: snapshot_name in schema = %v, want %v (declared options: %v)",
				w.Name, has, wantSnapshotName, w.Options)
		}
	}
}

func hasOption(options []string, name string) bool {
	for _, o := range options {
		if o == name {
			return true
		}
	}
	return false
}

// schemaProperties decodes a tool's input or output schema down to its
// top-level property names, however the SDK happens to have typed the field.
func schemaProperties(t *testing.T, toolName string, schema any) map[string]any {
	t.Helper()

	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("%s: marshal schema: %v", toolName, err)
	}
	var decoded struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("%s: decode schema: %v", toolName, err)
	}
	return decoded.Properties
}

// A model decides whether to call a mutating tool before it sees the result,
// so the fact that the call is asynchronous -- it returns a job id, not a
// finished result, and needs a follow-up call to see completion -- has to be
// visible in the description at that point. It used to only be discoverable
// in the response, after the call already happened.
func TestWriteDescriptionsStateTheAsyncContract(t *testing.T) {
	srv := NewMCPServer(MCPConfig{Version: "t", Target: "nas", EnableWrites: true}, stubSession)
	all := registeredTools(t, srv)

	for _, w := range tools.WriteOps() {
		tool, ok := all[w.Name]
		if !ok {
			t.Errorf("%s is missing", w.Name)
			continue
		}
		if !strings.Contains(tool.Description, asyncContractNote) {
			t.Errorf("%s description doesn't state the async contract: %q", w.Name, tool.Description)
		}
		// The per-op description must still be there, not replaced by the
		// suffix -- the suffix documents the mechanism, not the decision.
		if !strings.HasPrefix(tool.Description, w.Description) {
			t.Errorf("%s: async suffix must be appended, not substituted for the op's own description", w.Name)
		}
	}
}
