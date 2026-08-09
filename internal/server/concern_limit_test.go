package server

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestLimitBoundsAnOpThatNeverDeclaredIt is the end-to-end counterpart to
// TestLimitIsNeverRefused: it proves limit does not just avoid being refused
// but actually does its job -- bounding the response and reporting the
// truncation -- on storage's list_pools, an op that has never declared
// "limit" in its Args (pool.query takes no arguments in this concern at
// all). Before the fix this call failed outright with "does not accept
// limit"; shape() was always the thing bounding the response, so once
// Resolve stops standing in the way, this works exactly as it does for the
// three ops that used to declare limit specially.
func TestLimitBoundsAnOpThatNeverDeclaredIt(t *testing.T) {
	target := newFakeTarget(t)
	target.respond("pool.query", []map[string]any{
		{"id": 1, "name": "tank"},
		{"id": 2, "name": "backup"},
		{"id": 3, "name": "scratch"},
		{"id": 4, "name": "archive"},
	})
	client := discoveryClient(t, discoverySession(t, target), false)

	res, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "storage",
		Arguments: map[string]any{"op": "list_pools", "limit": 2},
	})
	if err != nil {
		t.Fatalf("call storage: %v", err)
	}
	if res.IsError {
		t.Fatalf("storage returned an error: %v", res.Content)
	}

	out, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured content, got %T", res.StructuredContent)
	}

	items, ok := out["result"].([]any)
	if !ok {
		t.Fatalf("expected result to be a list, got %T", out["result"])
	}
	if len(items) != 2 {
		t.Errorf("result has %d items, want 2 -- limit=2 must bound list_pools even though "+
			"it never declared limit in its Args", len(items))
	}
	if total, _ := out["total"].(float64); total != 4 {
		t.Errorf("total = %v, want 4", out["total"])
	}
	if truncated, _ := out["truncated"].(bool); !truncated {
		t.Error("truncated must be true: 4 pools were cut down to 2, and a bounded answer " +
			"that does not say so reads as a complete one")
	}
}
