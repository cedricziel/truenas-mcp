package server

import (
	"context"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPConfig is the posture the MCP surface is built from. It deliberately
// carries no TrueNAS credential: each session supplies its own, and that
// credential's privilege level is the outer bound on what the session reaches.
type MCPConfig struct {
	Version      string
	Target       string
	EnableWrites bool
}

// ServerInfoOutput reports what this server is and how it is configured, so a
// caller can tell which box it is talking to and whether mutation is possible
// before doing anything else.
type ServerInfoOutput struct {
	Name          string `json:"name" jsonschema:"the server's name"`
	Version       string `json:"version" jsonschema:"the build this server was made from"`
	Target        string `json:"target" jsonschema:"the TrueNAS instance this server manages"`
	WritesEnabled bool   `json:"writes_enabled" jsonschema:"whether any mutating tool is exposed"`
}

// serverInfoInput takes no arguments. It is a named empty struct rather than
// a map so the generated schema stays closed.
type serverInfoInput struct{}

// NewMCPServer builds the MCP server and registers the tool surface permitted
// by cfg. Tools are registered only when their tier is enabled, so the tool
// list is itself the policy: a disabled tier has no tool to call.
func NewMCPServer(cfg MCPConfig) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "truenas-mcp",
		Version: cfg.Version,
	}, nil)

	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}

	mcp.AddTool(srv, &mcp.Tool{
		Name: "server_info",
		Description: "Report which TrueNAS instance this server manages, which build " +
			"it is running, and whether mutating tools are enabled.",
		Annotations: readOnly,
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ serverInfoInput) (*mcp.CallToolResult, ServerInfoOutput, error) {
		return nil, ServerInfoOutput{
			Name:          "truenas-mcp",
			Version:       cfg.Version,
			Target:        cfg.Target,
			WritesEnabled: cfg.EnableWrites,
		}, nil
	})

	return srv
}

// NewMCPHandler serves the MCP server over the Streamable HTTP transport.
//
// The transport is HTTP rather than stdio because the server is deployed as a
// container on the appliance it manages, so the client cannot spawn it as a
// child process.
func NewMCPHandler(cfg MCPConfig) http.Handler {
	srv := NewMCPServer(cfg)
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return srv
	}, nil)
}
