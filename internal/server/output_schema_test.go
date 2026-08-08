package server

import (
	"encoding/json"
	"testing"
)

// A Go `any` field with no description generates the bare `true` schema --
// JSON Schema's "anything goes". Some MCP clients reject that outright, and a
// client that cannot parse the tool list drops the whole server, not just the
// offending tool. Every property must therefore carry an object schema.
func TestNoOutputSchemaUsesTheBareTrueSchema(t *testing.T) {
	srv := NewMCPServer(MCPConfig{Version: "t", Target: "nas", EnableWrites: true}, stubSession)

	for name, tool := range registeredTools(t, srv) {
		if tool.OutputSchema == nil {
			continue
		}
		raw, err := json.Marshal(tool.OutputSchema)
		if err != nil {
			t.Fatalf("%s: marshal schema: %v", name, err)
		}

		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("%s: decode schema: %v", name, err)
		}
		if path := findBareTrue(schema, "properties"); path != "" {
			t.Errorf("%s: output schema has a bare `true` at %s; "+
				"give the field a jsonschema description so it emits an object schema",
				name, path)
		}
	}
}

// findBareTrue walks a schema looking for a property whose schema is literally
// `true` rather than an object.
func findBareTrue(node any, path string) string {
	switch v := node.(type) {
	case bool:
		if v {
			return path
		}
	case map[string]any:
		for key, child := range v {
			if found := findBareTrue(child, path+"."+key); found != "" {
				// `additionalProperties: true` is legitimate; only property
				// schemas matter here.
				if key != "additionalProperties" {
					return found
				}
			}
		}
	case []any:
		for i, child := range v {
			if found := findBareTrue(child, path+"[]"); found != "" {
				_ = i
				return found
			}
		}
	}
	return ""
}
