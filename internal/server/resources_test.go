package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func resourceSession(t *testing.T, cfg MCPConfig, session sessionFor) *mcp.ClientSession {
	t.Helper()

	srv := NewMCPServer(cfg, session)
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

func listedResources(t *testing.T, sess *mcp.ClientSession) map[string]*mcp.Resource {
	t.Helper()
	res, err := sess.ListResources(context.Background(), nil)
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	out := map[string]*mcp.Resource{}
	for _, r := range res.Resources {
		out[r.URI] = r
	}
	return out
}

// Documentation resources teach the filter syntax and ZFS semantics once,
// rather than repeating them in every tool description where they would be
// paid for on every request.
func TestDocumentationResourcesArePublished(t *testing.T) {
	sess := resourceSession(t, MCPConfig{Version: "t", Target: "nas"}, stubSession)

	resources := listedResources(t, sess)
	for _, uri := range []string{uriDocFilter, uriDocZFS} {
		if _, ok := resources[uri]; !ok {
			t.Errorf("documentation resource %q is not published", uri)
		}
	}
}

func TestFilterDocumentationIsReadable(t *testing.T) {
	sess := resourceSession(t, MCPConfig{Version: "t", Target: "nas"}, stubSession)

	res, err := sess.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uriDocFilter})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(res.Contents) == 0 {
		t.Fatal("filter documentation is empty")
	}
	text := res.Contents[0].Text
	// It must actually teach the thing, with an example to copy.
	for _, want := range []string{"[field, operator, value]", `["name", "=", "tank"]`} {
		if !strings.Contains(text, want) {
			t.Errorf("filter documentation should contain %q", want)
		}
	}
}

func TestDatasetPropertyDocumentationExplainsInheritance(t *testing.T) {
	sess := resourceSession(t, MCPConfig{Version: "t", Target: "nas"}, stubSession)

	res, err := sess.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uriDocZFS})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := res.Contents[0].Text
	for _, want := range []string{"INHERITED", "parsed", "refquota"} {
		if !strings.Contains(text, want) {
			t.Errorf("dataset documentation should mention %q", want)
		}
	}
}

// Tool descriptions must not restate what the documentation resources carry;
// that content would otherwise be paid for on every request.
func TestToolDescriptionsDoNotRestateTheFilterSyntax(t *testing.T) {
	srv := NewMCPServer(MCPConfig{Version: "t", Target: "nas", EnableWrites: true}, stubSession)

	for name, tool := range registeredTools(t, srv) {
		if strings.Contains(tool.Description, "[field, operator, value]") {
			t.Errorf("tool %q restates the filter syntax; point at %s instead", name, uriDocFilter)
		}
	}
}

// Entity resources need a middleware connection, so they appear only when one
// is available.
func TestEntityResourcesRequireASession(t *testing.T) {
	sess := resourceSession(t, MCPConfig{Version: "t", Target: "nas"}, nil)

	resources := listedResources(t, sess)
	for _, uri := range []string{uriAlerts, uriPools, uriApps} {
		if _, ok := resources[uri]; ok {
			t.Errorf("%q should not be published without a middleware connection", uri)
		}
	}
	// Documentation has no such dependency and must still be there.
	if _, ok := resources[uriDocFilter]; !ok {
		t.Error("documentation resources should not depend on a connection")
	}
}

func TestEntityResourcesArePublishedWithASession(t *testing.T) {
	sess := resourceSession(t, MCPConfig{Version: "t", Target: "nas"}, stubSession)

	resources := listedResources(t, sess)
	for _, uri := range []string{uriAlerts, uriHealth, uriPools, uriApps} {
		if _, ok := resources[uri]; !ok {
			t.Errorf("entity resource %q is not published", uri)
		}
	}
}
