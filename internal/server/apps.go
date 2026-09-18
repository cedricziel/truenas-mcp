package server

import (
	"context"
	"time"

	"github.com/cedricziel/truenas-mcp/internal/apps"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerApps publishes every MCP App and the tool each one renders.
//
// The app resources are published even without a session, the same way the
// documentation resources are: a resource listing that changes shape with
// the connection would make a host's cached view of the server wrong. The
// tools behind them need a session and are registered only with one.
func registerApps(srv *mcp.Server, session sessionFor) {
	for _, app := range apps.All() {
		srv.AddResource(&mcp.Resource{
			URI:         app.URI,
			Name:        app.Name,
			Description: app.Description,
			MIMEType:    apps.MIMEType,
			Meta:        apps.ResourceMeta(app),
		}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{{
					URI:      req.Params.URI,
					MIMEType: apps.MIMEType,
					Text:     app.HTML,
				}},
			}, nil
		})
	}

	if session == nil {
		return
	}

	registerInventory(srv, session)
}

// registerInventory is the tool behind the inventory app. The tool is useful
// on its own -- one call for a box-wide summary -- and the _meta is what
// turns its result into a view on a host that supports the extension.
func registerInventory(srv *mcp.Server, session sessionFor) {
	app, _ := apps.ByTool("inventory")

	mcp.AddTool(srv, &mcp.Tool{
		Name: "inventory",
		Description: "Summarise everything on the target in one call: host identity, pools with " +
			"capacity, datasets, installed apps, virtual machines, containers, SMB and NFS shares, " +
			"and current alerts. Sections this API key cannot read are reported under errors " +
			"rather than failing the call. Use the concern tools for detail on any one section.",
		Annotations: readAnnotations("Inventory"),
		Meta:        apps.ToolMeta(app),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, InventoryOutput, error) {
		s, err := session(ctx)
		if err != nil {
			return nil, InventoryOutput{}, err
		}
		out, err := collectInventory(ctx, s.Client(), time.Now())
		if err != nil {
			return nil, InventoryOutput{}, err
		}
		return nil, out, nil
	})
}
