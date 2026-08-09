package server

import (
	"context"
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
