package server

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAppConfigSelectsFieldsAndReportsRevision(t *testing.T) {
	target := newFakeTarget(t)
	target.respond("app.config", map[string]any{"auth": map[string]any{"enabled": true}, "ix_context": strings.Repeat("x", 10000)})
	client := discoveryClient(t, discoverySession(t, target), false)
	res, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "apps", Arguments: map[string]any{"op": "config", "name": "authentik", "fields": []string{"auth"}}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("config failed: %v", res.Content)
	}
	out := res.StructuredContent.(map[string]any)
	config := out["result"].(map[string]any)
	if len(config) != 1 || config["auth"] == nil || out["projected"] != true {
		t.Fatalf("filtered config = %v", out)
	}
	revision, _ := out["revision"].(string)
	if len(revision) != 64 {
		t.Fatalf("revision = %q", revision)
	}
	if len(res.Content[0].(*mcp.TextContent).Text) > 1024 {
		t.Fatal("metadata leaked into text response")
	}
}

func TestAppConfigExcludesMetadataWithoutChangingRevision(t *testing.T) {
	target := newFakeTarget(t)
	target.respond("app.config", map[string]any{"auth": true, "ix_context": "hidden"})
	client := discoveryClient(t, discoverySession(t, target), false)
	full, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "apps", Arguments: map[string]any{"op": "config", "name": "authentik"}})
	if err != nil || full.IsError {
		t.Fatalf("full config: %v %v", full, err)
	}
	filtered, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "apps", Arguments: map[string]any{"op": "config", "name": "authentik", "exclude_fields": []string{"ix_context"}}})
	if err != nil || filtered.IsError {
		t.Fatalf("filtered config: %v %v", filtered, err)
	}
	a := full.StructuredContent.(map[string]any)
	b := filtered.StructuredContent.(map[string]any)
	if a["revision"] == nil || a["revision"] != b["revision"] {
		t.Fatal("revision must cover full config regardless of selection")
	}
	if _, ok := b["result"].(map[string]any)["ix_context"]; ok {
		t.Fatal("excluded metadata returned")
	}
	if a["projected"] == true || b["projected"] != true {
		t.Fatal("projection flags do not describe omitted fields")
	}
}

func TestAppConfigRejectsInvalidSelection(t *testing.T) {
	for _, args := range []map[string]any{
		{"op": "config", "name": "a", "fields": []string{"missing"}},
		{"op": "config", "name": "a", "fields": []string{"auth"}, "exclude_fields": []string{"auth"}},
		{"op": "config", "name": "a", "full": true, "fields": []string{"auth"}},
		{"op": "show", "name": "a", "fields": []string{"auth"}},
		{"op": "config", "name": "a", "offset": 0},
	} {
		t.Run(strings.ReplaceAll(args["op"].(string), " ", "_"), func(t *testing.T) {
			target := newFakeTarget(t)
			target.respond("app.config", map[string]any{"auth": true, "ix_context": "hidden"})
			target.respond("app.get_instance", map[string]any{"name": "a"})
			client := discoveryClient(t, discoverySession(t, target), false)
			res, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "apps", Arguments: args})
			if err == nil && !res.IsError {
				t.Fatal("invalid selection accepted")
			}
		})
	}
}
