package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cedricziel/truenas-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newCaveatFakeTarget wires up just enough of core.get_methods for the
// discovery tools to describe and invoke app.update -- the one method this
// server has an established caveat for -- plus a caveat-free read method,
// pool.query, so the "no caveat" path is exercised against something real
// rather than an absent lookup.
func newCaveatFakeTarget(t *testing.T) *fakeTarget {
	t.Helper()

	f := newFakeTarget(t)
	f.respond("core.get_methods", map[string]any{
		"app.update": map[string]any{
			"description": "Update `app_name` app with new configuration.",
			"job":         true,
			"roles":       []string{"APPS_WRITE"},
			"accepts": []map[string]any{
				{"_name_": "app_name", "_required_": true, "type": "string", "description": "", "default": nil},
				{"_name_": "update", "_required_": false, "type": "object", "description": "", "default": nil},
			},
		},
		"pool.query": map[string]any{
			"description": "Query pools.",
			"job":         false,
			"roles":       []string{"READONLY_ADMIN"},
			"accepts":     []map[string]any{},
		},
	})
	f.respond("app.update", 42)
	return f
}

// discoverySession opens one real session against target and hands back a
// sessionFor that always returns it, bypassing the HTTP credential layer this
// package's tools do not otherwise exercise.
func discoverySession(t *testing.T, target *fakeTarget) sessionFor {
	t.Helper()

	sessions := NewSessionManager(target.URL(), false)
	t.Cleanup(sessions.CloseAll)

	sess, err := sessions.Open(context.Background(), fakeTargetAPIKey)
	if err != nil {
		t.Fatalf("open session against fake target: %v", err)
	}
	return func(context.Context) (*Session, error) { return sess, nil }
}

// discoveryClient builds a server around session and connects a real MCP
// client to it, the same pattern registeredTools uses for surface-only tests.
func discoveryClient(t *testing.T, session sessionFor, enableWrites bool) *mcp.ClientSession {
	t.Helper()

	srv := NewMCPServer(MCPConfig{Version: "t", Target: "nas", EnableWrites: enableWrites}, session)
	httpSrv := httptest.NewServer(mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv }, nil))
	t.Cleanup(httpSrv.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(context.Background(),
		&mcp.StreamableClientTransport{Endpoint: httpSrv.URL}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

// An agent that reads before calling should see app.update's caveat without
// having to already know to distrust the middleware's own description.
func TestDescribeMethodReturnsCaveatForACaveatedMethod(t *testing.T) {
	target := newCaveatFakeTarget(t)
	client := discoveryClient(t, discoverySession(t, target), true)

	res, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "describe_method",
		Arguments: map[string]any{"method": "app.update"},
	})
	if err != nil {
		t.Fatalf("call describe_method: %v", err)
	}
	if res.IsError {
		t.Fatalf("describe_method returned an error: %v", res.Content)
	}

	out, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured content, got %T", res.StructuredContent)
	}
	caveat, _ := out["caveat"].(string)
	if caveat == "" {
		t.Fatal("describe_method must surface app.update's caveat")
	}
	if caveat != tools.MethodCaveat("app.update") {
		t.Errorf("caveat = %q, want %q", caveat, tools.MethodCaveat("app.update"))
	}
}

// describe_method is skippable; call_method is not, since it is the call
// itself. An agent that goes straight there must still see the caveat, and
// must still see the job follow-up note that call_method already attaches to
// every job method -- one must not silently replace the other.
func TestCallMethodComposesCaveatWithJobNote(t *testing.T) {
	target := newCaveatFakeTarget(t)
	client := discoveryClient(t, discoverySession(t, target), true)

	res, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "call_method",
		Arguments: map[string]any{
			"method": "app.update",
			"params": []any{"myapp", map[string]any{"values": map[string]any{}}},
		},
	})
	if err != nil {
		t.Fatalf("call call_method: %v", err)
	}
	if res.IsError {
		t.Fatalf("call_method returned an error: %v", res.Content)
	}

	out, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured content, got %T", res.StructuredContent)
	}
	note, _ := out["note"].(string)
	if !strings.Contains(note, tools.MethodCaveat("app.update")) {
		t.Errorf("note must include the caveat, got: %s", note)
	}
	if !strings.Contains(note, "Started") {
		t.Errorf("note must still include the job follow-up, got: %s", note)
	}
}

// A method with no established caveat must not grow a field for one: an
// absent key is the honest signal that nothing has been established, whereas
// an empty string next to a populated one on another method invites a caller
// to wonder whether it was simply forgotten.
func TestDescribeMethodOmitsCaveatFieldForMethodWithoutOne(t *testing.T) {
	target := newCaveatFakeTarget(t)
	client := discoveryClient(t, discoverySession(t, target), true)

	res, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "describe_method",
		Arguments: map[string]any{"method": "pool.query"},
	})
	if err != nil {
		t.Fatalf("call describe_method: %v", err)
	}
	if res.IsError {
		t.Fatalf("describe_method returned an error: %v", res.Content)
	}

	out, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured content, got %T", res.StructuredContent)
	}
	if v, present := out["caveat"]; present {
		t.Errorf("pool.query has no established caveat; the field must be omitted, got: %v", v)
	}
}

