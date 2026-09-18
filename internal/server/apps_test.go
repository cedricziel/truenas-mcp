package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cedricziel/truenas-mcp/internal/apps"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCP Apps are an extension the go-sdk does not model, so these tests hold the
// wire shape -- what a host actually receives -- to the extension's spec
// rather than trusting the helpers that produce it.

func TestAppResourcesArePublishedWithOrWithoutASession(t *testing.T) {
	for name, session := range map[string]sessionFor{"without": nil, "with": stubSession} {
		t.Run(name, func(t *testing.T) {
			sess := resourceSession(t, MCPConfig{Version: "t", Target: "nas"}, session)
			resources := listedResources(t, sess)
			for _, app := range apps.All() {
				r, ok := resources[app.URI]
				if !ok {
					t.Fatalf("app resource %q is not published", app.URI)
				}
				if r.MIMEType != apps.MIMEType {
					t.Errorf("%s mimeType = %q, want %q", app.URI, r.MIMEType, apps.MIMEType)
				}
				ui, _ := r.Meta["ui"].(map[string]any)
				if ui == nil {
					t.Errorf("%s carries no _meta.ui: %#v", app.URI, r.Meta)
				}
			}
		})
	}
}

func TestAppResourceReadsAsItsHTML(t *testing.T) {
	sess := resourceSession(t, MCPConfig{Version: "t", Target: "nas"}, nil)

	for _, app := range apps.All() {
		res, err := sess.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: app.URI})
		if err != nil {
			t.Fatalf("read %s: %v", app.URI, err)
		}
		if len(res.Contents) != 1 {
			t.Fatalf("%s returned %d contents, want one document", app.URI, len(res.Contents))
		}
		c := res.Contents[0]
		if c.URI != app.URI || c.MIMEType != apps.MIMEType {
			t.Errorf("%s content = uri %q mime %q", app.URI, c.URI, c.MIMEType)
		}
		if c.Text != app.HTML {
			t.Errorf("%s content does not match the embedded document", app.URI)
		}
		if len(c.Blob) != 0 {
			t.Errorf("%s must be delivered as text, not a blob", app.URI)
		}
	}
}

// The tool is the half that needs a middleware connection.
func TestInventoryToolRequiresASession(t *testing.T) {
	without := registeredToolNames(t, NewMCPServer(MCPConfig{Version: "t", Target: "nas"}, nil))
	if without["inventory"] {
		t.Error("inventory tool registered without a session")
	}
	with := registeredToolNames(t, NewMCPServer(MCPConfig{Version: "t", Target: "nas"}, stubSession))
	if !with["inventory"] {
		t.Error("inventory tool missing with a session")
	}
}

// Every app's tool must point at a resource that is actually published, and
// the pointer must survive the trip through the SDK's _meta handling.
func TestAppToolsLinkToPublishedResources(t *testing.T) {
	srv := NewMCPServer(MCPConfig{Version: "t", Target: "nas"}, stubSession)
	tools := registeredTools(t, srv)
	resources := listedResources(t, resourceSession(t, MCPConfig{Version: "t", Target: "nas"}, stubSession))

	for _, app := range apps.All() {
		tool, ok := tools[app.Tool]
		if !ok {
			t.Fatalf("app %q names tool %q, which is not registered", app.URI, app.Tool)
		}
		raw, err := json.Marshal(tool.Meta)
		if err != nil {
			t.Fatal(err)
		}
		var meta struct {
			UI struct {
				ResourceURI string `json:"resourceUri"`
			} `json:"ui"`
		}
		if err := json.Unmarshal(raw, &meta); err != nil {
			t.Fatal(err)
		}
		if meta.UI.ResourceURI != app.URI {
			t.Errorf("tool %q _meta.ui.resourceUri = %q, want %q", app.Tool, meta.UI.ResourceURI, app.URI)
		}
		if _, ok := resources[meta.UI.ResourceURI]; !ok {
			t.Errorf("tool %q points at %q, which is not a published resource", app.Tool, meta.UI.ResourceURI)
		}
		if !tool.Annotations.ReadOnlyHint {
			t.Errorf("tool %q renders a view but is not read-only; the view can call it to refresh without consent", app.Tool)
		}
	}
}
