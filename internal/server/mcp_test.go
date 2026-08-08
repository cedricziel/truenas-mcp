package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func connect(t *testing.T, cfg MCPConfig) *mcp.ClientSession {
	t.Helper()

	httpSrv := httptest.NewServer(NewMCPHandler(cfg))
	t.Cleanup(httpSrv.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(
		context.Background(),
		&mcp.StreamableClientTransport{Endpoint: httpSrv.URL},
		nil,
	)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

func toolNames(t *testing.T, sess *mcp.ClientSession) map[string]*mcp.Tool {
	t.Helper()
	res, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	out := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		out[tool.Name] = tool
	}
	return out
}

func TestServerAdvertisesTools(t *testing.T) {
	sess := connect(t, MCPConfig{Version: "test", Target: "nas.local"})

	tools := toolNames(t, sess)
	if len(tools) == 0 {
		t.Fatal("server must advertise at least one tool")
	}
	if _, ok := tools["server_info"]; !ok {
		t.Fatalf("expected a server_info tool, got: %v", tools)
	}
}

// The read tier is annotated read-only, and that annotation is what clients
// use to decide whether to prompt. See design D3.
func TestReadToolsAreAnnotatedReadOnly(t *testing.T) {
	sess := connect(t, MCPConfig{Version: "test", Target: "nas.local"})

	for name, tool := range toolNames(t, sess) {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("tool %q must be annotated read-only while no write tier exists", name)
		}
	}
}

// The write tier is off unless explicitly enabled, so with it disabled no
// tool may be annotated destructive.
func TestNoDestructiveToolsWhenWritesDisabled(t *testing.T) {
	sess := connect(t, MCPConfig{Version: "test", Target: "nas.local", EnableWrites: false})

	for name, tool := range toolNames(t, sess) {
		if tool.Annotations != nil && tool.Annotations.DestructiveHint != nil && *tool.Annotations.DestructiveHint {
			t.Errorf("tool %q is destructive but the write tier is disabled", name)
		}
	}
}

func TestServerInfoReportsPosture(t *testing.T) {
	sess := connect(t, MCPConfig{Version: "v1.2.3", Target: "nas.local", EnableWrites: false})

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "server_info",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("call server_info: %v", err)
	}
	if res.IsError {
		t.Fatalf("server_info returned an error: %v", res.Content)
	}

	info, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured content, got %T", res.StructuredContent)
	}
	if info["version"] != "v1.2.3" {
		t.Errorf("version = %v, want v1.2.3", info["version"])
	}
	if info["target"] != "nas.local" {
		t.Errorf("target = %v, want nas.local", info["target"])
	}
	if info["writes_enabled"] != false {
		t.Errorf("writes_enabled = %v, want false", info["writes_enabled"])
	}
}

// No tool may accept a credential: sessions supply their own, and the model
// must never be in a position to pass one. See design D11b.
func TestNoToolAcceptsCredentials(t *testing.T) {
	sess := connect(t, MCPConfig{Version: "test", Target: "nas.local"})

	forbidden := []string{"api_key", "apikey", "password", "username", "token", "host", "secret"}
	for name, tool := range toolNames(t, sess) {
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal schema for %q: %v", name, err)
		}
		for _, word := range forbidden {
			if containsFold(string(schema), `"`+word+`"`) {
				t.Errorf("tool %q exposes a %q parameter; credentials must never be tool arguments", name, word)
			}
		}
	}
}

func containsFold(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		indexFold(haystack, needle) >= 0
}

func indexFold(s, sub string) int {
	lower := func(b byte) byte {
		if b >= 'A' && b <= 'Z' {
			return b + 32
		}
		return b
	}
outer:
	for i := 0; i+len(sub) <= len(s); i++ {
		for j := range len(sub) {
			if lower(s[i+j]) != lower(sub[j]) {
				continue outer
			}
		}
		return i
	}
	return -1
}
