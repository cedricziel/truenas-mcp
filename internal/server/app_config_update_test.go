package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAppConfigUpdatePreservesCompleteConfig(t *testing.T) {
	target := newFakeTarget(t)
	config := map[string]any{"hidden": strings.Repeat("secret", 1000), "large_integer": json.Number("9007199254740993"), "nested": map[string]any{"keep": "sibling", "change": "before"}, "list": []any{"a", "b"}, "nullable": "before"}
	target.respond("app.config", config)
	target.respond("app.update", 42)
	client := discoveryClient(t, discoverySession(t, target), true)
	read, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "apps", Arguments: map[string]any{"op": "config", "name": "demo", "fields": []string{"nested"}}})
	if err != nil {
		t.Fatal(err)
	}
	if read.IsError {
		t.Fatalf("read config failed: %v", read.Content)
	}
	snapshot := read.StructuredContent.(map[string]any)
	if snapshot["projected"] != true || len(snapshot["result"].(map[string]any)) != 1 {
		t.Fatalf("filtered config = %v", snapshot)
	}
	if strings.Contains(string(mustMarshalUpdateResult(t, read)), "secret") {
		t.Fatal("filtered read exposed hidden configuration")
	}
	result, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "app_update_config", Arguments: map[string]any{"target": "demo", "expected_revision": snapshot["revision"], "changes": map[string]any{"new_integer": json.Number("9007199254740995"), "nested": map[string]any{"change": "after"}, "list": []any{"c"}, "nullable": nil}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("update failed: %v", result.Content)
	}
	output := result.StructuredContent.(map[string]any)
	if output["job_id"] != float64(42) || output["target"] != "demo" || output["resource"] != "truenas://job/42" || output["method"] != "app.update" {
		t.Fatalf("job output = %v", output)
	}
	if !strings.Contains(output["note"].(string), "not yet finished") {
		t.Fatalf("missing async completion note: %v", output)
	}
	raw, ok := target.lastParams("app.update")
	if !ok {
		t.Fatal("app.update was not called")
	}
	var params []json.RawMessage
	if err := json.Unmarshal(raw, &params); err != nil {
		t.Fatal(err)
	}
	if len(params) != 2 {
		t.Fatalf("update params = %s", params)
	}
	var payload struct {
		Values map[string]json.RawMessage `json:"values"`
	}
	if err := json.Unmarshal(params[1], &payload); err != nil {
		t.Fatal(err)
	}
	if string(payload.Values["hidden"]) != `"`+strings.Repeat("secret", 1000)+`"` {
		t.Fatal("hidden configuration was lost")
	}
	if string(payload.Values["new_integer"]) != "9007199254740995" {
		t.Fatalf("new integer precision was lost: %s", payload.Values["new_integer"])
	}
	if string(payload.Values["large_integer"]) != "9007199254740993" {
		t.Fatal("integer precision was lost")
	}
	if string(payload.Values["nested"]) != `{"change":"after","keep":"sibling"}` {
		t.Fatalf("nested config = %s", payload.Values["nested"])
	}
	if string(payload.Values["list"]) != `["c"]` || string(payload.Values["nullable"]) != "null" {
		t.Fatalf("replacement values = %s", params[1])
	}
	if strings.Contains(string(mustMarshalUpdateResult(t, result)), "secret") {
		t.Fatal("response exposed configuration")
	}
}

func mustMarshalUpdateResult(t *testing.T, result *mcp.CallToolResult) []byte {
	t.Helper()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestAppConfigUpdateRejectsUnsafeInputsWithoutWriting(t *testing.T) {
	for _, tc := range []struct {
		name     string
		config   any
		changes  any
		revision string
	}{
		{"stale", map[string]any{"a": 1}, map[string]any{"a": 2}, "stale"},
		{"null config", nil, map[string]any{"a": 2}, "revision"},
		{"array config", []any{}, map[string]any{"a": 2}, "revision"},
		{"empty changes", map[string]any{}, map[string]any{}, "revision"},
		{"missing changes", map[string]any{}, nil, "revision"},
		{"missing revision", map[string]any{}, map[string]any{"a": 2}, ""},
		{"reserved context", map[string]any{}, map[string]any{"ix_context": nil}, "revision"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := newFakeTarget(t)
			target.respond("app.config", tc.config)
			target.respond("app.update", 42)
			client := discoveryClient(t, discoverySession(t, target), true)
			result, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "app_update_config", Arguments: map[string]any{"target": "demo", "changes": tc.changes, "expected_revision": tc.revision}})
			if err == nil && !result.IsError {
				t.Fatal("unsafe update succeeded")
			}
			if _, called := target.lastParams("app.update"); called {
				t.Fatal("unsafe input reached app.update")
			}
		})
	}
}

func TestAppConfigUpdateRegistrationRequiresWrites(t *testing.T) {
	readOnly := registeredTools(t, NewMCPServer(MCPConfig{Version: "t", EnableWrites: false}, stubSession))
	if _, exists := readOnly["app_update_config"]; exists {
		t.Fatal("configuration update exposed with writes disabled")
	}
	writable := registeredTools(t, NewMCPServer(MCPConfig{Version: "t", EnableWrites: true}, stubSession))
	tool, exists := writable["app_update_config"]
	if !exists {
		t.Fatal("configuration update missing with writes enabled")
	}
	if tool.Annotations == nil || tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint {
		t.Fatal("configuration update must be marked destructive")
	}
}
