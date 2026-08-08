package server

import "testing"

// Annotations are what a client uses to decide whether to prompt, so they have
// to be accurate per tool. A safe operation wrongly marked destructive trains
// users to click through prompts; a destructive one wrongly marked safe runs
// unannounced. Both failures are worse than no annotation at all.
func TestReadToolsAreReadOnlyAndNotDestructive(t *testing.T) {
	srv := NewMCPServer(MCPConfig{Version: "t", Target: "nas", EnableWrites: true}, stubSession)

	readTools := []string{"storage", "system", "apps", "jobs", "server_info", "system_info"}
	all := registeredTools(t, srv)

	for _, name := range readTools {
		tool, ok := all[name]
		if !ok {
			t.Errorf("read tool %q is missing", name)
			continue
		}
		if tool.Annotations == nil {
			t.Errorf("%s carries no annotations", name)
			continue
		}
		if !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s must be annotated read-only", name)
		}
		if tool.Annotations.DestructiveHint != nil && *tool.Annotations.DestructiveHint {
			t.Errorf("%s must not be annotated destructive", name)
		}
	}
}

func TestMutatingToolsAreNotReadOnly(t *testing.T) {
	srv := NewMCPServer(MCPConfig{Version: "t", Target: "nas", EnableWrites: true}, stubSession)
	all := registeredTools(t, srv)

	for _, name := range []string{
		"app_pull_images", "app_redeploy", "app_start", "app_stop",
		"app_upgrade", "app_rollback", "create_snapshot",
	} {
		tool, ok := all[name]
		if !ok {
			t.Errorf("write tool %q is missing", name)
			continue
		}
		if tool.Annotations != nil && tool.Annotations.ReadOnlyHint {
			t.Errorf("%s mutates and must not be annotated read-only", name)
		}
	}
}

// Interrupting someone's running service warrants a prompt; creating a
// snapshot does not.
func TestDestructiveHintsMatchBlastRadius(t *testing.T) {
	srv := NewMCPServer(MCPConfig{Version: "t", Target: "nas", EnableWrites: true}, stubSession)
	all := registeredTools(t, srv)

	for _, name := range []string{"app_stop", "app_redeploy", "app_pull_images", "app_upgrade"} {
		tool := all[name]
		if tool == nil || tool.Annotations == nil ||
			tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint {
			t.Errorf("%s interrupts a service and must be annotated destructive", name)
		}
	}

	for _, name := range []string{"app_start", "create_snapshot"} {
		tool := all[name]
		if tool == nil || tool.Annotations == nil {
			t.Errorf("%s is missing or unannotated", name)
			continue
		}
		if tool.Annotations.DestructiveHint != nil && *tool.Annotations.DestructiveHint {
			t.Errorf("%s is additive and must not be annotated destructive; "+
				"prompting on safe operations trains users to ignore prompts", name)
		}
	}
}
