package server

import "testing"

func TestContainerExecRequiresBothGates(t *testing.T) {
	for _, writes := range []bool{false, true} {
		for _, exec := range []bool{false, true} {
			srv := NewMCPServer(MCPConfig{Version: "t", Target: "nas", EnableWrites: writes, EnableExec: exec}, stubSession)
			tool := registeredTools(t, srv)["container_exec"]
			if (tool != nil) != (writes && exec) {
				t.Fatalf("writes=%v exec=%v", writes, exec)
			}
			if tool != nil && (tool.Annotations.ReadOnlyHint || tool.Annotations.IdempotentHint || tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint) {
				t.Fatal("incorrect annotations")
			}
		}
	}
}
