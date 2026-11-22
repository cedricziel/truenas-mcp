package server

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerJobResourceLinks(srv *mcp.Server) {
	srv.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			res, err := next(ctx, method, req)
			if err != nil || method != "tools/call" {
				return res, err
			}
			if result, ok := res.(*mcp.CallToolResult); ok {
				if id, _, _, started := jobStarted(result); started {
					result.Content = append(result.Content, jobResourceLink(id))
				}
			}
			return res, nil
		}
	})
}

func jobResourceLink(id int64) *mcp.ResourceLink {
	return &mcp.ResourceLink{
		URI:         fmt.Sprintf("truenas://job/%d", id),
		Name:        fmt.Sprintf("TrueNAS job %d", id),
		MIMEType:    "application/json",
		Description: "Current state, progress, and outcome of this job.",
	}
}
