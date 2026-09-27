package server

import (
	"testing"
)

// pool.snapshot.create answers with the new snapshot, not a job id.
func TestCreateSnapshotReturnsTheSnapshot(t *testing.T) {
	target := newFakeTarget(t)
	snapshot := map[string]any{
		"id":            "tank/data@before",
		"name":          "tank/data@before",
		"dataset":       "tank/data",
		"snapshot_name": "before",
	}
	target.respond("pool.snapshot.create", snapshot)

	srv := NewMCPServer(MCPConfig{Version: "t", Target: "nas", EnableWrites: true}, discoverySession(t, target))
	s := newRawSession(t, srv)

	res, wireErr := s.callTool("create_snapshot", map[string]any{"target": "tank/data", "snapshot_name": "before"}, false)
	if wireErr != nil {
		t.Fatalf("create_snapshot: %v", wireErr)
	}
	if res["isError"] == true {
		t.Fatalf("create_snapshot failed: %v", res["content"])
	}
	sc, _ := res["structuredContent"].(map[string]any)
	result, _ := sc["result"].(map[string]any)
	if result["id"] != "tank/data@before" {
		t.Errorf("structuredContent = %v, want the created snapshot under result", sc)
	}
	if _, ok := sc["job_id"]; ok {
		t.Errorf("a synchronous write must not report a job id: %v", sc)
	}
}