// findBooleanItems walks a decoded schema document looking for a JSON object
// whose "items" member is a bare boolean rather than a schema object. Draft
// 2020-12 allows a boolean there -- it is shorthand for "matches anything"
// (true) or "matches nothing" (false) -- but llama.cpp's schema-to-grammar
// compiler does not implement boolean subschemas and rejects the request
// outright wherever one appears. The walk covers every nesting a schema can
// put an items keyword in (nested arrays, properties, anyOf/oneOf branches,
// $defs, ...), because the regression this guards against is a *future*
// field with an unconstrained element type, not only call_method's params.
func findBooleanItems(node any, path string) string {
	switch v := node.(type) {
	case map[string]any:
		if _, ok := v["items"].(bool); ok {
			return path + ".items"
		}
		for key, child := range v {
			if found := findBooleanItems(child, path+"."+key); found != "" {
				return found
			}
		}
	case []any:
		for i, child := range v {
			if found := findBooleanItems(child, fmt.Sprintf("%s[%d]", path, i)); found != "" {
				return found
			}
		}
	}
	return ""
}

// decodeSchema marshals whatever concrete type the SDK handed back for a
// schema (a *jsonschema.Schema, a map, ...) down to a plain map, mirroring
// what actually goes over the wire rather than asserting on the Go type.
func decodeSchema(t *testing.T, toolName, which string, schema any) map[string]any {
	t.Helper()
	if schema == nil {
		return nil
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("%s: marshal %s schema: %v", toolName, which, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		// A schema that is itself the bare boolean `true` decodes to a
		// non-object here; that shape is covered by TestNoOutputSchemaUsesTheBareTrueSchema
		// and isn't what this test is checking for.
		return nil
	}
	return decoded
}

// This is the regression test: it must fail the moment any tool's schema --
// today or after some future change -- puts a bare boolean where an items
// schema belongs, because that single shape is enough for llama.cpp to
// refuse every tool call this server offers, not just the one tool whose
// schema carries it. Verified against current main before the fix in
// discovery_tool.go: call_method's params property fails this check with
// `"items": true`.
func TestNoRegisteredSchemaHasABooleanItemsPosition(t *testing.T) {
	for _, writesEnabled := range []bool{true, false} {
		srv := NewMCPServer(MCPConfig{Version: "t", Target: "nas", EnableWrites: writesEnabled}, stubSession)
		for name, tool := range registeredTools(t, srv) {
			if in := decodeSchema(t, name, "input", tool.InputSchema); in != nil {
				if path := findBooleanItems(in, "input"); path != "" {
					t.Errorf("writesEnabled=%v: %s: boolean items schema at %s", writesEnabled, name, path)
				}
			}
			if out := decodeSchema(t, name, "output", tool.OutputSchema); out != nil {
				if path := findBooleanItems(out, "output"); path != "" {
					t.Errorf("writesEnabled=%v: %s: boolean items schema at %s", writesEnabled, name, path)
				}
			}
		}
	}
}

// call_method's params is a positional []any -- deliberately unconstrained,
// since the middleware method decides its own argument shapes -- so its
// items schema is JSON Schema's "matches anything". That is spelled either
// as the boolean `true` or as the empty object `{}`; both are correct draft
// 2020-12, but only the object form is something llama.cpp's schema
// compiler accepts. This test pins the wire shape directly, not a Go type,
// since the bug lived entirely in how the schema serializes.
func TestCallMethodParamsItemsIsObjectForm(t *testing.T) {
	srv := NewMCPServer(MCPConfig{Version: "t", Target: "nas", EnableWrites: true}, stubSession)
	tool, ok := registeredTools(t, srv)["call_method"]
	if !ok {
		t.Fatal("call_method is not registered")
	}

	schema := decodeSchema(t, "call_method", "input", tool.InputSchema)
	props, _ := schema["properties"].(map[string]any)
	params, _ := props["params"].(map[string]any)
	if params == nil {
		t.Fatal("call_method's input schema has no params property")
	}

	items, hasItems := params["items"]
	if !hasItems {
		t.Fatal("call_method's params property has no items schema")
	}
	itemsObj, ok := items.(map[string]any)
	if !ok {
		t.Fatalf("params.items = %#v (%T); want an object schema, not a bare boolean", items, items)
	}
	if len(itemsObj) != 0 {
		t.Errorf("params.items = %#v; want the empty object schema {}", itemsObj)
	}
}

// additionalProperties: false is a boolean subschema too, and llama.cpp
// compiles it fine -- verified separately, it is only booleans in items
// position that its grammar compiler rejects. This test proves the items
// fix didn't get generalized into rewriting every boolean subschema: a
// naive "rewrite all booleans to objects" pass would turn this `false` into
// `{"not": {}}`, which is a different, weaker constraint than "no
// additional properties allowed". call_method's own input schema carries
// this key because CallInput is a struct, so it doubles as the "real tool"
// this property is checked on.
func TestCallMethodAdditionalPropertiesStaysBoolean(t *testing.T) {
	srv := NewMCPServer(MCPConfig{Version: "t", Target: "nas", EnableWrites: true}, stubSession)
	tool, ok := registeredTools(t, srv)["call_method"]
	if !ok {
		t.Fatal("call_method is not registered")
	}

	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("marshal call_method input schema: %v", err)
	}
	var decoded struct {
		AdditionalProperties json.RawMessage `json:"additionalProperties"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode call_method input schema: %v", err)
	}
	if got := string(decoded.AdditionalProperties); got != "false" {
		t.Errorf("call_method's additionalProperties = %s; want the bare boolean false", got)
	}
}
